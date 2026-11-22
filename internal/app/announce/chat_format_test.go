package announce_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/announce"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

// Switch posts chat text verbatim, so a line must carry no Markdown that a person would see as literal "**" or "`".
func TestChatLinesCarryNoRawMarkdown(t *testing.T) {
	history := []change.Event{
		ev(t, change.EventPROpened, change.PROpened{Repo: "aarush323/heimdall-fixture-pay", Number: 15}),
		ev(t, change.EventFindingFiled, change.FindingFiled{Ref: 1, Severity: "high"}),
	}
	for _, e := range []change.Event{
		ev(t, change.EventPROpened, change.PROpened{Repo: "aarush323/heimdall-fixture-pay", Number: 15}),
		ev(t, change.EventCIReported, change.CIReported{Check: "heimdall/acceptance", SHA: "abcdef1234567", Conclusion: "success"}),
		ev(t, change.EventCIReported, change.CIReported{Check: "heimdall/acceptance", SHA: "abcdef1234567", Conclusion: "failure"}),
		ev(t, change.EventGateEvaluated, change.GateEvaluated{OK: true}),
		ev(t, change.EventGateEvaluated, change.GateEvaluated{OK: false, Reasons: []string{"blocking: F1 is open"}}),
		ev(t, change.EventChangeMerged, struct{}{}),
		ev(t, change.EventApprovalRecorded, change.ApprovalRecorded{Login: "rekdfr"}),
		ev(t, change.EventReviewersPicked, change.ReviewersPicked{People: []change.ActorID{"ana", "bob"}}),
	} {
		line, ok := narrate(t, history, e)
		require.True(t, ok, e.Type)
		require.NotContains(t, line.Text, "**", e.Type)
		require.NotContains(t, line.Text, "`", e.Type)
	}
}

// A ticketless pull request is named by its own address in chat, never the pr-... stream id it is stored under.
func TestAChatLineNamesATicketlessPullRequestReadably(t *testing.T) {
	const prc = "pr-aarush323-heimdall-fixture-pay-15"
	e := change.Event{
		ID:       "ev-ci",
		ChangeID: prc,
		Type:     change.EventCIReported,
		Payload:  []byte(`{"check":"heimdall/acceptance","sha":"abcdef1234567","conclusion":"success"}`),
	}
	line, ok := announce.Narrate(nil, e)
	require.True(t, ok)
	require.Contains(t, line.Text, "aarush323/heimdall-fixture-pay #15")
	require.NotContains(t, line.Text, "pr-aarush323")
}

// The CI line carries the pull request link when the history has one.
func TestACILineCarriesThePullRequestLink(t *testing.T) {
	history := []change.Event{ev(t, change.EventPROpened, change.PROpened{Repo: "aarush323/heimdall-fixture-pay", Number: 15})}
	for _, conclusion := range []change.CIConclusion{change.CISuccess, change.CIFailure} {
		line, ok := narrate(t, history, ev(t, change.EventCIReported, change.CIReported{
			Check: "heimdall/acceptance", SHA: "abcdef1234567", Conclusion: conclusion,
		}))
		require.True(t, ok)
		require.Contains(t, line.Text, "https://github.com/aarush323/heimdall-fixture-pay/pull/15")
	}
}

// An agent's progress line is one short line, so internal chatter cannot flood a team room.
func TestAProgressLineIsShort(t *testing.T) {
	long := strings.Repeat("I am reading the code and thinking about the change. ", 20)
	line, ok := narrate(t, nil, ev(t, change.EventTaskProgress, change.TaskProgress{Text: "KAN-14: " + long, Role: "test-author"}))
	require.True(t, ok)
	require.LessOrEqual(t, len(line.Text), 220, line.Text)
	require.NotContains(t, line.Text, "\n")
}
