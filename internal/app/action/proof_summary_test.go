package action_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/action"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// The proof job's own summary (AM14e). The job is green whenever the step wrote its result, so the summary must say
// that green is not a pass, and in shadow mode it says whether enforce mode would block and why.

const greenLine = "Green means this job ran and wrote its result. It does not mean the change passed."

func TestTheStepSummarySaysGreenIsNotAPass(t *testing.T) {
	outcomes := []proof.Outcome{
		{Kind: proof.KindSkippedTrivial},
		{Kind: proof.KindProven},
		{Kind: proof.KindBlocked, Counts: proof.Counts{In: 1, Proven: 1, Blocking: 1}},
		{Kind: proof.KindNotReproduced, Note: proof.ReasonNoProposal},
		{Kind: proof.KindUnknown, Reason: proof.ReasonTurnCap},
	}
	for _, o := range outcomes {
		for _, mode := range []string{policy.ActionModeShadow, policy.ActionModeEnforce} {
			got := action.ProofStepSummary(action.StepSummaryIn{Number: 19, Outcome: o, Action: policy.Action{Mode: mode}})
			require.Contains(t, got, greenLine, "%s in %s", o.Kind, mode)
			require.Contains(t, got, o.Headline(), "%s in %s", o.Kind, mode)
		}
	}
}

func TestTheStepSummaryShowsTheOutcomeAndWhatShadowWouldBlock(t *testing.T) {
	turnCap := action.StepSummaryIn{Number: 19, Outcome: proof.Outcome{Kind: proof.KindUnknown, Reason: proof.ReasonTurnCap},
		Stop: ports.AgentStopTurns, Turns: 40, CostUSD: 0.2603,
		Action: policy.Action{Mode: policy.ActionModeShadow, ProofRequired: true}}
	got := action.ProofStepSummary(turnCap)
	require.Contains(t, got, "#19")
	require.Contains(t, got, "`turn_cap`")
	require.Contains(t, got, "40")
	require.Contains(t, got, "`turns`")
	require.Contains(t, got, "$0.2603")
	require.Contains(t, got, "Would block because: unknown: turn cap reached")

	advisory := turnCap
	advisory.Action.ProofRequired = false
	got = action.ProofStepSummary(advisory)
	require.NotContains(t, got, "Would block because")
	require.Contains(t, got, "Would not block")

	proven := turnCap
	proven.Outcome = proof.Outcome{Kind: proof.KindProven}
	require.NotContains(t, action.ProofStepSummary(proven), "Would block")

	stalled := turnCap
	stalled.Outcome = proof.Outcome{Kind: proof.KindNotReproduced, Note: proof.ReasonNoProposal}
	got = action.ProofStepSummary(stalled)
	require.Contains(t, got, "`no_proposal`")
	require.Contains(t, got, "Would block because: not reproduced")

	enforce := turnCap
	enforce.Action.Mode = policy.ActionModeEnforce
	got = action.ProofStepSummary(enforce)
	require.NotContains(t, got, "Would block", "enforce mode states the check, not a would")
	require.Contains(t, got, "`action_required`")
	require.Equal(t, 1, strings.Count(got, greenLine))
}
