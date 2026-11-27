package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// The offline analysis behind the stall defaults (AM12g). It reads the recorded live runs and prints, per case, the
// turn of the first run_test, the turn of the first propose_finding, the total turns, the stop, and the longest
// streak of answers with no progress call before the first proposal (the count the stall rule keeps). Print it with:
//
//	CGO_ENABLED=0 go test -count=1 -v -run TestStallTable ./cmd/heimdall-bench

// stallRuns are the recorded live runs with a transcript. Runs 3 and 4 had no stall rule, so they show when the model
// commits when nothing cuts it off; runs 5a and 5b ran with the old 10+4 rule.
var stallRuns = []struct {
	name, file string
	ruleFree   bool
}{
	{"3", "bench-live-3-2026-11-21.json", true},
	{"4", "bench-live-4-2026-11-22.json", true},
	{"5a", "bench-live-5a-2026-11-22.json", false},
	{"5b", "bench-live-5b-2026-11-22.json", false},
}

// stallRow is one case of one recorded run. A turn of 0 means the run never made that call.
type stallRow struct {
	run, id      string
	ruleFree     bool
	firstTest    int
	firstPropose int
	turns        int
	stop         string
	proposed     int
	// streakBeforePropose is the longest run of answers with no progress call before the first proposal, or in the
	// whole run when it never proposed. streakBeforeTest is the same count before the first run_test.
	streakBeforePropose int
	streakBeforeTest    int
}

// stallRowOf reads one case's transcript the way the loop's stall counter does (agentloop stall.go): an answer with
// a call to a progress tool resets the count, any other answer adds one.
func stallRowOf(run string, ruleFree bool, c recordedCase) stallRow {
	r := stallRow{run: run, id: c.ID, ruleFree: ruleFree, turns: len(c.Transcript), stop: c.Stop, proposed: c.Proposed}
	since := 0
	for i, turn := range c.Transcript {
		names := make([]string, 0, len(turn.Calls))
		for _, call := range turn.Calls {
			names = append(names, call.Name)
		}
		if r.firstTest == 0 && slices.Contains(names, "run_test") {
			r.firstTest = i + 1
		}
		if r.firstPropose == 0 && slices.Contains(names, "propose_finding") {
			r.firstPropose = i + 1
		}
		if slices.Contains(names, "run_test") || slices.Contains(names, "propose_finding") {
			since = 0
			continue
		}
		since++
		if r.firstPropose == 0 {
			r.streakBeforePropose = max(r.streakBeforePropose, since)
		}
		if r.firstTest == 0 {
			r.streakBeforeTest = max(r.streakBeforeTest, since)
		}
	}
	return r
}

// recordedCase is the part of a recorded case the analysis reads.
type recordedCase struct {
	ID         string `json:"id"`
	Stop       string `json:"stop"`
	Proposed   int    `json:"proposed"`
	Transcript []struct {
		Calls []struct {
			Name string `json:"name"`
		} `json:"calls"`
	} `json:"transcript"`
}

func loadStallRows(t *testing.T) []stallRow {
	t.Helper()
	var rows []stallRow
	for _, run := range stallRuns {
		b, err := os.ReadFile(filepath.Join("..", "..", "docs", "sota", run.file))
		require.NoError(t, err)
		var rep struct {
			Cases []recordedCase `json:"cases"`
		}
		require.NoError(t, json.Unmarshal(b, &rep), run.file)
		for _, c := range rep.Cases {
			if len(c.Transcript) > 0 {
				rows = append(rows, stallRowOf(run.name, run.ruleFree, c))
			}
		}
	}
	return rows
}

// TestStallDefaultsInterruptLongReadingRuns holds the tightened stall defaults (AM17) to the recorded data: runs that
// read for 12 answers without a test or proposal now get the diagnostic nudge, and a run that only reads is stopped at
// 18, well before the 40-turn cap. The old 18+6 rule let those runs read for 24 turns; the tightening removes that.
func TestStallDefaultsInterruptLongReadingRuns(t *testing.T) {
	const margin, beforeCap = 4, 10
	require.Equal(t, 12, defaultStallTurns)
	require.Equal(t, 6, stallGraceTurns)
	stopAt := defaultStallTurns + stallGraceTurns
	require.Equal(t, 18, stopAt)
	var nudged, earlyProposal int
	for _, r := range loadStallRows(t) {
		if !r.ruleFree {
			continue
		}
		if r.streakBeforeTest >= defaultStallTurns {
			nudged++
		}
		if r.firstPropose > 0 && r.streakBeforePropose+margin <= defaultStallTurns {
			earlyProposal++
		}
	}
	require.Positive(t, nudged, "recorded runs that read for 12 answers justify the diagnostic nudge")
	require.Positive(t, earlyProposal, "a run that proposes early still gets no nudge")
	require.LessOrEqual(t, stopAt+beforeCap, maxTurnsPerCase, "a run that only reads must stop well before the cap")
}
