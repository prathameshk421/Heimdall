package gate_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
)

func TestVerdictOfADecision(t *testing.T) {
	fail := gate.Clause{ID: gate.ClauseCI, Reason: "check build is failure on head, want success"}
	unknown := gate.Clause{ID: gate.ClauseReviews, Reason: "the review requests are not known", Unknown: true}

	cases := []struct {
		name string
		d    gate.Decision
		want gate.Verdict
	}{
		{"nothing unmet is pass", gate.Decision{OK: true}, gate.VerdictPass},
		{"every clause known false is fail", gate.Decision{Unmet: []gate.Clause{fail, {ID: gate.ClauseApproval, Reason: "no approval"}}}, gate.VerdictFail},
		{"one clause known false is fail", gate.Decision{Unmet: []gate.Clause{fail}}, gate.VerdictFail},
		{"every clause unknown is unknown", gate.Decision{Unmet: []gate.Clause{unknown, {ID: gate.ClausePolicy, Reason: "not known", Unknown: true}}}, gate.VerdictUnknown},
		{"one clause unknown is unknown", gate.Decision{Unmet: []gate.Clause{unknown}}, gate.VerdictUnknown},
		{"a mix of fail and unknown is fail", gate.Decision{Unmet: []gate.Clause{unknown, fail}}, gate.VerdictFail},
		{"a mix in the other order is fail", gate.Decision{Unmet: []gate.Clause{fail, unknown}}, gate.VerdictFail},
		// Silence never helps: a decision that is not OK and names no clause cannot pass.
		{"not OK with no clause is fail", gate.Decision{}, gate.VerdictFail},
		// An OK flag that disagrees with the clause list cannot pass either.
		{"OK with an unmet clause is not pass", gate.Decision{OK: true, Unmet: []gate.Clause{fail}}, gate.VerdictFail},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, gate.VerdictOf(c.d))
		})
	}
}

// Evaluate never sets Unknown: its contract is unchanged, so every unmet clause it returns reads as a fail.
func TestEvaluateKeepsItsContract(t *testing.T) {
	f := green()
	f.Approvals = nil
	d := gate.Evaluate(f)
	require.False(t, d.OK)
	require.Equal(t, gate.VerdictFail, gate.VerdictOf(d))
	require.Equal(t, gate.VerdictPass, gate.VerdictOf(gate.Evaluate(green())))
}

func TestVerdictStrings(t *testing.T) {
	require.Equal(t, "pass", gate.VerdictPass.String())
	require.Equal(t, "fail", gate.VerdictFail.String())
	require.Equal(t, "unknown", gate.VerdictUnknown.String())
	require.True(t, gate.VerdictPass.Valid())
	require.False(t, gate.Verdict("maybe").Valid())
}

func TestApprovalReasonIsTheApprovalClause(t *testing.T) {
	require.Empty(t, gate.ApprovalReason(green()))
	f := green()
	f.Approvals = []gate.Approval{{Login: "rekdfr", Commit: "old", Approved: true}}
	require.Contains(t, gate.ApprovalReason(f), "approval by rekdfr is for commit old but head is abc1234")
	f.Approvals = nil
	require.Contains(t, gate.ApprovalReason(f), "no approval has been recorded")
}
