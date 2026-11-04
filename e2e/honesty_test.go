package e2e_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/e2e"
)

// A scenario whose steps all pass but whose scenario-level quality check fails must not look green anywhere.
func TestAQualityViolationMakesTheBoardNotGreen(t *testing.T) {
	chatty := e2e.Step{ID: "P2.1", Run: func(_ context.Context, e *e2e.Env) error {
		e.Capture.AddMessages(e2e.Message{ID: "1", Room: "r", From: "bob-reviewer", FromAgent: true, Kind: e2e.KindProgress, Text: "Reading file 3 of 9"})
		return nil
	}}
	// The cost is measured (scripted $0), so the only thing that can fail strict is the quality violation.
	sc := runOne(t, &fakeWorld{spend: []float64{0, 0}}, story(), e2e.Scenario{ID: "P2", Steps: []e2e.Step{chatty}, Budget: e2e.Budget{Milestones: 5}})
	require.Equal(t, e2e.StatusPass, sc.Results[0].Status, "the step itself passed")
	require.NotEmpty(t, sc.Quality, "the scenario's quiet check must have failed")

	totals := e2e.Summarize([]e2e.ScenarioResult{sc})
	require.Equal(t, 1, totals.QualityViolations)
	require.Equal(t, 0, totals.CostUnmeasured+totals.Unmeasured, "nothing else may be the reason strict fails")
	require.True(t, totals.StrictFailure(), "a quality violation must fail -strict even when every step passed")
	require.Contains(t, totals.Line(), "1 quality violation")
}

func TestUnmeasuredScenarioChecksAreCountedAndFailStrict(t *testing.T) {
	sc := e2e.ScenarioResult{ID: "P2", Results: []e2e.Result{{Step: "P2.1", Status: e2e.StatusPass, CostKnown: true}},
		Unmeasured: []string{"idempotent replay: the poller replay is not built yet (T26)"}}
	totals := e2e.Summarize([]e2e.ScenarioResult{sc})
	require.Equal(t, 1, totals.Unmeasured)
	require.True(t, totals.StrictFailure())
	require.Contains(t, totals.Line(), "1 unmeasured")
}

func TestAllGreenWithMeasuredQualityIsNotAStrictFailure(t *testing.T) {
	sc := e2e.ScenarioResult{ID: "P2", Results: []e2e.Result{{Step: "P2.1", Status: e2e.StatusPass, CostKnown: true}}}
	totals := e2e.Summarize([]e2e.ScenarioResult{sc})
	require.False(t, totals.StrictFailure())
}

// An unmeasured cost fails strict on its own: every step passed, nothing else is unmeasured, and no quality check
// failed.
func TestAnUnmeasuredCostFailsStrictOnItsOwn(t *testing.T) {
	sc := e2e.ScenarioResult{ID: "P2", Results: []e2e.Result{{Step: "P2.1", Status: e2e.StatusPass}}}
	totals := e2e.Summarize([]e2e.ScenarioResult{sc})
	require.Equal(t, 1, totals.CostUnmeasured)
	require.Equal(t, 0, totals.Unmeasured+totals.QualityViolations)
	require.True(t, totals.StrictFailure())
}

func TestDemoBeatIsNotRealWhileItsScenarioHasAQualityViolation(t *testing.T) {
	results := []e2e.ScenarioResult{{
		ID: "P1",
		Results: []e2e.Result{
			{Step: "P1.1", Status: e2e.StatusPass},
			{Step: "P1.2", Status: e2e.StatusPass},
		},
		Quality: []e2e.Violation{{Check: e2e.CheckQuiet, Detail: "progress chatter"}},
	}}
	got := e2e.DemoCheck(results)
	require.NotEqual(t, e2e.StatusPass, got[0].Status, "1:10 must not be claimed while P1 has a quality violation")
	require.Contains(t, got[0].Why, "quality")
	require.Equal(t, 1, e2e.Summarize(results).QualityViolations)
	require.Equal(t, "demo beats 0/9 real", e2e.DemoLine(got)[:len("demo beats 0/9 real")], "the line must not count it")
}

func TestAPanickingStepIsRedWithTheValueAndTheRunGoesOn(t *testing.T) {
	boom := e2e.Step{ID: "P3.1", Run: func(context.Context, *e2e.Env) error { panic("nil map in the room") }}
	sc := runOne(t, &fakeWorld{}, story(), e2e.Scenario{ID: "P3", Steps: []e2e.Step{boom, okStep("P3.2")}})
	require.Len(t, sc.Results, 2, "the scenario must keep running after a panic")
	require.Equal(t, e2e.StatusRed, sc.Results[0].Status)
	require.Contains(t, sc.Results[0].Why, "nil map in the room")
	require.Equal(t, e2e.StatusPass, sc.Results[1].Status)
}

func TestAStepThatOverrunsItsBarIsRedAndNotAnInternalError(t *testing.T) {
	slow := e2e.Step{ID: "P3.1", Bar: 20 * time.Millisecond, Run: func(ctx context.Context, _ *e2e.Env) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	r := runOne(t, &fakeWorld{}, story(), e2e.Scenario{ID: "P3", Steps: []e2e.Step{slow}}).Results[0]
	require.Equal(t, e2e.StatusRed, r.Status)
	require.Contains(t, r.Why, "within 20ms")
	require.NotContains(t, r.Why, "deadline exceeded")
}

func TestAStepWithNoBarStillHasABoardMaximum(t *testing.T) {
	hang := e2e.Step{ID: "P3.1", Run: func(ctx context.Context, _ *e2e.Env) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	b := &e2e.Board{World: &fakeWorld{}, Story: story(), Now: ticker(time.Millisecond), Scripted: true, MaxStep: 20 * time.Millisecond}
	r := b.Run(context.Background(), []e2e.Scenario{{ID: "P3", Steps: []e2e.Step{hang}}})[0].Results[0]
	require.Equal(t, e2e.StatusRed, r.Status, "a hung step must end red, not hang the board")
	require.Contains(t, r.Why, "within 20ms")
}

func TestASpendErrorThatIsNotNotBuiltIsAQualityViolation(t *testing.T) {
	w := &fakeWorld{spendErr: errors.New("spend API returned 500")}
	r := runOne(t, w, story(), e2e.Scenario{ID: "P2", Steps: []e2e.Step{okStep("P2.2")}}).Results[0]
	require.Equal(t, e2e.StatusRed, r.Status, "a broken cost reading must not pass as scripted $0")
	require.Contains(t, r.Why, "cannot read the spend")
	require.Len(t, r.Violations, 1)
}

func TestASpendNotBuiltIsRecordedAsUnmeasuredNotDropped(t *testing.T) {
	r := runOne(t, &fakeWorld{}, story(), e2e.Scenario{ID: "P2", Steps: []e2e.Step{okStep("P2.2")}}).Results[0]
	require.Equal(t, e2e.StatusPass, r.Status)
	require.Len(t, r.Unmeasured, 1)
	require.Contains(t, r.Unmeasured[0], "T19")
}

// A manual step is blocked on the manual need and is never green. The guarded spec test in e2e/scenarios asserts
// that every step is blocked when nothing is built, so the manual status is not made reachable here; see the report.
func TestAManualStepIsBlockedOnManualAndNeverGreen(t *testing.T) {
	man := e2e.Step{ID: "P9.3", Needs: []e2e.Need{e2e.NeedManual}, Run: func(context.Context, *e2e.Env) error { return nil }}
	w := &fakeWorld{missing: map[e2e.Need]string{e2e.NeedManual: "a person must do this step by hand"}}
	r := runOne(t, w, story(), e2e.Scenario{ID: "P9", Steps: []e2e.Step{man}}).Results[0]
	require.Equal(t, e2e.StatusBlocked, r.Status)
	require.Equal(t, e2e.NeedManual, r.BlockedOn)
	require.Equal(t, 0, e2e.Summarize([]e2e.ScenarioResult{{ID: "P9", Results: []e2e.Result{r}}}).Green)
}

func TestRawInternalsCatchesGoAndInfraErrorText(t *testing.T) {
	for _, text := range []string{
		"dial tcp 127.0.0.1:8000: connection refused",
		"Post http://x: context deadline exceeded",
		"sql: no rows in result set",
		"pq: relation \"runs\" does not exist",
		"panic: runtime error: index out of range",
		"goroutine 17 [running]",
		"invalid memory address: nil pointer dereference",
		"runtime error: invalid memory address",
	} {
		require.NotEmpty(t, e2e.RawInternals(text), "should flag: %s", text)
	}
}
