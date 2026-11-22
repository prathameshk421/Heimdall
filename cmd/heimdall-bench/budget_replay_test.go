//go:build benchreplay

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// The fixes for live run 4 on a real case: the context budget and the stall rule, through the whole review pipeline
// (real repository, real tools, real go test) with a scripted model.

// readingScript is a testify-1207 script that lists the changes and then reads mock/mock.go 60 lines at a time, n
// times, and never proposes a finding.
func readingScript(t *testing.T, n int) script {
	t.Helper()
	turns := []string{`{"calls":[{"name":"list_changes","arguments":{}}],"usage":{"input_tokens":1000,"output_tokens":50},"cost_usd":0.0001}`}
	for i := range n {
		turns = append(turns, fmt.Sprintf(`{"calls":[{"name":"read_code","arguments":{"path":"mock/mock.go","start_line":%d,"end_line":%d}}],"usage":{"input_tokens":1000,"output_tokens":50},"cost_usd":0.0001}`,
			i*60+1, i*60+60))
	}
	var s script
	require.NoError(t, json.Unmarshal([]byte(`{"case":"testify-1207","turns":[`+strings.Join(turns, ",")+`]}`), &s))
	return s
}

func TestReplayTestify1207UnderTheContextBudgetAndTheStallRule(t *testing.T) {
	// The recorded script is short, so its requests stay small: the budget changes nothing it needs, the stall rule
	// never fires (it proposes on turn 5), and the case is proven and hit as before.
	o := replayOptions(t, "testify-1207", testingCapUSD)
	o.contextBytes, o.stallTurns = defaultContextBytes, defaultStallTurns
	model := loadScripts(t, "testify-1207")
	code, err := reviewRun(context.Background(), o, execRunner{}, model, testWriter{t})
	require.NoError(t, err)
	require.Equal(t, exitAgrees, code)

	mock := readReport(t, o.out).Cases[0]
	require.True(t, mock.Scored, mock.Error)
	require.Equal(t, ports.AgentStopDone, mock.Stop)
	require.Equal(t, 1, mock.Proven)
	require.True(t, mock.Hit)
	for i, turn := range mock.Transcript {
		require.Positive(t, turn.RequestBytes, "turn %d records its request size", i+1)
		require.LessOrEqual(t, turn.RequestBytes, defaultContextBytes, "turn %d", i+1)
		require.False(t, turn.StallNudged)
	}
}

func TestReplayTestify1207ReadingAloneIsTrimmedNudgedAndStopped(t *testing.T) {
	// A reviewer that only reads, as four t3code cases did in live run 4. With a 16 KB budget the old reads become
	// stubs and every request after the trim stays near the budget; after 10 answers with no test run or proposal the
	// loop nudges once, and after 4 more it stops the case as no_proposal before a 15th call.
	const budget = 16 << 10
	o := replayOptions(t, "testify-1207", testingCapUSD)
	o.contextBytes, o.stallTurns = budget, defaultStallTurns
	model := &scriptedModel{scripts: []script{readingScript(t, 30)}, byRepo: map[string]int{}, turn: map[int]int{}}
	m, err := loadManifest(o.manifest)
	require.NoError(t, err)
	for _, c := range m.Cases {
		if c.ID == "testify-1207" {
			model.byRepo[c.RepoSlug()] = 0
		}
	}
	code, err := reviewRun(context.Background(), o, execRunner{}, model, testWriter{t})
	require.NoError(t, err)
	require.Equal(t, exitAgrees, code, "a case that stopped for no proposal is scored: it found nothing")

	mock := readReport(t, o.out).Cases[0]
	require.Equal(t, ports.AgentStopNoProposal, mock.Stop)
	require.True(t, mock.Scored, mock.Error)
	require.Zero(t, mock.Proposed)
	require.False(t, mock.Hit)
	require.Len(t, mock.Transcript, defaultStallTurns+stallGraceTurns)
	for i, turn := range mock.Transcript {
		require.Equal(t, i == defaultStallTurns-1, turn.StallNudged, "turn %d: one nudge, after the tenth answer", i+1)
	}
	trimmed := false
	for i, turn := range mock.Transcript {
		require.Positive(t, turn.RequestBytes)
		if turn.Trimmed > 0 {
			trimmed = true
			// The three newest reads stay whole (60 lines of mock.go each), so the request may pass the budget by
			// about that much, never by the whole history.
			require.LessOrEqual(t, turn.RequestBytes, budget+24<<10, "turn %d", i+1)
		}
	}
	require.True(t, trimmed, "a 14-turn read of mock.go passes 16 KB, so old results are trimmed")
	last := mock.Transcript[len(mock.Transcript)-1]
	t.Logf("last request %d bytes with %d results trimmed", last.RequestBytes, last.Trimmed)
}
