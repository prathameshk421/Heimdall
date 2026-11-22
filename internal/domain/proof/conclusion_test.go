package proof_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
)

func outcomes() map[string]proof.Outcome {
	return map[string]proof.Outcome{
		"skipped":        {Kind: proof.KindSkippedTrivial},
		"proven":         {Kind: proof.KindProven},
		"blocked":        {Kind: proof.KindBlocked},
		"not reproduced": {Kind: proof.KindNotReproduced},
		"unknown":        {Kind: proof.KindUnknown, Reason: proof.ReasonBudget},
	}
}

// TestConclusionTable is the proof half of the table in ACTION_MODE 5.4.
func TestConclusionTable(t *testing.T) {
	const (
		shadow   = policy.ActionModeShadow
		enforce  = policy.ActionModeEnforce
		neutral  = proof.ConclusionNeutral
		success  = proof.ConclusionSuccess
		failure  = proof.ConclusionFailure
		required = proof.ConclusionActionRequired
	)
	type cell struct {
		mode string
		req  bool
	}
	want := map[cell]map[string]proof.Conclusion{
		{shadow, false}:  {"skipped": neutral, "proven": neutral, "blocked": neutral, "not reproduced": neutral, "unknown": neutral},
		{shadow, true}:   {"skipped": neutral, "proven": neutral, "blocked": neutral, "not reproduced": neutral, "unknown": neutral},
		{"", false}:      {"skipped": neutral, "proven": neutral, "blocked": neutral, "not reproduced": neutral, "unknown": neutral},
		{enforce, false}: {"skipped": success, "proven": success, "blocked": neutral, "not reproduced": neutral, "unknown": neutral},
		{enforce, true}:  {"skipped": success, "proven": success, "blocked": failure, "not reproduced": required, "unknown": required},
	}
	for c, row := range want {
		for name, o := range outcomes() {
			t.Run(c.mode+"/"+name, func(t *testing.T) {
				a := policy.Action{Mode: c.mode, ProofRequired: c.req}
				require.Equal(t, row[name], o.Conclusion(a))
			})
		}
	}
}

func TestConclusionNeverSucceedsWithoutAPass(t *testing.T) {
	for _, mode := range []string{"", policy.ActionModeShadow, policy.ActionModeEnforce, "enforced", "ENFORCE"} {
		for _, req := range []bool{false, true} {
			for name, o := range outcomes() {
				got := o.Conclusion(policy.Action{Mode: mode, ProofRequired: req})
				if got == proof.ConclusionSuccess {
					require.Equal(t, "pass", o.Verdict().String(), "%s in %q required=%v", name, mode, req)
				}
			}
			for _, o := range []proof.Outcome{{}, {Kind: "invented"}} {
				require.NotEqual(t, proof.ConclusionSuccess, o.Conclusion(policy.Action{Mode: mode, ProofRequired: req}))
			}
		}
	}
}

func TestConclusionOfAnUnrecognisedModeBlocks(t *testing.T) {
	// An unrecognised mode is not shadow: shadow blocks nothing, and a typo must not switch the gate off.
	bad := policy.Action{Mode: "enforced"}
	require.Equal(t, proof.ConclusionActionRequired, outcomes()["unknown"].Conclusion(bad))
	require.Equal(t, proof.ConclusionActionRequired, outcomes()["not reproduced"].Conclusion(bad))
	require.Equal(t, proof.ConclusionFailure, outcomes()["blocked"].Conclusion(bad))
	require.Equal(t, proof.ConclusionSuccess, outcomes()["proven"].Conclusion(bad))
}

func TestHeadlines(t *testing.T) {
	cases := []struct {
		o    proof.Outcome
		want string
	}{
		{proof.Outcome{Kind: proof.KindSkippedTrivial}, "skipped: trivial (rule)"},
		{proof.Outcome{Kind: proof.KindProven}, "proven"},
		{proof.Outcome{Kind: proof.KindBlocked}, "blocked: a proven finding blocks the merge"},
		{proof.Outcome{Kind: proof.KindNotReproduced}, "not reproduced: no test failed on base and passed on head"},
		{proof.Outcome{Kind: proof.KindUnknown, Reason: proof.ReasonBudget}, "unknown: budget hit"},
		{proof.Outcome{Kind: proof.KindUnknown, Reason: proof.ReasonMonthlyCap}, "unknown: monthly cap reached"},
		{proof.Outcome{Kind: proof.KindUnknown, Reason: proof.ReasonDeadline}, "unknown: timeout"},
		{proof.Outcome{Kind: proof.KindUnknown, Reason: proof.ReasonSuperseded}, "unknown: superseded"},
		{proof.Outcome{Kind: proof.KindUnknown, Reason: proof.ReasonForkNoKey}, "unknown: fork without key"},
		{proof.Outcome{Kind: proof.KindUnknown, Reason: proof.ReasonCannotIsolate}, "unknown: runner cannot isolate PR code"},
		{proof.Outcome{Kind: proof.KindUnknown, Reason: proof.ReasonInputMissing}, "unknown: input missing"},
		{proof.Outcome{Kind: proof.KindUnknown, Reason: proof.ReasonWorkflowChanged}, "unknown: workflow changed"},
		{proof.Outcome{Kind: proof.KindUnknown, Reason: proof.ReasonStateUnverified}, "unknown: state failed verification"},
		{proof.Outcome{Kind: proof.KindUnknown, Reason: proof.ReasonSandbox}, "unknown: sandbox setup or dependency fetch failed"},
		{proof.Outcome{Kind: proof.KindUnknown, Reason: proof.ReasonNoIntent}, "unknown: no intent"},
		{proof.Outcome{Kind: proof.KindUnknown, Reason: proof.ReasonPlanRefused}, "unknown: test plan refused"},
		{proof.Outcome{Kind: proof.KindUnknown, Reason: proof.ReasonModelFailure}, "unknown: model failure"},
		{proof.Outcome{Kind: proof.KindUnknown, Reason: proof.ReasonNoResult}, "unknown: no result"},
		{proof.Outcome{Kind: proof.KindUnknown, Reason: proof.ReasonTurnCap}, "unknown: turn cap reached"},
		{proof.Outcome{Kind: proof.KindNotReproduced, Note: proof.ReasonNoProposal},
			"not reproduced: no test failed on base and passed on head (no proposal before the stall limit)"},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, tc.o.Headline())
	}
}

func TestEveryStopReasonIsAKnownReason(t *testing.T) {
	for _, r := range proof.StopReasons() {
		require.True(t, r.Valid(), r)
		require.True(t, r.CanStop(), r)
		require.NotEmpty(t, r.Text(), r)
	}
	require.False(t, proof.ReasonTrivial.CanStop())
	require.False(t, proof.ReasonNotProvable.CanStop())
	require.True(t, proof.ReasonTurnCap.CanStop())
	require.False(t, proof.ReasonNoProposal.CanStop(), "no proposal makes a run not reproduced, never unknown")
	require.True(t, proof.ReasonNoProposal.Valid())
	require.Equal(t, []proof.Reason{proof.ReasonNoProposal}, proof.NoteReasons())
	require.False(t, proof.Reason("").Valid())
}
