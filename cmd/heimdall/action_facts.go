package main

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/action"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/fastcheck"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/sarif"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testintegrity"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/secretscan"
)

// diffFacts is the gate's action.FactsReader. It reads the fast-check groups that the diff alone decides: the secret
// scan of the added lines, the pinned test files, and the static test-integrity rules. Every fact comes from the
// patches GitHub returns, so a file whose patch GitHub withholds (binary, or too large) makes each group that needs it
// unknown.
//
// The analyzers clause reads the customer's code scanning alerts for the pull request's merge ref, when the policy at
// the merge base names that source (the default). A repository without code scanning answers 403 or 404, which cannot
// be told apart from a token that may not read them, so the clause stays unknown: unknown blocks in enforce mode, never
// a false pass. A policy that names the SARIF artifact source is not wired yet, and its clause stays unknown too.
//
// The coverage report and the rule cards still need inputs this reader does not fetch (the declared test workflow's
// coverage artifact, the cards at the merge base). Their Known flags stay false.
type diffFacts struct {
	scanner *secretscan.SecretScanner
	// alerts reads code scanning alerts. Nil leaves the analyzers clause unknown.
	alerts ports.CodeScanReader
}

var _ action.FactsReader = diffFacts{}

func newDiffFacts(alerts ports.CodeScanReader) (diffFacts, error) {
	s, err := secretscan.NewSecretScanner()
	if err != nil {
		return diffFacts{}, fmt.Errorf("secret scanner: %w", err)
	}
	return diffFacts{scanner: s, alerts: alerts}, nil
}

// FastFacts reads the groups of one change.
func (d diffFacts) FastFacts(ctx context.Context, req action.FactRequest) (action.FastFacts, error) {
	diffs := make([]fileDiff, 0, len(req.Files))
	for _, f := range req.Files {
		diffs = append(diffs, parseFileDiff(f))
	}
	secrets, err := d.secrets(diffs)
	if err != nil {
		return action.FastFacts{}, err
	}
	return action.FastFacts{
		Secrets:   secrets,
		Analyzers: d.analyzers(ctx, req),
		Tests:     pinnedChanges(diffs, req.Policy.Pinned),
		Integrity: integrity(diffs, req.Tests),
	}, nil
}

// analyzers reads the code scanning alerts for the pull request's merge ref. A read that failed leaves the group
// unknown and does not fail the other groups: a broken analyzer feed must not hide the secret scan.
//
// Only alerts open at the head's merge ref count. ACTION_MODE 5.1 keeps what is new against the merge base
// (sarif.New(base, head)), and this reader leaves Base empty: reading the base's alerts needs the base branch name,
// which the fact request does not carry. A pre-existing open alert therefore counts too, which is stricter than the
// design, never weaker: it can block a change and can never pass one.
//
// A 403 or a 404 cannot be told apart ("the token may not read them" from "code scanning is not enabled"), and the
// fastcheck contract has no "not checked" state for this clause, so both leave the clause unknown. Unknown blocks in
// enforce mode and reads as neutral in shadow mode (ACTION_MODE 5.4); it never reads as a pass.
func (d diffFacts) analyzers(ctx context.Context, req action.FactRequest) fastcheck.AnalyzerFacts {
	if d.alerts == nil || req.Policy.Analyzers.Source != policy.AnalyzerSourceCodeScanning {
		return fastcheck.AnalyzerFacts{}
	}
	ref, err := codeScanningRef(req.Number)
	if err != nil {
		return fastcheck.AnalyzerFacts{}
	}
	alerts, err := d.alerts.CodeScanAlerts(ctx, req.Repo, ref, "open")
	if err != nil {
		return fastcheck.AnalyzerFacts{}
	}
	head := make([]sarif.Result, 0, len(alerts))
	for _, a := range alerts {
		if r, ok := codeScanResult(a); ok {
			head = append(head, r)
		}
	}
	return fastcheck.AnalyzerFacts{Known: true, Head: head}
}

// codeScanningRef is the ref GitHub analyses for a pull request: the merge commit of the PR. The fast check judges that
// commit, so its alerts are the ones that count.
func codeScanningRef(n int) (string, error) {
	if n < 1 || n > maxPRNumber {
		return "", fmt.Errorf("pull request number %d is out of range", n)
	}
	return "refs/pull/" + strconv.Itoa(n) + "/merge", nil
}

// codeScanResult maps one alert to a SARIF result. Check and Finger name the alert by its repo-wide number, which GitHub
// keeps for the life of the alert, so an alert that only moves to another line is not a new result. The level is the
// strongest of the security severity and the rule severity, because the policy threshold is a severity: a critical
// security alert counts as an error, which is high. "none" is the lowest level, so it never blocks at the default.
func codeScanResult(a ports.CodeScanAlert) (sarif.Result, bool) {
	if a.Number < 1 {
		return sarif.Result{}, false
	}
	return sarif.Result{
		Check:   "code-scanning:" + a.Tool,
		Rule:    a.RuleID,
		Level:   codeScanLevel(a),
		File:    a.Path,
		Lines:   [2]int{a.StartLine, a.EndLine},
		Message: a.Message,
		Finger:  "alert:" + strconv.Itoa(a.Number) + ":" + a.RuleID + ":" + a.Path,
	}, true
}

// codeScanLevel maps the two severities GitHub reports to the SARIF levels the fast check reads.
func codeScanLevel(a ports.CodeScanAlert) string {
	switch strings.ToLower(a.SecuritySeverity) {
	case "critical", "high":
		return "error"
	case "medium":
		return "warning"
	case "low":
		return "note"
	}
	switch strings.ToLower(a.Severity) {
	case "error":
		return "error"
	case "warning":
		return "warning"
	case "note", "none":
		return "note"
	}
	// An alert GitHub classified with neither severity is counted at the default threshold rather than ignored.
	return "warning"
}

// fileDiff is one changed file with its patch read into lines.
type fileDiff struct {
	file ports.ChangedFile
	// known is false when the file changed lines and GitHub returned no patch for it.
	known   bool
	added   []diffLine
	removed []string
}

type diffLine struct {
	line int
	text string
}

var reHunk = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// parseFileDiff reads the added lines with their head line numbers, and the removed lines. A hunk header it cannot
// read makes the file unknown rather than guessed.
func parseFileDiff(f ports.ChangedFile) fileDiff {
	d := fileDiff{file: f, known: true}
	if f.Patch == "" {
		d.known = f.Additions == 0 && f.Deletions == 0
		return d
	}
	next := 0
	for raw := range strings.SplitSeq(f.Patch, "\n") {
		switch {
		case strings.HasPrefix(raw, "@@"):
			m := reHunk.FindStringSubmatch(raw)
			if m == nil {
				d.known = false
				return d
			}
			next, _ = strconv.Atoi(m[1])
		case strings.HasPrefix(raw, "+"):
			d.added = append(d.added, diffLine{line: next, text: raw[1:]})
			next++
		case strings.HasPrefix(raw, "-"):
			d.removed = append(d.removed, raw[1:])
		case strings.HasPrefix(raw, `\`):
			// "\ No newline at end of file" is not a line of the file.
		default:
			next++
		}
	}
	return d
}

// credentialKinds are the scanner's kinds that are a credential. The personal-data kinds (an email, a phone number, a
// long random-looking word) are not secrets of the fast check: a commit id or a test fixture would trip them.
func credentialKinds() []string {
	return []string{"private_key", "bearer", "connection_string", "jwt", "cloud_key"}
}

func isCredential(kind string) bool {
	return strings.HasPrefix(kind, "gitleaks:") || slices.Contains(credentialKinds(), kind)
}

// secrets scans each added line, then each file's added lines together, so a key that spans lines is found too. A
// file whose patch is missing leaves the scan unknown: it did not run over the whole diff.
func (d diffFacts) secrets(diffs []fileDiff) (fastcheck.SecretFacts, error) {
	out := fastcheck.SecretFacts{Known: true}
	for _, fd := range diffs {
		if !fd.known {
			out.Known = false
			continue
		}
		perLine := 0
		for _, l := range fd.added {
			kinds, err := d.credentials(l.text)
			if err != nil {
				return fastcheck.SecretFacts{}, err
			}
			for _, k := range kinds {
				out.Findings = append(out.Findings, fastcheck.SecretFinding{File: fd.file.Path, Line: l.line, Rule: k})
			}
			perLine += len(kinds)
		}
		if len(fd.added) < 2 {
			continue
		}
		block := make([]string, 0, len(fd.added))
		for _, l := range fd.added {
			block = append(block, l.text)
		}
		kinds, err := d.credentials(strings.Join(block, "\n"))
		if err != nil {
			return fastcheck.SecretFacts{}, err
		}
		if len(kinds) > perLine {
			out.Findings = append(out.Findings, fastcheck.SecretFinding{File: fd.file.Path, Line: fd.added[0].line,
				Rule: kinds[0] + " (across lines)"})
		}
	}
	return out, nil
}

// credentials returns the credential rules that match text. The matched value is never kept.
func (d diffFacts) credentials(text string) ([]string, error) {
	_, hits, err := d.scanner.Scan(text)
	if err != nil {
		return nil, fmt.Errorf("secret scan: %w", err)
	}
	var out []string
	for _, h := range hits {
		if isCredential(h.Kind) {
			out = append(out, strings.TrimPrefix(h.Kind, "gitleaks:"))
		}
	}
	return out, nil
}

// pinnedChanges lists what the change does to the files under action.pinned that existed at the merge base. GitHub's
// file list gives a renamed file's new path only, so a rename could move a pinned file out of the globs unseen: any
// rename leaves the facts unknown when pins are set. A skip marker added to a pinned file is an edit of it, so the
// edit already names it.
func pinnedChanges(diffs []fileDiff, pinned []string) fastcheck.TestFacts {
	if len(pinned) == 0 {
		return fastcheck.TestFacts{Known: true}
	}
	out := fastcheck.TestFacts{Known: true}
	for _, fd := range diffs {
		f := fd.file
		switch f.Status {
		case ports.FileRenamed:
			return fastcheck.TestFacts{}
		case ports.FileAdded:
			continue
		case ports.FileRemoved, ports.FileModified:
		}
		if !matchesGlobs(pinned, f.Path) {
			continue
		}
		kind := fastcheck.TestEdited
		if f.Status == ports.FileRemoved {
			kind = fastcheck.TestDeleted
		}
		out.Changes = append(out.Changes, fastcheck.TestChange{Path: f.Path, Kind: kind})
	}
	return out
}

// integrity runs the static test-integrity rules over the diff. A test file whose patch is missing leaves them unknown:
// a weakened assertion there would go unseen. The mock rule gets no parsed mock facts here; its level is warn, so it
// never blocks, and only blocking violations reach the clause.
func integrity(diffs []fileDiff, tests policy.Tests) action.IntegrityFacts {
	facts := testintegrity.Facts{}
	for _, fd := range diffs {
		isTest := isTestPath(fd.file.Path, tests.Dir)
		if !fd.known && isTest {
			return action.IntegrityFacts{}
		}
		c := testintegrity.FileChange{Path: fd.file.Path, Status: integrityStatus(fd.file.Status), IsTest: isTest,
			Removed: fd.removed}
		for _, l := range fd.added {
			c.Added = append(c.Added, l.text)
		}
		facts.Files = append(facts.Files, c)
	}
	return action.IntegrityFacts{Known: true, Result: testintegrity.Evaluate(facts, testintegrity.Options{})}
}

func integrityStatus(s ports.FileStatus) testintegrity.ChangeStatus {
	switch s {
	case ports.FileAdded:
		return testintegrity.StatusAdded
	case ports.FileRemoved:
		return testintegrity.StatusDeleted
	case ports.FileRenamed:
		return testintegrity.StatusRenamed
	case ports.FileModified:
	}
	return testintegrity.StatusModified
}

func matchesGlobs(globs []string, name string) bool {
	return slices.ContainsFunc(globs, func(g string) bool { return policy.MatchGlob(g, name) })
}

// isTestPath is the test-file rule of the diff reader: a path under tests.dir, or a file named by the test conventions
// of the languages the integrity rules read (Go, Python, JavaScript and TypeScript, Java).
func isTestPath(p, dir string) bool {
	if dir = strings.Trim(dir, "/"); dir != "" && (p == dir || strings.HasPrefix(p, dir+"/")) {
		return true
	}
	base := path.Base(p)
	switch {
	case strings.HasSuffix(base, "_test.go"), strings.HasSuffix(base, "_test.py"), strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py"):
		return true
	case strings.HasSuffix(base, "Test.java"), strings.HasSuffix(base, "Tests.java"):
		return true
	}
	for _, mid := range []string{".test.", ".spec."} {
		if strings.Contains(base, mid) {
			return true
		}
	}
	return false
}
