package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
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

func turnText(n int) string {
	if n == 0 {
		return "-"
	}
	return fmt.Sprint(n)
}

func TestStallTable(t *testing.T) {
	rows := loadStallRows(t)
	require.NotEmpty(t, rows)
	var b strings.Builder
	fmt.Fprintf(&b, "\n%-4s %-22s %9s %12s %6s %-12s %8s %14s %12s\n", "run", "case", "first_test", "first_propose",
		"turns", "stop", "proposed", "streak_propose", "streak_test")
	for _, r := range rows {
		fmt.Fprintf(&b, "%-4s %-22s %9s %12s %6d %-12s %8d %14d %12d\n", r.run, r.id, turnText(r.firstTest),
			turnText(r.firstPropose), r.turns, r.stop, r.proposed, r.streakBeforePropose, r.streakBeforeTest)
	}
	t.Log(b.String())
}

// TestStallDefaultsFitTheRecordedRuns holds the stall defaults to the data. In the runs with no stall rule (3 and 4):
// a run that proposed never gets the nudge before its first proposal, with at least 4 turns to spare, and no run is
// stopped before its first run_test. A run that only reads still stops at least 10 turns before the 40-turn cap.
func TestStallDefaultsFitTheRecordedRuns(t *testing.T) {
	const margin, beforeCap = 4, 10
	stopAt := defaultStallTurns + stallGraceTurns
	for _, r := range loadStallRows(t) {
		if !r.ruleFree {
			continue
		}
		if r.firstPropose > 0 {
			require.LessOrEqual(t, r.streakBeforePropose+margin, defaultStallTurns,
				"run %s %s: %d answers with no progress before its first proposal; the nudge would come too soon",
				r.run, r.id, r.streakBeforePropose)
		}
		require.Less(t, r.streakBeforeTest, stopAt,
			"run %s %s: %d answers before its first run_test; the stall rule would stop it first", r.run, r.id,
			r.streakBeforeTest)
	}
	require.LessOrEqual(t, stopAt+beforeCap, maxTurnsPerCase, "a run that only reads must stop well before the cap")
}

func TestStallRowCountsTheLoopsWay(t *testing.T) {
	// Turns: read, read, test, read, read, read, propose, read. The streak before the proposal is 3 (turns 4 to 6),
	// not 5, because the test on turn 3 reset the count; the streak before the test is 2.
	var c recordedCase
	require.NoError(t, json.Unmarshal([]byte(`{"id":"x","stop":"done","proposed":1,"transcript":[
		{"calls":[{"name":"read_code"}]},{"calls":[{"name":"search_code"},{"name":"read_code"}]},
		{"calls":[{"name":"read_code"},{"name":"run_test"}]},{"calls":[{"name":"read_code"}]},{"calls":[]},
		{"calls":[{"name":"read_code"}]},{"calls":[{"name":"propose_finding"}]},{"calls":[{"name":"read_code"}]}]}`), &c))
	r := stallRowOf("t", true, c)
	require.Equal(t, 3, r.firstTest)
	require.Equal(t, 7, r.firstPropose)
	require.Equal(t, 8, r.turns)
	require.Equal(t, 3, r.streakBeforePropose)
	require.Equal(t, 2, r.streakBeforeTest)
}
