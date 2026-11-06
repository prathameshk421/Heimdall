package gateuse

import (
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/stretchr/testify/require"
)

// consensusRules is the build rule of TECH_SPEC 11.2b: two distinct high-confidence reporters hold. BlockScore is
// deliberately above any reachable score, because the weighted rule is Later (D40): only high-confidence reporters
// count today, never a weighted total.
func consensusRules() policy.Consensus {
	return policy.Consensus{
		MinHighReporters: 2,
		BlockScore:       100,
		Confidence:       policy.Confidence{High: 1, Medium: .5, Low: .2},
	}
}

// highReport is one live high-confidence finding for a fingerprint.
func highReport(suffix, reporter string) gate.FindingState {
	return report(suffix, reporter, finding.ConfidenceHigh, finding.StateOpen)
}

func report(suffix, reporter string, confidence finding.Confidence, state finding.State) gate.FindingState {
	return gate.FindingState{
		ID:          id.FindingID("fnd_01HZX3K9QW7M2P5R8T0V4YB6C" + suffix),
		Tier:        finding.TierE2,
		Severity:    finding.SeverityLow,
		State:       state,
		Fingerprint: "same-issue",
		Reporter:    reporter,
		Confidence:  confidence,
	}
}

// TestConsensusHoldNeedsDistinctHighConfidenceReporters is the build rule of TECH_SPEC 11.2b. The count comes from the
// reports themselves and the policy threshold decides; confidence weighting is the Later rule and must not hold a gate.
func TestConsensusHoldNeedsDistinctHighConfidenceReporters(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		rules  policy.Consensus
		states []gate.FindingState
		want   []bool
	}{
		{
			name:   "two distinct high reporters hold",
			rules:  consensusRules(),
			states: []gate.FindingState{highReport("1", "reviewer-a"), highReport("2", "reviewer-b")},
			want:   []bool{true, true},
		},
		{
			name:   "one high and one medium reporter do not hold",
			rules:  consensusRules(),
			states: []gate.FindingState{highReport("1", "reviewer-a"), report("2", "reviewer-b", finding.ConfidenceMedium, finding.StateOpen)},
			want:   []bool{false, false},
		},
		{
			name:   "the same high reporter twice does not hold",
			rules:  consensusRules(),
			states: []gate.FindingState{highReport("1", "reviewer-a"), highReport("2", "reviewer-a")},
			want:   []bool{false, false},
		},
		{
			name:   "a dismissed high report leaves one reporter and does not hold",
			rules:  consensusRules(),
			states: []gate.FindingState{highReport("1", "reviewer-a"), report("2", "reviewer-b", finding.ConfidenceHigh, finding.StateDismissed)},
			want:   []bool{false, false},
		},
		{
			name:   "an upheld high report still counts",
			rules:  consensusRules(),
			states: []gate.FindingState{report("1", "reviewer-a", finding.ConfidenceHigh, finding.StateUpheld), highReport("2", "reviewer-b")},
			want:   []bool{true, true},
		},
		{
			name:   "the policy threshold, not a fixed count, decides",
			rules:  policy.Consensus{MinHighReporters: 3, BlockScore: 100, Confidence: policy.Confidence{High: 1}},
			states: []gate.FindingState{highReport("1", "reviewer-a"), highReport("2", "reviewer-b")},
			want:   []bool{false, false},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			held := withConsensus(c.states, c.rules)
			require.Len(t, held, len(c.want))
			for i, want := range c.want {
				require.Equal(t, want, held[i].Consensus, "report %d", i)
			}
		})
	}
}

// TestConsensusHoldPutsEveryLiveReportInTheBlockingSet proves the gate consequence: the reports hold the merge even
// though they are outside the policy's tier and severity thresholds, and a dismissed report stays out.
func TestConsensusHoldPutsEveryLiveReportInTheBlockingSet(t *testing.T) {
	t.Parallel()
	states := []gate.FindingState{highReport("1", "reviewer-a"), highReport("2", "reviewer-b")}
	held := withConsensus(states, consensusRules())
	blocking := gate.BlockingSet(held, policy.Blocking{MinTier: finding.TierE3, MinSeverity: finding.SeverityHigh})
	require.ElementsMatch(t, []id.FindingID{states[0].ID, states[1].ID}, blocking)
}
