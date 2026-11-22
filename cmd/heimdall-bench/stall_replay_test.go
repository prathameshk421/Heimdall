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
// both proposals on turn 18 and finish_review on turn 19. Under the stall defaults the run gets no nudge, reaches both
// proposals, proves them and hits the truth. Under run 5's 10+4 rule it got the nudge after answer 10 (AM12g).
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
	for i, turn := range refunds.Transcript {
		require.False(t, turn.StallNudged, "turn %d: a run that proposed in run 3 gets no stall nudge", i+1)
	}
}
