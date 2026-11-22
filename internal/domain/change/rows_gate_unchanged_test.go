package change_test

import (
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/stretchr/testify/require"
)

// TestGateRowDoesNotEmitSecondCheckWhenResultIsUnchanged confirms P7: once gate.evaluated matches the current
// EvaluateGate result for the PR head, rowGate emits no new fact or github_check job.
func TestGateRowDoesNotEmitSecondCheckWhenResultIsUnchanged(t *testing.T) {
	t.Parallel()
	head := id.SHA("1111111111111111111111111111111111111111")
	base := id.SHA("2222222222222222222222222222222222222222")
	in := change.In{
		Change: "KAN-7",
		View: change.View{
			Room: "room-7",
			PR: &change.PRInfo{
				Repo:    "org/repo",
				Number:  7,
				HeadSHA: head,
				Base:    base,
				Author:  "alice",
				Ticket:  "KAN-7",
			},
		},
	}

	gateRow := findRowByID(t, change.Rows(), "gate")
	first := gateRow.Next(in)
	require.Len(t, first, 2, "initial evaluation emits gate.evaluated fact and github_check job")

	eval, ok := first[0].Fact.(change.GateEvaluated)
	require.True(t, ok)

	// Record that gate result in the view; an unrelated fact that does not change EvaluateGate emits nothing.
	in.View.PR.Gate = &change.GateInfo{SHA: eval.SHA, OK: eval.OK, Reasons: eval.Reasons}
	in.View.PR.GateCount = 1
	second := gateRow.Next(in)
	require.Empty(t, second, "unchanged gate must not post a second github_check job")
}
