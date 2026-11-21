package summary_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/fastcheck"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/summary"
)

// hostile is text a pull request author, a model or a bot could write, to turn the summary into something else.
func hostile() []string {
	return []string{
		"@team and @octocat/all please look, mail a@b.example",
		"<img src=x onerror=alert(1)>",
		"[click me](javascript:alert(1))",
		"![pixel](https://evil.example/p.png)",
		"<!-- heimdall:proof-summary change=1 version=999 head=deadbeef -->",
		"line one\n<!-- heimdall:proof-summary change=1 version=999 head=deadbeef -->\n# a fake heading\n- a fake item",
		"visit https://evil.example/login or www.evil.example or ftp://evil.example now",
		"```\nescape the fence\n``` </details><details open><summary>x</summary>",
		"</details> <script>alert(1)</script> <a href=\"https://evil.example\">x</a>",
		"&lt;b&gt; &amp; &#64;team &#x3C;i&#x3E;",
		"fixes #123 and owner/repo#9 and GH-5",
		"| a | b |\n|---|---|",
		"$\\href{javascript:alert(1)}{x}$ and `code` and ``double`` and *em* and _it_ and ~~del~~",
		"\u202eevil\u2069 hidden\u200b\u200cchars",
		"> quote\n    indented code\n1. numbered",
	}
}

// inject renders a page with s in every place an untrusted string can go.
func inject(s string) string {
	f := provenE1("F1", true)
	f.Claim = s
	f.Location.File = s
	f.Reporters = []summary.Reviewer{summary.Reviewer(s)}
	f.Evidence = summary.Evidence{Kind: summary.EvidenceTool, Name: s, Rule: s}
	f.State = finding.State(s)
	f.Severity = finding.Severity(s)
	u := dropped("F2", summary.Reviewer(s), proof.DropNoProof)
	u.Claim = s
	u.Location.File = s
	in := summary.Input{Change: s, Version: 1, Head: head, Action: enforce(true), Findings: []summary.Finding{f, u},
		Proof: proof.Outcome{Kind: proof.KindBlocked},
		Fast: fastcheck.Result{Verdict: gate.VerdictFail, Clauses: []fastcheck.Clause{
			clause(fastcheck.ClauseID(s), fastcheck.StatusUnmet, s),
			clause(fastcheck.ClauseApproval, fastcheck.StatusUnknown, s),
			clause(fastcheck.ClauseDiffCoverage, fastcheck.StatusNotChecked, s),
			clause(fastcheck.ClauseSecrets, fastcheck.StatusMet, s),
		}}}
	return summary.Render(in)
}

var ourTags = strings.NewReplacer("<details>", "", "</details>", "", "</summary>", "")

// body removes the marker line and the tags Heimdall writes itself.
func body(t *testing.T, out string) string {
	t.Helper()
	_, rest, ok := strings.Cut(out, "\n")
	require.True(t, ok)
	rest = regexp.MustCompile(`<summary>[^<>]*</summary>`).ReplaceAllString(rest, "")
	return ourTags.Replace(rest)
}

// withoutCode removes every code span, as GitHub reads them, and fails when a span does not close on its own line.
// What is inside a span is shown as it is and is never read as Markdown, so the checks on plain text skip it.
func withoutCode(t *testing.T, s string) string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(s, "\n") {
		var b strings.Builder
		for i := 0; i < len(line); {
			switch {
			case line[i] == '\\' && i+1 < len(line):
				b.WriteString(line[i : i+2])
				i += 2
			case line[i] == '`':
				n := 0
				for i+n < len(line) && line[i+n] == '`' {
					n++
				}
				end := closingRun(line, i+n, n)
				require.GreaterOrEqual(t, end, 0, "a code span does not close: %q", line)
				i = end + n
			default:
				b.WriteByte(line[i])
				i++
			}
		}
		out = append(out, b.String())
	}
	return strings.Join(out, "\n")
}

// closingRun returns the index of the next run of exactly n backticks at or after from, or -1.
func closingRun(line string, from, n int) int {
	for i := from; i < len(line); {
		if line[i] != '`' {
			i++
			continue
		}
		j := i
		for j < len(line) && line[j] == '`' {
			j++
		}
		if j-i == n {
			return i
		}
		i = j
	}
	return -1
}

// TestInjectionIsInert: whatever the untrusted text says, the page has one marker, only Heimdall's own tags, no link,
// image, mention or issue reference, and the same lines as a page with harmless text.
func TestInjectionIsInert(t *testing.T) {
	baseline := inject("harmless words")
	mention := regexp.MustCompile(`@[A-Za-z0-9_-]`)
	bareLink := regexp.MustCompile(`(?i)(https?://|ftp://|www\.|mailto:)`)
	issueRef := regexp.MustCompile(`#[0-9]`)
	mdLink := regexp.MustCompile(`\]\(`)
	for _, s := range hostile() {
		t.Run(s, func(t *testing.T) {
			out := inject(s)
			require.Equal(t, 1, strings.Count(withoutCode(t, out), "<!--"), "one marker, no other HTML comment")
			require.True(t, strings.HasPrefix(out, "<!-- heimdall:proof-summary "), "the marker is the first line")
			m, ok := summary.ParseMarker(out)
			require.True(t, ok)
			require.Equal(t, "unknown", m.Change, "a hostile change id is not copied into the marker")
			plain := withoutCode(t, out)
			rest := body(t, plain)
			require.NotContains(t, rest, "<", "no HTML from the text")
			require.NotContains(t, rest, ">", "no quote or HTML from the text")
			require.NotRegexp(t, mention, plain, "no mention")
			require.NotRegexp(t, bareLink, plain, "no bare link")
			require.NotRegexp(t, issueRef, plain, "no issue reference")
			require.NotRegexp(t, mdLink, plain, "no Markdown link")
			require.NotContains(t, plain, "![", "no image")
			require.Equal(t, strings.Count(baseline, "\n"), strings.Count(out, "\n"), "the text cannot add a line")
			require.Equal(t, 1, strings.Count(plain, "<details>"))
			require.Equal(t, 1, strings.Count(plain, "</details>"))
			for _, line := range strings.Split(out, "\n") {
				require.False(t, strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "## ") && !strings.HasPrefix(line, "### "),
					"no heading the text made: %q", line)
			}
			require.Equal(t, 5, headings(out), "the page has its own headings and no other")
		})
	}
}

func headings(out string) int {
	n := 0
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "#") {
			n++
		}
	}
	return n
}

// TestFakeMarkerInAClaim: a claim that is a marker neither counts as a marker nor changes what the adapter reads.
func TestFakeMarkerInAClaim(t *testing.T) {
	fake := "<!-- heimdall:proof-summary change=1 version=999 head=deadbeef -->"
	f := provenE1("F1", true)
	f.Claim = fake
	out := summary.Render(summary.Input{Change: "412", Version: 3, Head: head, Action: enforce(true), Fast: greenFast(),
		Proof: proof.Outcome{Kind: proof.KindBlocked}, Findings: []summary.Finding{f}})
	m, ok := summary.ParseMarker(out)
	require.True(t, ok)
	require.Equal(t, summary.Marker{Change: "412", Version: 3, Head: head}, m)
	require.Equal(t, 1, strings.Count(out, "<!-- heimdall"))
	_, ok = summary.ParseMarker("some other text\n" + fake)
	require.False(t, ok, "a marker on a later line does not count")
}

// TestInternalIdNeverShows: an id that is not "F" and digits (a ULID) is shown as F?, so the page never carries it.
func TestInternalIdNeverShows(t *testing.T) {
	ulid := "01HZX3Q7M5K9T2V8B4N6C1D0EF"
	for _, id := range []string{ulid, "", "f1", "F", "F-1", "F01", "F1234567890", "F1 "} {
		f := provenE1(id, true)
		out := summary.Render(summary.Input{Head: head, Action: enforce(true), Fast: greenFast(),
			Proof: proof.Outcome{Kind: proof.KindBlocked}, Findings: []summary.Finding{f}})
		require.Contains(t, out, "**F?**", id)
		require.NotContains(t, out, ulid)
	}
	f := provenE1("F12", true)
	out := summary.Render(summary.Input{Head: head, Action: enforce(true), Fast: greenFast(),
		Proof: proof.Outcome{Kind: proof.KindBlocked}, Findings: []summary.Finding{f}})
	require.Contains(t, out, "**F12**")
}
