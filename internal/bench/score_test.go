package bench

import (
	"errors"
	"math"
	"sync"
	"sync/atomic"
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

func TestTurnSummaryCountsEveryTurnAndIgnoresUnfinishedCases(t *testing.T) {
	// A run of three cases: one finished case with two turns, one finished case with no recorded turn, and one case the
	// cost cap never started. Every recorded turn counts, and the per-case mean is over the cases that ran a turn.
	cases := []CaseScore{
		{ID: "a", Scored: true, TurnSeconds: []float64{1, 3},
			ToolSeconds: []TurnToolLatency{{Turn: 1, Tool: "read_code", Seconds: 0.5}, {Turn: 2, Tool: "run_test", Seconds: 4}}},
		{ID: "b", Scored: true},
		{ID: "c", Scored: false},
	}
	s := TurnSummary(cases)
	require.Equal(t, 2, s.Turns)
	require.InDelta(t, 1, s.TurnP50, 1e-12, "nearest rank: the lower of two turns is the median")
	require.InDelta(t, 3, s.TurnP95, 1e-12)
	require.InDelta(t, 2, s.PerCase, 1e-12, "two turns over the one case that ran a turn")
	require.InDelta(t, 0.5, s.ToolP50, 1e-12, "nearest rank over two tool calls")
	require.InDelta(t, 4, s.ToolP95, 1e-12)
	require.InDelta(t, 4.5, s.ToolSecs, 1e-12, "the tool time of both calls")
}

func TestTurnSummaryOfARunWithNoTurn(t *testing.T) {
	// A run the cost cap stopped before the first call has no timing to report, not a zero that looks like a fast run.
	s := TurnSummary([]CaseScore{{ID: "a"}, {ID: "b"}})
	require.Zero(t, s.Turns)
	require.Zero(t, s.PerCase)
	require.Zero(t, s.TurnP50)
	require.Zero(t, s.ToolP95)
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

func TestCostCapHoldsUnderConcurrentReserve(t *testing.T) {
	// A parallel review asks the one shared cap for money from many workers at once. Each worker reserves the most its
	// call can cost before the call and settles the real cost after it, so the spend can never pass the cap. The bound
	// is a power of two, so the money adds up exactly and the count is not a float tolerance.
	const (
		capUSD = 1.0
		bound  = 0.0625
	)
	c, err := NewCostCap(capUSD)
	require.NoError(t, err)
	var wg sync.WaitGroup
	var admitted atomic.Int64
	var refused atomic.Int64
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 25 {
				settle, err := c.Reserve(bound)
				if err != nil {
					if errors.Is(err, ErrCostCap) {
						refused.Add(1)
					}
					continue
				}
				admitted.Add(1)
				_ = settle(bound)
			}
		}()
	}
	wg.Wait()
	require.LessOrEqual(t, c.Spent(), capUSD, "the spend never passes the cap")
	require.InDelta(t, capUSD, c.Spent(), 1e-12, "every worker spends until the cap is reached")
	require.EqualValues(t, 16, admitted.Load(), "the cap admits exactly as many calls as it can pay for")
	require.EqualValues(t, 184, refused.Load(), "every other call is refused, not dropped")
	require.InDelta(t, 0, c.Remaining(), 1e-12, "nothing is left held after every call settled")
}

func TestCostCapSettlesAReleasedCallWithoutChargingIt(t *testing.T) {
	// A call that fails is not billed: the worker settles it with zero, so the held money returns to the cap.
	c, err := NewCostCap(1)
	require.NoError(t, err)
	settle, err := c.Reserve(0.4)
	require.NoError(t, err)
	require.InDelta(t, 0.6, c.Remaining(), 1e-12, "a held call cannot be spent twice")
	require.NoError(t, settle(0))
	require.InDelta(t, 1, c.Remaining(), 1e-12, "a call that never billed returns its hold")
	require.InDelta(t, 0, c.Spent(), 1e-12)
}

func TestCostCapIsSafeUnderConcurrentAllowAndAdd(t *testing.T) {
	// Allow and Add stay the two-step pair the loop's budget uses. Many workers may hold them at once without a data
	// race, and the spend only ever grows.
	c, err := NewCostCap(1)
	require.NoError(t, err)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 25 {
				if c.Allow(0.001) == nil {
					_ = c.Add(0.001)
				}
				_ = c.Spent()
				_ = c.Remaining()
				_, _ = c.CaseBudget(0.1)
			}
		}()
	}
	wg.Wait()
	require.Positive(t, c.Spent(), "some calls were admitted")
	require.LessOrEqual(t, c.Spent(), 1.0)
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

func TestCostCapShareGivesTheFullBudgetWhenNothingIsHeld(t *testing.T) {
	c, err := NewCostCap(1)
	require.NoError(t, err)
	s, err := c.Share(0.5)
	require.NoError(t, err)
	require.Equal(t, CaseShare{Budget: 0.5}, s)
}

func TestCostCapShareMarksABudgetCutOnlyByOtherHolds(t *testing.T) {
	// Two other workers hold $0.70 between them. Only $0.30 is free, but the unspent cap ($1.00) covers a full $0.50
	// case: the holds alone cut this budget, and they return to the cap when their calls settle.
	c, err := NewCostCap(1)
	require.NoError(t, err)
	_, err = c.Reserve(0.3)
	require.NoError(t, err)
	_, err = c.Reserve(0.4)
	require.NoError(t, err)
	s, err := c.Share(0.5)
	require.NoError(t, err)
	require.InDelta(t, 0.3, s.Budget, 1e-12, "only what the other calls do not hold is free")
	require.True(t, s.Held, "the unspent cap covers a full case, so only the holds cut this budget")
}

func TestCostCapShareDoesNotBlameHoldsWhenTheCapRunsOut(t *testing.T) {
	// $0.80 is spent: the unspent $0.20 cannot cover a $0.50 case whatever the other workers hold.
	c, err := NewCostCap(1)
	require.NoError(t, err)
	require.NoError(t, c.Add(0.8))
	_, err = c.Reserve(0.05)
	require.NoError(t, err)
	s, err := c.Share(0.5)
	require.NoError(t, err)
	require.InDelta(t, 0.15, s.Budget, 1e-12)
	require.False(t, s.Held, "the cap itself is short")
}

func TestCostCapShareWithEverythingHeldByOthers(t *testing.T) {
	// Other calls hold all that is unspent, but the unspent cap covers a case: the case gets nothing now and the run
	// is not over, so this is no ErrCostCap.
	c, err := NewCostCap(1)
	require.NoError(t, err)
	_, err = c.Reserve(1)
	require.NoError(t, err)
	s, err := c.Share(0.5)
	require.NoError(t, err)
	require.Zero(t, s.Budget)
	require.True(t, s.Held)
}

func TestCostCapShareRefusesWhenTheCapCannotCoverACase(t *testing.T) {
	c, err := NewCostCap(1)
	require.NoError(t, err)
	require.NoError(t, c.Add(0.9))
	_, err = c.Reserve(0.1)
	require.NoError(t, err)
	_, err = c.Share(0.5)
	require.ErrorIs(t, err, ErrCostCap, "nothing is free and the unspent cap cannot cover a case")

	spent, err := NewCostCap(1)
	require.NoError(t, err)
	require.NoError(t, spent.Add(1))
	_, err = spent.Share(0.5)
	require.ErrorIs(t, err, ErrCostCap)

	broken, err := NewCostCap(1)
	require.NoError(t, err)
	require.Error(t, broken.Add(-1))
	_, err = broken.Share(0.5)
	require.ErrorIs(t, err, ErrCostCap)

	_, err = c.Share(0)
	require.Error(t, err)
}

func TestCostCapCoversIgnoresHolds(t *testing.T) {
	c, err := NewCostCap(1)
	require.NoError(t, err)
	_, err = c.Reserve(0.9)
	require.NoError(t, err)
	require.True(t, c.Covers(0.5), "a hold is not spend: it returns to the cap when its call settles")
	require.NoError(t, c.Add(0.6))
	require.False(t, c.Covers(0.5))
	require.Error(t, c.Add(-1))
	require.False(t, c.Covers(0), "a broken cap covers nothing")
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
