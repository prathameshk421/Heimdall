package digest_test

import (
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/digest"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/fastcheck"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
)

var update = flag.Bool("update", false, "rewrite the golden files from the current output")

// TestGoldens: each realistic page renders to exactly the committed file. The files are read by eye when they change:
// run go test ./internal/domain/digest -update, then read the diff.
func TestGoldens(t *testing.T) {
	cases := goldenCases()
	require.GreaterOrEqual(t, len(cases), 4)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := digest.RenderMarkdown(c.in)
			path := filepath.Join("testdata", c.name+".golden.md")
			if *update {
				require.NoError(t, os.WriteFile(path, []byte(got), 0o600))
			}
			want, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, string(want), got)
			require.Equal(t, got, digest.RenderMarkdown(c.in), "the same report must give the same page")
			require.LessOrEqual(t, len(got), digest.MaxBytes)
		})
	}
}

// golden is one page and the input that makes it.
type golden struct {
	name string
	in   digest.Report
}

// goldenCases are the pages a reviewer reads: an empty week, a clean shadow week, a week with would-blocks and misses,
// and a week already in enforce mode.
func goldenCases() []golden {
	from, to := period()
	return []golden{
		{name: "empty_week", in: digest.Compute(digest.Input{Repo: "acme/pay", From: from, To: to})},
		{name: "clean_shadow_week", in: digest.Compute(digest.Input{Repo: "acme/pay", From: from, To: to, PRs: []digest.PRRecord{
			withCost(passed(11), 210_000),
			withCost(passed(12), 180_000),
			withCost(withProof(passed(13), proof.KindProven, digest.Counts{In: 1, Proven: 1, NeedsPerson: 1}), 320_000),
		}})},
		{name: "would_block_and_misses", in: digest.Compute(digest.Input{Repo: "acme/pay", From: from, To: to,
			PRs: mixedWeek()})},
		{name: "enforce_week", in: digest.Compute(digest.Input{Repo: "acme/pay", From: from, To: to, PRs: []digest.PRRecord{
			withEnforce(withCost(passed(21), 190_000)),
			withEnforce(withCost(withProof(passed(22), proof.KindBlocked,
				digest.Counts{In: 3, Proven: 1, Blocking: 1, NoProof: 2}), 410_000)),
		}})},
	}
}

// mixedWeek is the week the digest exists for: some pass, some would block on a real finding, some on a missing fact,
// one was never checked, and the misses are counted.
func mixedWeek() []digest.PRRecord {
	return []digest.PRRecord{
		withCost(passed(31), 150_000),
		withCost(passed(32), 175_000),
		withCost(withProof(passed(33), proof.KindProven,
			digest.Counts{In: 2, Proven: 1, NeedsPerson: 1, NoProof: 1}), 260_000),
		withCost(withClause(passed(34), fastcheck.ClauseSecrets, fastcheck.StatusUnmet), 0),
		withCost(withClause(passed(35), fastcheck.ClauseApproval, fastcheck.StatusUnmet), 0),
		withCost(withClause(passed(36), fastcheck.ClauseDiffCoverage, fastcheck.StatusUnknown), 90_000),
		withCost(withClause(passed(37), fastcheck.ClauseMemoryRules, fastcheck.StatusNotChecked), 120_000),
		withCost(withProof(passed(38), proof.KindBlocked,
			digest.Counts{In: 4, Proven: 2, Blocking: 1, NeedsPerson: 1, Refuted: 1, NoLocation: 1}), 380_000),
		withCost(withProof(passed(39), proof.KindUnknown, digest.Counts{In: 5, Proven: 1, NeedsPerson: 1, NotChecked: 4}),
			210_000),
		withCost(withMisses(withProof(passed(40), proof.KindUnknown, digest.Counts{In: 6, NotChecked: 6}),
			digest.ReasonTally{Reason: proof.ReasonBudget, Count: 4},
			digest.ReasonTally{Reason: proof.ReasonTurnCap, Count: 2}), 300_000),
		withCost(withProof(passed(41), proof.KindNotReproduced, digest.Counts{In: 1, NoProof: 1}), 240_000),
		withCost(withBreakGlass(passed(42), 1, 1), 160_000),
		{Number: 43, At: day(18), Mode: digest.ModeShadow},
	}
}

// TestHostileTextIsEscaped: a repository name, a clause id and a reason come from a snapshot another tool wrote. Every
// one of them is placed in a code span whose fence no run inside can close, so none of them makes HTML, a link, a
// mention, an image, a heading or a table cell.
func TestHostileTextIsEscaped(t *testing.T) {
	from, to := period()
	hostile := "<script>alert(1)</script> ![x](http://evil.example/p.png) @team [a](b) | a | b *em* `code` " +
		"#12 www.evil.example https://evil.example"
	in := digest.Input{Repo: hostile, From: from, To: to, PRs: []digest.PRRecord{
		withReason(withClause(passed(1), fastcheck.ClauseID(hostile), fastcheck.StatusNotChecked), hostile),
		withMisses(withProof(passed(2), proof.KindUnknown, digest.Counts{In: 2, NotChecked: 2}),
			digest.ReasonTally{Reason: proof.Reason(hostile), Count: 2}),
	}}
	page := digest.RenderMarkdown(digest.Compute(in))
	require.Contains(t, page, "`code`", "the hostile text is shown, not silently dropped")
	require.NotContains(t, withoutCodeSpans(page), "<script>", "no html outside a code span")
	require.NotContains(t, withoutCodeSpans(page), "](http", "no link outside a code span")
	require.NotContains(t, withoutCodeSpans(page), "@team", "no mention outside a code span")
	require.NotContains(t, withoutCodeSpans(page), "www.evil")
	require.NotContains(t, withoutCodeSpans(page), "https://evil")
	require.NotContains(t, withoutCodeSpans(page), "\n#12", "no heading outside a code span")
}

// withoutCodeSpans drops every code span from a page, so a test can see what Markdown interprets as markup. It is the
// reader's view of the page with the safe places removed.
func withoutCodeSpans(page string) string {
	var b strings.Builder
	for {
		start := strings.Index(page, "`")
		if start < 0 {
			b.WriteString(page)
			return b.String()
		}
		b.WriteString(page[:start])
		run := 0
		for start+run < len(page) && page[start+run] == '`' {
			run++
		}
		fence := page[start : start+run]
		rest := page[start+run:]
		end := strings.Index(rest, fence)
		if end < 0 {
			b.WriteString(rest)
			return b.String()
		}
		page = rest[end+run:]
	}
}

// TestEscapedTextIsSafe: the escapes of the page turn markup into plain characters, and break a mention, a reference
// and a bare link, so a reason that comes from a snapshot cannot make one.
func TestEscapedTextIsSafe(t *testing.T) {
	page := digest.RenderMarkdown(digest.Compute(digest.Input{PRs: []digest.PRRecord{
		withReason(withClause(passed(1), fastcheck.ClauseSecrets, fastcheck.StatusNotChecked), "@team see #12 at www.x.example"),
	}}))
	require.Contains(t, page, "`@team see #12 at www.x.example`")
	require.NotContains(t, withoutCodeSpans(page), "@team")
}

// TestPageIsBounded: a snapshot with more distinct labels than a week holds still gives a page under the bound, and a
// cut list says how many entries it left out so the counts stay whole.
func TestPageIsBounded(t *testing.T) {
	var prs []digest.PRRecord
	for i := 1; i <= 200; i++ {
		rec := withClause(passed(i), fastcheck.ClauseID(strings.Repeat("x", 100)+strconv.Itoa(i)), fastcheck.StatusNotChecked)
		rec = withReason(rec, strings.Repeat("y", 200)+strconv.Itoa(i))
		prs = append(prs, rec)
	}
	page := digest.RenderMarkdown(digest.Compute(digest.Input{Repo: "acme/pay", PRs: prs}))
	require.LessOrEqual(t, len(page), digest.MaxBytes)
	require.Contains(t, page, "not listed to keep this report short")
}

// TestNoNaNOrInfinity: a page never holds a number a person cannot read, whatever the record held.
func TestNoNaNOrInfinity(t *testing.T) {
	page := digest.RenderMarkdown(digest.Compute(digest.Input{PRs: []digest.PRRecord{
		withCost(passed(1), 1), withCost(passed(2), 2),
	}}))
	for _, bad := range []string{"NaN", "Inf", "+Inf", "-Inf"} {
		require.NotContains(t, page, bad)
	}
}
