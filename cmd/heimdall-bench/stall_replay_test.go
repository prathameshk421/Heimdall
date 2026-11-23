//go:build benchreplay

package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// TestReplayFixtureRefundsReachesItsLateProposalUnderTheStallRule replays the shape of fixture-refunds in live run 3
// (testdata/bench/scripts/fixture-refunds.json): 12 answers that only read and search, the first run_test on turn 13,
// both proposals on turn 18 and finish_review on turn 19. Under the tightened 12+6 rule the long read streak gets one
// diagnostic nudge after answer 12; the scripted reviewer complies with a test on turn 13, reaches both proposals,
// proves them and hits the truth. Under run 5's 10+4 rule it got a weaker nudge after answer 10 (AM12g).
func TestReplayFixtureRefundsReachesItsLateProposalUnderTheStallRule(t *testing.T) {
	o := replayOptions(t, "fixture-refunds", testingCapUSD)
	o.contextBytes, o.stallTurns = defaultContextBytes, defaultStallTurns
	model := loadScripts(t, "fixture-refunds")
	code, err := reviewRun(context.Background(), o, execRunner{}, model, testWriter{t})
	require.NoError(t, err)
	require.Equal(t, exitAgrees, code)

	refunds := readReport(t, o.out).Cases[0]
	require.True(t, refunds.Scored, refunds.Error)
	require.Equal(t, ports.AgentStopDone, refunds.Stop)
	require.Len(t, refunds.Transcript, 19, "the run reaches its proposals on turn 18 and finishes on turn 19")
	require.Equal(t, 2, refunds.Proposed)
	require.Equal(t, 2, refunds.Proven, "both tests fail twice on the head and pass on the fix")
	require.True(t, refunds.Hit)
	nudges := 0
	for i, turn := range refunds.Transcript {
		if !turn.StallNudged {
			continue
		}
		nudges++
		require.Equal(t, defaultStallTurns, i+1, "one diagnostic nudge, after the 12-reading streak")
	}
	require.Equal(t, 1, nudges, "the long read streak gets exactly one nudge, then the reviewer tests its suspect")
}
