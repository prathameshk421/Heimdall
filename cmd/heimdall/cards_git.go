package main

import (
	"context"
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/cardgen"
)

const (
	refixWindowSeconds = 14 * 24 * 3600
	maxRevertLeads     = 10
	maxFixLeads        = 15
	maxHotLeads        = 12
	maxRefixLeads      = 10
	maxLeadFiles       = 6
	commitSeparator    = "\x1e"
	commitFormat       = "--pretty=format:" + commitSeparator + "%h%x09%at%x09%s"
	fixPattern         = "(^|[^[:alnum:]])(fix|fixes|fixed|bug|hotfix|regression)"
)

// gitRunner runs git with fixed arguments in dir and returns its output.
type gitRunner func(ctx context.Context, dir string, args ...string) (string, error)

// execGit is the real runner. Arguments are always fixed by this package, never built from session text.
func execGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...) // #nosec G204 -- fixed git command; this package builds every argument, as in internal/pack/collect.go
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return string(out), nil
}

type commitInfo struct {
	Hash    string
	Unix    int64
	Subject string
	Files   []string
}

// parseCommits reads the output of git log with commitFormat and --name-only.
func parseCommits(out string) []commitInfo {
	var commits []commitInfo
	for _, chunk := range strings.Split(out, commitSeparator) {
		lines := strings.Split(strings.TrimSpace(chunk), "\n")
		head := strings.SplitN(lines[0], "\t", 3)
		if len(head) != 3 {
			continue
		}
		unix, err := strconv.ParseInt(head[1], 10, 64)
		if err != nil {
			continue
		}
		c := commitInfo{Hash: head[0], Unix: unix, Subject: head[2]}
		for _, f := range lines[1:] {
			if f = strings.TrimSpace(f); f != "" {
				c.Files = append(c.Files, f)
			}
		}
		commits = append(commits, c)
	}
	return commits
}

// gitIdentity returns the configured name and email, which may be empty.
func gitIdentity(ctx context.Context, run gitRunner, dir string) (name, email string) {
	n, _ := run(ctx, dir, "config", "user.name")
	e, _ := run(ctx, dir, "config", "user.email")
	return strings.TrimSpace(n), strings.TrimSpace(e)
}

// gitLeads finds cheap pointers in history: reverts, fix commits of this author, the files this author changed
// most, and files fixed twice within 14 days. They are hints for the agent, never conclusions.
func gitLeads(ctx context.Context, run gitRunner, dir, email string, days int, redact func(string) string) ([]cardgen.Lead, error) {
	since := "--since=" + strconv.Itoa(days) + " days ago"
	reverts, err := run(ctx, dir, "log", since, "-i", "-E", "--grep=^revert", "--name-only", commitFormat, "-n", "100")
	if err != nil {
		return nil, err
	}
	fixes, err := run(ctx, dir, "log", since, "--author="+email, "-i", "-E", "--grep="+fixPattern, "--name-only", commitFormat, "-n", "300")
	if err != nil {
		return nil, err
	}
	hot, err := run(ctx, dir, "log", since, "--author="+email, "--name-only", "--pretty=format:", "-n", "2000")
	if err != nil {
		return nil, err
	}
	fixList := parseCommits(fixes)
	var leads []cardgen.Lead
	leads = append(leads, commitLeads("revert", parseCommits(reverts), maxRevertLeads, redact)...)
	leads = append(leads, commitLeads("fix", fixList, maxFixLeads, redact)...)
	leads = append(leads, refixLeads(fixList)...)
	leads = append(leads, hotLeads(hot)...)
	return leads, nil
}

func commitLeads(kind string, commits []commitInfo, limit int, redact func(string) string) []cardgen.Lead {
	var leads []cardgen.Lead
	for _, c := range commits {
		files := codeFiles(c.Files)
		if len(files) == 0 || len(leads) >= limit {
			continue
		}
		leads = append(leads, cardgen.Lead{Kind: kind, Ref: c.Hash, Text: redact(c.Subject), Detail: strings.Join(firstN(files, maxLeadFiles), ", ")})
	}
	return leads
}

// refixLeads finds files that two fix commits touched within 14 days of each other: the first fix did not hold.
func refixLeads(fixes []commitInfo) []cardgen.Lead {
	byFile := map[string][]commitInfo{}
	for _, c := range fixes {
		for _, f := range codeFiles(c.Files) {
			byFile[f] = append(byFile[f], c)
		}
	}
	var leads []cardgen.Lead
	for file, list := range byFile {
		slices.SortFunc(list, func(a, b commitInfo) int { return int(a.Unix - b.Unix) })
		for i := 1; i < len(list); i++ {
			if list[i].Unix-list[i-1].Unix <= refixWindowSeconds {
				leads = append(leads, cardgen.Lead{Kind: "refix", Ref: list[i].Hash, Text: file, Detail: "fixed again within 14 days of " + list[i-1].Hash})
				break
			}
		}
	}
	slices.SortFunc(leads, func(a, b cardgen.Lead) int { return strings.Compare(a.Text, b.Text) })
	return firstN(leads, maxRefixLeads)
}

func hotLeads(out string) []cardgen.Lead {
	counts := map[string]int{}
	for _, f := range codeFiles(strings.Split(out, "\n")) {
		counts[f]++
	}
	files := make([]string, 0, len(counts))
	for f, n := range counts {
		if n > 1 {
			files = append(files, f)
		}
	}
	slices.SortFunc(files, func(a, b string) int {
		if counts[a] != counts[b] {
			return counts[b] - counts[a]
		}
		return strings.Compare(a, b)
	})
	var leads []cardgen.Lead
	for _, f := range firstN(files, maxHotLeads) {
		leads = append(leads, cardgen.Lead{Kind: "hot", Text: f, Detail: strconv.Itoa(counts[f]) + " commits by you"})
	}
	return leads
}

// codeFiles drops lines that are not source worth a lead: blanks, locks, sums, docs and generated data.
func codeFiles(files []string) []string {
	var out []string
	for _, f := range files {
		f = strings.TrimSpace(f)
		switch {
		case f == "", strings.HasSuffix(f, ".md"), strings.HasSuffix(f, ".sum"), strings.HasSuffix(f, ".lock"),
			strings.HasSuffix(f, "package-lock.json"), strings.HasPrefix(f, "testdata/"), strings.HasPrefix(f, "docs/"),
			strings.HasPrefix(f, ".heimdall/"), strings.HasPrefix(f, ".claude/"):
			continue
		}
		out = append(out, f)
	}
	return out
}

func firstN[T any](list []T, n int) []T {
	if len(list) > n {
		return list[:n]
	}
	return list
}
