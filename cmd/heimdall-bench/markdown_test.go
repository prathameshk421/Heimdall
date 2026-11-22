package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/bench"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testintegrity"
)

func markdownReport() reviewReport {
	return reviewReport{
		Started: "2026-11-21T12:00:00Z", Model: "scripted", Command: "go test", HeimdallCommit: "abc",
		Machine: "darwin/arm64", Sandbox: sandboxNote, MaxCostUSD: 3, PerCaseUSD: 0.5, BlockSeverity: "medium",
		Turns: bench.TurnStats{Turns: 12, PerCase: 6, TurnP50: 22.5, TurnP95: 41, ToolP50: 0.4, ToolP95: 25.3, ToolSecs: 27.1},
		Cases: []reviewCase{
			{CaseScore: bench.CaseScore{ID: "a", Kind: bench.KindBug, Scored: true, Turns: 6, CostUSD: 0.002,
				TurnSeconds: []float64{20, 25, 41}, Seconds: 90}},
			{CaseScore: bench.CaseScore{ID: "b", Kind: bench.KindClean}, Error: "not run: cost cap"},
		},
		Summary: bench.Summarize([]bench.CaseScore{{ID: "a", Kind: bench.KindBug, Scored: true, CostUSD: 0.002, Seconds: 90}}),
	}
}

func TestReviewMarkdownShowsTurnTiming(t *testing.T) {
	// The table must answer "how slow is a model turn" without opening the JSON: median and p95 seconds per turn for
	// the case, and the same over the run, with the turns per case.
	md := reviewMarkdown(markdownReport())
	require.Contains(t, md, "| Turn s p50/p95 |", "the case table has a turn column")
	require.Contains(t, md, "| 25.0/41.0 |", "the case row shows the median and p95 of its turns")
	require.Contains(t, md, "| Model turns | 12 total, 6.0 per case; 22.5 s median, 41.0 s p95 |")
	require.Contains(t, md, "| Tool calls | 27.1 s total, 0.4 s median, 25.3 s p95 |")
	require.Contains(t, md, "| n/a |", "a case with no recorded turn says so")
}

func TestReviewMarkdownKeepsTheColumnCount(t *testing.T) {
	// Every row of a table keeps the column count of its header, so a Markdown renderer draws it. A row that drifts by
	// one cell hides the last column, which here is the note on why a case failed.
	md := reviewMarkdown(markdownReport())
	lines := strings.Split(md, "\n")
	for i := 0; i < len(lines)-2; i++ {
		if !strings.HasPrefix(lines[i], "| ") || !strings.Contains(lines[i+1], "---|") {
			continue
		}
		cols := strings.Count(lines[i], "|")
		for j := i + 2; j < len(lines) && strings.HasPrefix(lines[j], "|"); j++ {
			require.Equal(t, cols, strings.Count(lines[j], "|"), "row %d of the table at line %d", j-i-1, i)
		}
	}
}

func TestReviewMarkdownListsFindingsProvenElsewhere(t *testing.T) {
	// A finding proven by reverting its own lines is a real issue the bench cannot credit. The report lists it with its
	// claim, file, lines and test, so a person can judge it.
	rep := markdownReport()
	other := bench.ScoredFinding{File: "mock/mock.go", Lines: bench.Span{Start: 282, End: 283}, Severity: "high",
		Claim: "On() replaces a held mutex | badly", TestPath: "mock/zz_test.go", TestName: "TestOnKeepsTheMutex",
		Rule: bench.RuleRevertLines, Proof: testintegrity.ProofResult{Class: testintegrity.ClassProven}}
	rep.Cases[0].Findings = []bench.ScoredFinding{other}
	rep.Cases[0].ProvenElsewhere = 1
	rep.Summary.ProvenElsewhere = 1
	md := reviewMarkdown(rep)
	require.Contains(t, md, "| Proven elsewhere (a proven issue away from the known bug; not in recall) | 1 |")
	require.Contains(t, md, "| a | mock/mock.go:282-283 | high | `mock/zz_test.go` TestOnKeepsTheMutex | On() replaces a held mutex \\| badly |")
}
