package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/bench"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

var errTestRun = errors.New("no result")

func TestProgressLinesAreOneShortStableLineEach(t *testing.T) {
	// A live run is silent for minutes at a time, so these lines are how an operator knows it is alive. Each line is
	// one line with stable fields, so a run can be read and grepped while it is still going.
	var b strings.Builder
	p := newProgress(&b)
	cp := newCaseProgress(p, "testify-1207")
	cp.startCase(bench.KindBug)
	cp.modelTurn(benchTurn{tools: []string{"read_code", "run_test"}, input: 1200, output: 80, cost: 0.0012, seconds: 21.5},
		0.5, 30_000_000_000)
	cp.testStart("3c33e07c")
	cp.testEnd("3c33e07c", ports.SandboxResult{Passed: 3, Failed: 1}, nil, 25.3)
	cp.end(reviewCase{CaseScore: bench.CaseScore{ID: "testify-1207", Kind: bench.KindBug, Scored: true,
		CostUSD: 0.0028, Hit: true}}, 1.5)

	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	require.Len(t, lines, 5)
	require.Equal(t, "bench case=testify-1207 start kind=bug", lines[0])
	require.Equal(t, "bench case=testify-1207 turn=1 tool=read_code,run_test in=1200 out=80 case$=0.0012 run$=0.5000 30.0s", lines[1])
	require.Equal(t, "bench case=testify-1207 test=3c33e07c start", lines[2])
	require.Equal(t, "bench case=testify-1207 test=3c33e07c done pass=3 fail=1 errored=0 25.3s", lines[3])
	require.True(t, strings.HasPrefix(lines[4], "bench case=testify-1207 end scored=yes verdict=hit cost$=0.0028 run$="))
	for _, line := range lines {
		require.Less(t, len(line), 120, "a progress line stays short: %s", line)
	}
}

func TestProgressRoundsALatencyToMillis(t *testing.T) {
	// A scripted model answers in a fraction of a millisecond. The results file and the table read better in
	// milliseconds, and the percentile of a fast turn is then a real number rather than 1.209e-06.
	require.InDelta(t, 0.025, roundSeconds(0.0254), 1e-12)
	require.Zero(t, roundSeconds(0.0004), "a turn faster than half a millisecond is zero, not a fraction")
	require.InDelta(t, 25.657, roundSeconds(25.6574), 1e-12)
	require.InDelta(t, 1.958, roundSeconds(1.9581), 1e-12)
}

func TestProgressNamesTheToolsOfAnAnswer(t *testing.T) {
	// An answer with no tool call is a stop the operator must see, so the field is "-" and never empty.
	require.Equal(t, "-", toolNames(nil))
	require.Equal(t, "list_changes", toolNames([]string{"list_changes"}))
	require.Equal(t, "a,b", toolNames([]string{"a", "b"}))
}

func TestProgressReportsAFailedTestRun(t *testing.T) {
	// A run that produced no result at all must say so. A zero count would read as a fast pass.
	var b strings.Builder
	cp := newCaseProgress(newProgress(&b), "case")
	cp.testEnd("abc", ports.SandboxResult{}, errTestRun, 1.5)
	require.Contains(t, b.String(), "done error=yes 1.5s")
}

func TestProgressToANilWriterIsSilent(t *testing.T) {
	require.NotPanics(t, func() {
		cp := newCaseProgress(newProgress(nil), "case")
		cp.startCase(bench.KindClean)
		cp.end(reviewCase{}, 0)
	})
}

func TestVerdictOfACase(t *testing.T) {
	// One word says what the case found. A bug the reviewer located but could not prove is not a hit, and a clean case
	// that stayed quiet is not a false positive.
	tests := []struct {
		rc   reviewCase
		want string
	}{
		{reviewCase{CaseScore: bench.CaseScore{Kind: bench.KindBug, Scored: true, Hit: true}}, "hit"},
		{reviewCase{CaseScore: bench.CaseScore{Kind: bench.KindBug, Scored: true, Located: true}}, "located"},
		{reviewCase{CaseScore: bench.CaseScore{Kind: bench.KindBug, Scored: true}}, "miss"},
		{reviewCase{CaseScore: bench.CaseScore{Kind: bench.KindClean, Scored: true, FalsePositive: true}}, "false_positive"},
		{reviewCase{CaseScore: bench.CaseScore{Kind: bench.KindClean, Scored: true}}, "clean"},
		{reviewCase{CaseScore: bench.CaseScore{Kind: bench.KindBug}, Error: "model error"}, "error"},
		{reviewCase{CaseScore: bench.CaseScore{Kind: bench.KindBug}}, "unscored"},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, verdictOf(tt.rc))
	}
}
