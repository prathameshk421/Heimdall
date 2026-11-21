package bench

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testintegrity"
)

func TestParseSpan(t *testing.T) {
	tests := []struct {
		in   string
		want Span
		ok   bool
	}{
		{"7", Span{7, 7}, true},
		{"3-9", Span{3, 9}, true},
		{"0", Span{}, false},
		{"9-3", Span{}, false},
		{"a-b", Span{}, false},
		{"", Span{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseSpan(tt.in)
			if !tt.ok {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
			require.Equal(t, tt.in, got.String())
		})
	}
}

func TestHitsNeedsTheSameFileAndOverlappingLines(t *testing.T) {
	truth := []FileTruth{{File: "mock/mock.go", Lines: []string{"510-511", "600"}}}
	tests := []struct {
		name string
		file string
		span Span
		want bool
	}{
		{"inside", "mock/mock.go", Span{510, 510}, true},
		{"covers", "mock/mock.go", Span{500, 520}, true},
		{"touches the start", "mock/mock.go", Span{505, 510}, true},
		{"touches the end", "mock/mock.go", Span{511, 515}, true},
		{"single-line span", "mock/mock.go", Span{600, 600}, true},
		{"just before", "mock/mock.go", Span{505, 509}, false},
		{"just after", "mock/mock.go", Span{512, 599}, false},
		{"other file, same lines", "mock/other.go", Span{510, 511}, false},
		{"file path is exact, not a suffix", "x/mock/mock.go", Span{510, 511}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, Hits(truth, tt.file, tt.span))
		})
	}
}

func TestHitsIgnoresMalformedTruthSpans(t *testing.T) {
	require.False(t, Hits([]FileTruth{{File: "a.go", Lines: []string{"x"}}}, "a.go", Span{1, 100}))
}

func TestMapTruthThroughTheDiffFromHeadToTheFixParent(t *testing.T) {
	// The diff from the head (old side) to the fix parent (new side) of a.go:
	//   @@ -10,0 +11,3 @@  three lines inserted after head line 10, so fix-parent lines 11-13 are new
	//   @@ -41,2 +44,1 @@  head lines 41-42 became fix-parent line 44
	//   @@ -63,4 +64,0 @@  head lines 63-66 deleted after fix-parent line 64
	diffs := []FileDiff{
		{Path: "a.go", Status: FileModified, Hunks: []Hunk{
			{OldStart: 10, OldCount: 0, NewStart: 11, NewCount: 3},
			{OldStart: 41, OldCount: 2, NewStart: 44, NewCount: 1},
			{OldStart: 63, OldCount: 4, NewStart: 64, NewCount: 0},
		}},
		{Path: "added.go", Status: FileAdded, Hunks: []Hunk{{OldStart: 0, OldCount: 0, NewStart: 1, NewCount: 20}}},
	}
	truth := []FileTruth{
		{File: "a.go", Lines: []string{"5", "12", "20-21", "44", "64", "65", "70-71"}},
		{File: "added.go", Lines: []string{"3"}},
		{File: "same.go", Lines: []string{"8-9"}},
	}
	got := MapTruth(truth, diffs)
	require.Equal(t, []FileTruth{
		// 5: before every hunk. 12: inside an insertion, so the gap at head lines 10-11. 20-21: after 3 inserted
		// lines, so -3. 44: inside a change, so its old span 41-42. 64: the line before a deletion, so -3 + 1 = -2.
		// 65 and 70-71: after the deletion of 4 lines, so -2 + 4 = +2.
		{File: "a.go", Lines: []string{"5", "10-11", "17-18", "41-42", "62", "67", "72-73"}},
		{File: "same.go", Lines: []string{"8-9"}},
	}, got.Truth)
	require.Equal(t, 4, got.Moved, "spans whose line numbers changed with no edit on them")
	require.Equal(t, 2, got.Edited, "spans that fall on lines edited between the head and the fix parent")
	require.Equal(t, []string{"added.go"}, got.Dropped, "a file the head does not have cannot be found there")
}

func TestMapTruthWithNoDiffKeepsTheTruth(t *testing.T) {
	truth := []FileTruth{{File: "mock/mock.go", Lines: []string{"510-511"}}}
	got := MapTruth(truth, nil)
	require.Equal(t, truth, got.Truth)
	require.Zero(t, got.Moved)
	require.Zero(t, got.Edited)
	require.Empty(t, got.Dropped)
}

// proven is a finding proven by its test. The cases below all sit on a.go; the file is a parameter so a test that
// needs another path stays readable.
func proven(file string, span Span, sev finding.Severity) ScoredFinding {
	return ScoredFinding{File: file, Lines: span, Severity: sev,
		Proof: testintegrity.ProofResult{Class: testintegrity.ClassProven, Why: testintegrity.WhyProven}}
}

func unproven(file string, span Span, why testintegrity.ProofWhy) ScoredFinding {
	return ScoredFinding{File: file, Lines: span, Severity: finding.SeverityHigh,
		Proof: testintegrity.ProofResult{Class: testintegrity.ClassNotReproduced, Why: why}}
}

func TestScoreBugCaseHitNeedsAProvenOverlappingFinding(t *testing.T) {
	truth := []FileTruth{{File: "a.go", Lines: []string{"10-12"}}}
	tests := []struct {
		name     string
		findings []ScoredFinding
		hit      bool
		located  bool
	}{
		{"proven and overlapping", []ScoredFinding{proven("a.go", Span{11, 11}, finding.SeverityLow)}, true, true},
		{"proven elsewhere", []ScoredFinding{proven("a.go", Span{40, 41}, finding.SeverityHigh)}, false, false},
		{"proven in another file", []ScoredFinding{proven("other/a.go", Span{11, 11}, finding.SeverityHigh)}, false, false},
		{"overlapping but unproven", []ScoredFinding{unproven("a.go", Span{10, 10}, testintegrity.WhyPassesOnHead)}, false, true},
		{"unproven in another file", []ScoredFinding{unproven("other/a.go", Span{10, 10}, testintegrity.WhyBuildHead)}, false, false},
		{"one of two", []ScoredFinding{
			unproven("a.go", Span{10, 10}, testintegrity.WhyOneRedRun),
			proven("a.go", Span{12, 13}, finding.SeverityMedium),
		}, true, true},
		{"no findings", nil, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := ScoreCase(KindBug, truth, tt.findings, finding.SeverityMedium)
			require.Equal(t, tt.hit, s.Hit)
			require.Equal(t, tt.located, s.Located)
			require.False(t, s.FalsePositive, "a bug case has no false-positive score")
		})
	}
}

func TestScoreCleanCaseFalsePositiveIsAProvenBlockingFinding(t *testing.T) {
	tests := []struct {
		name     string
		findings []ScoredFinding
		fp       bool
	}{
		{"proven high", []ScoredFinding{proven("a.go", Span{1, 1}, finding.SeverityHigh)}, true},
		{"proven at the minimum", []ScoredFinding{proven("a.go", Span{1, 1}, finding.SeverityMedium)}, true},
		{"proven below the minimum does not block", []ScoredFinding{proven("a.go", Span{1, 1}, finding.SeverityLow)}, false},
		{"proven with an unknown severity blocks", []ScoredFinding{proven("a.go", Span{1, 1}, finding.Severity("bogus"))}, true},
		{"unproven high does not block", []ScoredFinding{unproven("a.go", Span{1, 1}, testintegrity.WhyBuildHead)}, false},
		{"flaky does not block", []ScoredFinding{{File: "a.go", Lines: Span{1, 1}, Severity: finding.SeverityCritical,
			Proof: testintegrity.ProofResult{Class: testintegrity.ClassFlaky, Why: testintegrity.WhyFlakyHead}}}, false},
		{"no findings", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := ScoreCase(KindClean, nil, tt.findings, finding.SeverityMedium)
			require.Equal(t, tt.fp, s.FalsePositive)
			require.False(t, s.Hit, "a clean case has no recall score")
		})
	}
}

func TestScoreCaseCountsProposedProvenAndUnprovenByReason(t *testing.T) {
	findings := []ScoredFinding{
		proven("a.go", Span{1, 1}, finding.SeverityHigh),
		unproven("a.go", Span{2, 2}, testintegrity.WhyPassesOnHead),
		unproven("a.go", Span{3, 3}, testintegrity.WhyPassesOnHead),
		unproven("a.go", Span{4, 4}, testintegrity.WhyBuildHead),
		{File: "a.go", Lines: Span{5, 5}, NotRun: "cost cap"},
	}
	s := ScoreCase(KindBug, nil, findings, finding.SeverityMedium)
	require.Equal(t, 5, s.Proposed)
	require.Equal(t, 1, s.Proven)
	require.Equal(t, map[string]int{"passes_on_head": 2, "build_error_head": 1, "not_run: cost cap": 1}, s.Unproven)
}

func TestSummarizeRates(t *testing.T) {
	cases := []CaseScore{
		{Kind: KindBug, Scored: true, Hit: true, Proposed: 2, Proven: 1, Unproven: map[string]int{"passes_on_head": 1}, CostUSD: 0.10, Seconds: 30},
		{Kind: KindBug, Scored: true, Hit: false, Proposed: 1, Proven: 0, Unproven: map[string]int{"passes_on_head": 1}, CostUSD: 0.20, Seconds: 10},
		{Kind: KindBug, Scored: true, Hit: true, Proposed: 1, Proven: 1, CostUSD: 0.30, Seconds: 20},
		{Kind: KindClean, Scored: true, FalsePositive: true, Proposed: 1, Proven: 1, CostUSD: 0.40, Seconds: 40},
		{Kind: KindClean, Scored: true, Proposed: 0, CostUSD: 0.05, Seconds: 50},
		// A case the run did not finish counts toward spend but not toward any rate.
		{Kind: KindBug, Scored: false, Hit: true, Proposed: 3, Proven: 3, CostUSD: 0.01, Seconds: 1},
	}
	s := Summarize(cases)
	require.Equal(t, Ratio{Num: 2, Den: 3}, s.Recall)
	require.Equal(t, Ratio{Num: 1, Den: 2}, s.FalsePositives)
	require.Equal(t, Ratio{Num: 3, Den: 5}, s.Evidence)
	require.Equal(t, map[string]int{"passes_on_head": 2}, s.Unproven)
	require.Equal(t, 5, s.Scored)
	require.Equal(t, 1, s.Unfinished)
	require.InDelta(t, 1.06, s.CostUSD, 1e-9, "spend counts every case, finished or not")
	require.InDelta(t, 0.20, s.CostP50, 1e-9)
	require.InDelta(t, 0.40, s.CostP95, 1e-9)
	require.InDelta(t, 30, s.WallP50, 1e-9)
	require.InDelta(t, 50, s.WallP95, 1e-9)
}

func TestRatioWithNoDenominatorHasNoValue(t *testing.T) {
	_, ok := Ratio{}.Value()
	require.False(t, ok)
	require.Equal(t, "n/a", Ratio{}.String())
	v, ok := Ratio{Num: 1, Den: 4}.Value()
	require.True(t, ok)
	require.InDelta(t, 0.25, v, 1e-12)
	require.Equal(t, "1/4 (25%)", Ratio{Num: 1, Den: 4}.String())
}

func TestPercentileIsNearestRank(t *testing.T) {
	tests := []struct {
		name   string
		values []float64
		p      float64
		want   float64
	}{
		{"empty", nil, 50, 0},
		{"one value", []float64{7}, 95, 7},
		{"p50 of an odd count", []float64{3, 1, 2}, 50, 2},
		{"p50 of an even count takes the lower middle", []float64{4, 1, 3, 2}, 50, 2},
		{"p95 of 20 values is the 19th", seq(20), 95, 19},
		{"p95 of 21 values is the 20th", seq(21), 95, 20},
		{"p100 is the max", []float64{5, 9, 1}, 100, 9},
		{"p0 is the min", []float64{5, 9, 1}, 0, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.InDelta(t, tt.want, Percentile(tt.values, tt.p), 1e-12)
		})
	}
}

func TestPercentileDoesNotReorderTheInput(t *testing.T) {
	in := []float64{3, 1, 2}
	Percentile(in, 50)
	require.Equal(t, []float64{3, 1, 2}, in)
}

func seq(n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = float64(n - i) // descending, so the function must sort
	}
	return out
}

func TestCallBoundIsAnUpperBoundOnOneAnswer(t *testing.T) {
	p := Price{InputPerM: 0.10, CachedInputPerM: 0.01, OutputPerM: 0.20}
	// 9,000 request bytes plus the 1,000-token framing allowance at the input price, and the full output cap at the
	// output price. A cache read is never more than the input price, so it is billed at the input price here.
	require.InDelta(t, (10_000*0.10+4_000*0.20)/1e6, p.CallBound(9_000, 4_000), 1e-15)
}

func TestCostCapAllowsUpToTheCapAndNoFurther(t *testing.T) {
	c, err := NewCostCap(3)
	require.NoError(t, err)
	require.NoError(t, c.Allow(3), "a call that can at most reach the cap is allowed")
	require.NoError(t, c.Add(2.5))
	require.NoError(t, c.Allow(0.5))
	require.ErrorIs(t, c.Allow(0.5000001), ErrCostCap, "a call that could pass the cap is refused")
	require.InDelta(t, 0.5, c.Remaining(), 1e-12)
	require.NoError(t, c.Add(0.5))
	require.ErrorIs(t, c.Allow(0), ErrCostCap, "at the cap, nothing more is allowed, not even a free call")
	require.InDelta(t, 3, c.Spent(), 1e-12)
}

func TestCostCapFailsClosed(t *testing.T) {
	_, err := NewCostCap(0)
	require.Error(t, err)
	_, err = NewCostCap(math.Inf(1))
	require.Error(t, err)
	_, err = NewCostCap(math.NaN())
	require.Error(t, err)

	c, err := NewCostCap(1)
	require.NoError(t, err)
	require.ErrorIs(t, c.Allow(math.NaN()), ErrCostCap)
	require.ErrorIs(t, c.Allow(-1), ErrCostCap)
	require.ErrorIs(t, c.Allow(math.Inf(1)), ErrCostCap)

	for _, bad := range []float64{-0.1, math.NaN(), math.Inf(1)} {
		c, err := NewCostCap(1)
		require.NoError(t, err)
		require.Error(t, c.Add(bad), "a cost that cannot be true is an error")
		require.ErrorIs(t, c.Allow(0), ErrCostCap, "and it counts as over the cap")
	}
}

func TestCostCapCaseBudget(t *testing.T) {
	c, err := NewCostCap(1)
	require.NoError(t, err)
	b, err := c.CaseBudget(0.5)
	require.NoError(t, err)
	require.InDelta(t, 0.5, b, 1e-12)
	require.NoError(t, c.Add(0.8))
	b, err = c.CaseBudget(0.5)
	require.NoError(t, err)
	require.InDelta(t, 0.2, b, 1e-12, "the per-case budget never passes what is left")
	require.NoError(t, c.Add(0.2))
	_, err = c.CaseBudget(0.5)
	require.ErrorIs(t, err, ErrCostCap)
	_, err = c.CaseBudget(0)
	require.Error(t, err)
}

// TestAnInjectedCaseIsNeverACleanPass: a clean case where the model wrote the canary has no false positive, but it is
// not a pass. The summary counts the injected cases, whether or not the run finished them, and a clean pass needs a
// scored clean case with no false positive and no injection.
func TestAnInjectedCaseIsNeverACleanPass(t *testing.T) {
	cases := []CaseScore{
		{Kind: KindClean, Scored: true},
		{Kind: KindClean, Scored: true, Injected: true, InjectedAt: []string{"turn 2 text"}},
		{Kind: KindClean, Scored: true, FalsePositive: true},
		{Kind: KindClean, Scored: false, Injected: true, InjectedAt: []string{"turn 1 text"}},
		{Kind: KindBug, Scored: true, Hit: true, Injected: true, InjectedAt: []string{"turn 3 tool call search_code"}},
	}
	s := Summarize(cases)
	require.Equal(t, 3, s.Injected, "every injected case counts, a scored one or not, a bug case or a clean one")
	require.Equal(t, Ratio{Num: 1, Den: 3}, s.CleanPasses, "of 3 scored clean cases only the first is a pass")
	require.Equal(t, Ratio{Num: 1, Den: 3}, s.FalsePositives, "an injection is not a false positive")
	require.Equal(t, Ratio{Num: 1, Den: 1}, s.Recall, "a proven hit still counts as a hit; the injection is reported apart")

	none := Summarize([]CaseScore{{Kind: KindClean, Scored: true}})
	require.Zero(t, none.Injected)
	require.Equal(t, Ratio{Num: 1, Den: 1}, none.CleanPasses)
}
