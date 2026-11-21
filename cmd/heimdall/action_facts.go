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
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testintegrity"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/secretscan"
)

// diffFacts is the gate's action.FactsReader. It reads the fast-check groups that the diff alone decides: the secret
// scan of the added lines, the pinned test files, and the static test-integrity rules. Every fact comes from the
// patches GitHub returns, so a file whose patch GitHub withholds (binary, or too large) makes each group that needs it
// unknown.
//
// The analyzer results, the coverage report and the rule cards need inputs this reader does not fetch (code scanning
// alerts or a SARIF artifact, the declared test workflow's coverage artifact, the cards at the merge base). Their Known
// flags stay false, so a clause the policy turns on for them reads unknown, never met.
type diffFacts struct {
	scanner *secretscan.SecretScanner
}

var _ action.FactsReader = diffFacts{}

func newDiffFacts() (diffFacts, error) {
	s, err := secretscan.NewSecretScanner()
	if err != nil {
		return diffFacts{}, fmt.Errorf("secret scanner: %w", err)
	}
	return diffFacts{scanner: s}, nil
}

// FastFacts reads the groups of one change.
func (d diffFacts) FastFacts(_ context.Context, req action.FactRequest) (action.FastFacts, error) {
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
		Tests:     pinnedChanges(diffs, req.Policy.Pinned),
		Integrity: integrity(diffs, req.Tests),
	}, nil
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
