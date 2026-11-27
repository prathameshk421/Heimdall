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

func elsewhere(file string, span Span, class testintegrity.ProofClass, why testintegrity.ProofWhy) ScoredFinding {
	return ScoredFinding{File: file, Lines: span, Severity: finding.SeverityHigh, Rule: RuleRevertLines,
		Proof: testintegrity.ProofResult{Class: class, Why: why}}
}

func TestProofRuleForAFindingAwayFromTheTruthRevertsItsLines(t *testing.T) {
	truth := []FileTruth{{File: "a.go", Lines: []string{"10-12"}}}
	require.Equal(t, RuleFixCommit, ProofRuleFor(KindBug, truth, "a.go", Span{11, 14}), "on the truth: the fix commit")
	require.Equal(t, RuleRevertLines, ProofRuleFor(KindBug, truth, "a.go", Span{40, 41}), "away from it: revert the lines")
	require.Equal(t, RuleRevertLines, ProofRuleFor(KindBug, truth, "b.go", Span{11, 11}), "another file is away from it")
	require.Equal(t, RuleRevertLines, ProofRuleFor(KindBug, nil, "a.go", Span{11, 11}), "a bug case with no mapped truth")
	require.Equal(t, RuleFixCommit, ProofRuleFor(KindClean, nil, "a.go", Span{1, 1}),
		"a clean case keeps its fixed tree, the base, so a proven blocking finding stays a false positive")
}

func TestScoreCaseCountsAFindingProvenElsewhereApart(t *testing.T) {
	truth := []FileTruth{{File: "a.go", Lines: []string{"10-12"}}}
	findings := []ScoredFinding{
		elsewhere("a.go", Span{40, 41}, testintegrity.ClassProven, testintegrity.WhyProven),
		elsewhere("a.go", Span{50, 50}, testintegrity.ClassBuildError, testintegrity.WhyBuildFixed),
	}
	s := ScoreCase(KindBug, truth, findings, finding.SeverityMedium)
	require.Equal(t, 2, s.Proposed)
	require.Equal(t, 0, s.Proven, "a finding away from the truth is not the case's known bug")
	require.Equal(t, 1, s.ProvenElsewhere)
	require.False(t, s.Hit, "it does not count toward recall")
	require.False(t, s.Located)
	require.Equal(t, map[string]int{"build_error_fixed": 1}, s.Unproven)
}

func TestSummarizeCountsProvenElsewhereAsEvidenceButNotRecall(t *testing.T) {
	cases := []CaseScore{
		{Kind: KindBug, Scored: true, Proposed: 2, Proven: 0, ProvenElsewhere: 2},
		{Kind: KindBug, Scored: true, Proposed: 1, Proven: 1, Hit: true},
		{Kind: KindBug, Scored: false, Proposed: 1, ProvenElsewhere: 1},
	}
	s := Summarize(cases)
	require.Equal(t, Ratio{Num: 1, Den: 2}, s.Recall)
	require.Equal(t, 2, s.ProvenElsewhere, "only scored cases count")
	require.Equal(t, Ratio{Num: 3, Den: 3}, s.Evidence, "a finding proven elsewhere is still a proven finding")
}
