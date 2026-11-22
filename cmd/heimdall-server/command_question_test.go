package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

func TestOpenFindingRefsSortByNumberNotText(t *testing.T) {
	view := change.View{Findings: map[string]change.FindingInfo{}}
	for _, ref := range []string{"F10", "F2", "F1"} {
		view.Findings[ref] = change.FindingInfo{State: finding.StateUpheld}
	}
	got := openFindingRefs(view)
	require.Len(t, got, 3)
	require.True(t, strings.HasPrefix(got[0], "F1 "), got[0])
	require.True(t, strings.HasPrefix(got[1], "F2 "), got[1])
	require.True(t, strings.HasPrefix(got[2], "F10 "), got[2])
}

func TestDismissedFindingsAreNotOpen(t *testing.T) {
	view := change.View{Findings: map[string]change.FindingInfo{
		"F1": {State: finding.StateDismissed},
		"F2": {State: finding.StateUpheld},
	}}
	require.Equal(t, []string{"F2 (upheld, must be fixed)"}, openFindingRefs(view))
}

// A chat status reads as lines, one fact per line, with no raw gate clause dump.
func TestFormatQuestionAnswerIsReadableLines(t *testing.T) {
	view := change.View{
		PR: &change.PRInfo{
			Number:  7,
			HeadSHA: id.SHA("abcdef1234567890"),
			Gate: &change.GateInfo{SHA: id.SHA("abcdef1234567890"), OK: false, Reasons: []string{
				"ci: check heimdall/acceptance has not reported on head",
				"reviews: the review requests are not known",
				"blocking: F1 is open",
			}},
		},
		Findings: map[string]change.FindingInfo{"F1": {State: finding.StateUpheld}},
	}
	got := formatQuestionAnswer(id.ChangeID("KAN-5"), view, "why is KAN-5 blocked?")
	lines := strings.Split(got, "\n")
	require.Greater(t, len(lines), 1, "the answer is one fact per line")
	require.NotContains(t, got, "blocking:")
	require.NotContains(t, got, "ci:")
	require.Contains(t, got, "Gate blocked: F1 is open")
	require.Contains(t, got, "Still waiting for: the acceptance tests to finish, the code reviews")
	require.Contains(t, got, "Open findings: F1 (upheld, must be fixed).")
}

// A question that names CI leads with the check status, before the gate.
func TestACheckQuestionLeadsWithTheCheckStatus(t *testing.T) {
	view := change.View{
		PR: &change.PRInfo{
			Number:  7,
			HeadSHA: id.SHA("abcdef1234567890"),
			CI:      map[id.SHA]map[string]change.CIConclusion{id.SHA("abcdef1234567890"): {"heimdall/acceptance": change.CIFailure}},
		},
	}
	got := formatQuestionAnswer(id.ChangeID("KAN-5"), view, "what does CI say?")
	lines := strings.Split(got, "\n")
	require.True(t, strings.HasPrefix(lines[1], "Checks on abcdef1:"), "the check status comes second, right after the headline")
	require.Contains(t, lines[1], "heimdall/acceptance: failed")
}

// A ticketless pull request is named by its own address in chat, never the pr-... stream id.
func TestStatusNamesATicketlessPullRequestReadably(t *testing.T) {
	view := change.View{PR: &change.PRInfo{Number: 15}}
	got := formatQuestionAnswer(id.ChangeID("pr-aarush323-heimdall-fixture-pay-15"), view, "status")
	require.Contains(t, got, "aarush323/heimdall-fixture-pay #15")
	require.NotContains(t, got, "pr-aarush323")
}
