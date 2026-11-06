package gateuse_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

// Verification events are written by the server. They must not erase who reported a finding or its consensus facts:
// two high-confidence reviewers on one fingerprint hold the merge even after the server verified both (A2).
func TestVerificationDoesNotEraseConsensusFacts(t *testing.T) {
	const fp = "fp-same-claim"
	fnd := []id.FindingID{"fnd_01HZX3K9QW7M2P5R8T0V4YB6CD", "fnd_01HZX3K9QW7M2P5R8T0V4YB6CE"}
	refs := []id.FindingRef{ref(1), ref(2)}
	filed := func(i int, by change.ActorID) change.FindingFiled {
		return change.FindingFiled{FindingID: fnd[i], Ref: refs[i], Tier: finding.TierE2, Severity: finding.SeverityHigh,
			State: finding.StateOpen, By: by, Fingerprint: fp, Confidence: finding.ConfidenceHigh}
	}
	verified := func(i int) change.FindingVerified {
		return change.FindingVerified{FindingID: fnd[i], Ref: refs[i], Tier: finding.TierE2, Severity: finding.SeverityHigh,
			State: finding.StateOpen, By: "heimdall"}
	}
	out := greenOutside()
	out.PolicyAtBase = policy.Policy{Version: 1, Workflow: "default",
		Blocking:  policy.Blocking{MinSeverity: finding.SeverityHigh, MinTier: finding.TierE1},
		Consensus: policy.Consensus{MinHighReporters: 2, BlockScore: 100, Confidence: policy.Confidence{High: 1, Medium: .5, Low: .2}}}
	out.PolicyAtHead = out.PolicyAtBase
	s := greenStream(t).
		add(change.EventFindingFiled, systemActor, filed(0, "ana-reviewer")).
		add(change.EventFindingFiled, systemActor, filed(1, "eli-reviewer")).
		add(change.EventFindingVerified, systemActor, verified(0)).
		add(change.EventFindingVerified, systemActor, verified(1))
	res, _ := evaluate(t, s, out)
	require.False(t, res.OK, "two independent high-confidence reports of one claim hold the merge after verification")
	require.True(t, hasClause(res.Reasons, gate.ClauseBlocking))
}
