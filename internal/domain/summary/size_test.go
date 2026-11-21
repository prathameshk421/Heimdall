package summary_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/fastcheck"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/summary"
)

// hugeInput is the worst case for size: thousands of findings and clauses, every string at its longest and made of
// the characters that grow most when escaped.
func hugeInput(provenN, unprovenN int) summary.Input {
	long := strings.Repeat("&<`@", 400)
	in := summary.Input{Change: "412", Version: 7, Head: head, Action: enforce(true), Proof: proof.Outcome{Kind: proof.KindBlocked}}
	for i := 1; i <= provenN; i++ {
		f := provenE1(fmt.Sprintf("F%d", i), true)
		f.Claim, f.Location.File = long, long
		f.Evidence = summary.Evidence{Kind: summary.EvidenceTool, Name: long, Rule: long}
		in.Findings = append(in.Findings, f)
	}
	for i := 1; i <= unprovenN; i++ {
		f := dropped(fmt.Sprintf("F%d", provenN+i), summary.Reviewer(fmt.Sprintf("bot-%d", i%50)), proof.DropNoProof)
		f.Claim, f.Location.File = long, long
		in.Findings = append(in.Findings, f)
	}
	fast := fastcheck.Result{Verdict: gate.VerdictFail}
	for i := range 100 {
		fast.Clauses = append(fast.Clauses, clause(fastcheck.ClauseID(fmt.Sprintf("c%d", i)), fastcheck.StatusUnmet, long))
	}
	in.Fast = fast
	return in
}

// TestSizeLimit: the page never passes MaxBytes, however much comes in, and a cut list says how much is left out.
func TestSizeLimit(t *testing.T) {
	require.LessOrEqual(t, summary.MaxBytes, 65_536, "GitHub's limit for a comment is 65,536 characters")
	for _, c := range [][2]int{{0, 0}, {1, 1}, {45, 0}, {45, 5000}, {3000, 3000}, {0, 20_000}} {
		t.Run(fmt.Sprintf("proven=%d,unproven=%d", c[0], c[1]), func(t *testing.T) {
			in := hugeInput(c[0], c[1])
			out := summary.Render(in)
			require.LessOrEqual(t, len(out), summary.MaxBytes)
			require.Equal(t, out, summary.Render(in), "a cut page is the same every time")
			require.True(t, strings.HasPrefix(out, "<!-- heimdall:proof-summary "))
			plain := withoutCode(t, out)
			require.Equal(t, strings.Count(plain, "<details>"), strings.Count(plain, "</details>"), "no details block is cut")
			require.LessOrEqual(t, strings.Count(plain, "<details>"), 1)
			if c[0] > 0 || c[1] > 0 {
				require.Contains(t, out, "- **unmet** ", "the fast check clauses are never dropped for the lists")
			}
		})
	}
}

// TestCutListSaysHowManyAreLeftOut: a list over its cap ends with "and N more", and N makes the count whole.
func TestCutListSaysHowManyAreLeftOut(t *testing.T) {
	in := summary.Input{Head: head, Action: enforce(true), Fast: greenFast(), Proof: proof.Outcome{Kind: proof.KindBlocked}}
	for i := 1; i <= 100; i++ {
		in.Findings = append(in.Findings, provenE1(fmt.Sprintf("F%d", i), true))
	}
	out := summary.Render(in)
	require.LessOrEqual(t, len(out), summary.MaxBytes)
	shown := strings.Count(out, "- **F")
	var left int
	for _, line := range strings.Split(out, "\n") {
		if _, err := fmt.Sscanf(line, "- and %d more proven findings, not listed", &left); err == nil {
			break
		}
	}
	require.Positive(t, left, "the cut is announced")
	require.Equal(t, 100, shown+left)
	require.Contains(t, out, "**F1** ", "the first in order stays")
	require.NotContains(t, out, fmt.Sprintf("**F%d** ", 100), "the cut takes from the end")
	require.Contains(t, out, "Proven: 100 blocking findings", "the headline counts every finding, not the shown ones")
}

// TestSmallPageIsNotCut: a normal page shows every item and has no "and N more" line.
func TestSmallPageIsNotCut(t *testing.T) {
	for _, c := range goldenCases() {
		require.NotContains(t, summary.Render(c.in), "more", c.name)
	}
}
