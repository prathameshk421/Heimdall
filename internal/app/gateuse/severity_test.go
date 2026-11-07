package gateuse_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

// The policy's blocking.min_severity decides whether an open finding holds the merge. A finding's severity comes from
// the event it was filed with, not from a constant in the fold (TECH_SPEC 11.1, TECH_SPEC 12.4).
func TestSeverityBelowPolicyMinimumDoesNotBlock(t *testing.T) {
	out := greenOutside()
	out.PolicyAtBase.Blocking = policy.Blocking{MinSeverity: finding.SeverityMedium, MinTier: finding.TierE3}
	out.PolicyAtHead = out.PolicyAtBase

	low := greenStream(t).add(change.EventFindingFiled, systemActor, change.FindingFiled{
		FindingID: findingID, Ref: ref(1), Tier: finding.TierE2, Severity: finding.SeverityLow,
		State: finding.StateOpen, By: "reviewer",
	})
	res, _ := evaluate(t, low, out)
	require.True(t, res.OK, "a low severity finding is below the policy's medium minimum and must not block")
	require.False(t, hasClause(res.Reasons, gate.ClauseBlocking))
}

func TestSeverityAtOrAbovePolicyMinimumBlocks(t *testing.T) {
	out := greenOutside()
	out.PolicyAtBase.Blocking = policy.Blocking{MinSeverity: finding.SeverityMedium, MinTier: finding.TierE3}
	out.PolicyAtHead = out.PolicyAtBase

	high := greenStream(t).add(change.EventFindingFiled, systemActor, change.FindingFiled{
		FindingID: findingID, Ref: ref(1), Tier: finding.TierE2, Severity: finding.SeverityHigh,
		State: finding.StateOpen, By: "reviewer",
	})
	res, _ := evaluate(t, high, out)
	require.False(t, res.OK, "a high severity finding meets the policy's medium minimum and must block")
	require.True(t, hasClause(res.Reasons, gate.ClauseBlocking))
}

// With no policy known, every open E1 to E3 finding blocks whatever its severity.
func TestSeverityLowBlocksUnderTheStrictDefault(t *testing.T) {
	out := greenOutside()
	out.PolicyKnown = false

	low := greenStream(t).add(change.EventFindingFiled, systemActor, change.FindingFiled{
		FindingID: findingID, Ref: ref(1), Tier: finding.TierE2, Severity: finding.SeverityLow,
		State: finding.StateOpen, By: "reviewer",
	})
	res, _ := evaluate(t, low, out)
	require.False(t, res.OK, "with no policy known, a low finding must still block")
}
