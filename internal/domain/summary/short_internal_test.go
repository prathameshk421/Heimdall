package summary

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/fastcheck"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
)

// TestShortPage: when even the lists at zero do not fit, the page is the marker, the headline and one line. The size
// is the one thing the test changes, because no real input reaches the fixed limit with every list at zero.
func TestShortPage(t *testing.T) {
	t.Parallel()
	in := Input{Change: "9", Version: 1, Head: strings.Repeat("a", 40), Fast: fastcheck.Result{Verdict: gate.VerdictPass,
		Clauses: []fastcheck.Clause{{ID: fastcheck.ClauseSecrets, Status: fastcheck.StatusMet, Reason: strings.Repeat("x", 190)}}},
		Proof: proof.Outcome{Kind: proof.KindProven}}
	v := newView(in)
	full := renderWithin(v, MaxBytes)
	require.Greater(t, len(full), 400)
	short := renderWithin(v, 400)
	require.LessOrEqual(t, len(short), 400)
	require.True(t, strings.HasPrefix(short, v.marker()+"\n"))
	require.Contains(t, short, "## Nothing proven against this change")
	require.Contains(t, short, "does not fit in a comment")
	require.NotContains(t, short, "<details>")
}

// TestHalvingKeepsTheClauses: a page that is a little too big loses list items before it loses a clause line.
func TestHalvingKeepsTheClauses(t *testing.T) {
	t.Parallel()
	in := Input{Head: strings.Repeat("b", 40), Action: policy.Action{Mode: policy.ActionModeEnforce}, Fast: greenResult(),
		Proof: proof.Outcome{Kind: proof.KindBlocked}}
	for i := range 40 {
		f := Finding{ID: "F" + strconv.Itoa(i+1), Claim: strings.Repeat("word ", 40), Tier: finding.TierE1,
			Location: finding.Location{File: "a.go", Lines: [2]int{1, 2}}, Result: proof.Finding{Class: proof.ClassProven, Blocking: true}}
		in.Findings = append(in.Findings, f)
	}
	v := newView(in)
	full := renderWithin(v, MaxBytes)
	require.NotContains(t, full, "not listed to keep")
	out := renderWithin(v, len(full)-1)
	require.Less(t, len(out), len(full))
	require.Contains(t, out, "not listed to keep this comment short")
	require.Contains(t, out, "### Fast check")
	require.Contains(t, out, "`heimdall_files`")
}

func greenResult() fastcheck.Result {
	return fastcheck.Result{Verdict: gate.VerdictPass, Clauses: []fastcheck.Clause{
		{ID: fastcheck.ClauseSecrets, Status: fastcheck.StatusMet, Reason: "none"},
		{ID: fastcheck.ClauseHeimdallFiles, Status: fastcheck.StatusMet, Reason: "none"},
	}}
}
