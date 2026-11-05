package e2e_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/e2e"
)

func okStep(id string) e2e.Step {
	return e2e.Step{ID: id, Run: func(context.Context, *e2e.Env) error { return nil }}
}

func story() *e2e.Story {
	return &e2e.Story{Name: "t", Canaries: []string{"CANARY-1"}}
}

// ticker is a clock that advances by step on every Now call, so elapsed time is deterministic.
func ticker(step time.Duration) func() time.Time {
	t := time.Date(2026, 11, 5, 14, 0, 0, 0, time.UTC)
	return func() time.Time { t = t.Add(step); return t }
}

func runOne(t *testing.T, w *fakeWorld, st *e2e.Story, sc e2e.Scenario) e2e.ScenarioResult {
	t.Helper()
	b := &e2e.Board{World: w, Story: st, Now: ticker(time.Second), Scripted: true}
	return b.Run(context.Background(), []e2e.Scenario{sc})[0]
}

func TestAPendingStoryBlocksEveryStepOnTheTaskThatFinishesIt(t *testing.T) {
	st := story()
	st.Pending = "T31"
	ran := false
	sc := e2e.Scenario{ID: "P1", Steps: []e2e.Step{{ID: "P1.1", Run: func(context.Context, *e2e.Env) error { ran = true; return nil }}}}
	r := runOne(t, &fakeWorld{}, st, sc).Results[0]
	require.False(t, ran, "a step must not run on an incomplete story")
	require.Equal(t, e2e.StatusBlocked, r.Status)
	require.Equal(t, e2e.Need("story"), r.BlockedOn)
	require.Equal(t, "T31", r.Task)
	require.Contains(t, r.Why, "T31")
}

func TestAMissingPrerequisiteBlocksWithItsReasonAndDoesNotRunTheStep(t *testing.T) {
	ran := false
	sc := e2e.Scenario{ID: "P1", Steps: []e2e.Step{{
		ID: "P1.1", Needs: []e2e.Need{e2e.NeedServer, e2e.NeedForward},
		Run: func(context.Context, *e2e.Env) error { ran = true; return nil },
	}}}
	w := &fakeWorld{missing: map[e2e.Need]string{e2e.NeedForward: "nothing listens on 127.0.0.1:8000"}}
	r := runOne(t, w, story(), sc).Results[0]
	require.False(t, ran)
	require.Equal(t, e2e.StatusBlocked, r.Status)
	require.Equal(t, e2e.NeedForward, r.BlockedOn)
	require.Contains(t, r.Why, "127.0.0.1:8000")
}

func TestRedCarriesExpectedAndSaw(t *testing.T) {
	sc := e2e.Scenario{ID: "P3", Steps: []e2e.Step{{ID: "P3.1", Run: func(context.Context, *e2e.Env) error {
		return &e2e.Red{Expected: "3 tasks assigned", Saw: "0"}
	}}}}
	r := runOne(t, &fakeWorld{}, story(), sc).Results[0]
	require.Equal(t, e2e.StatusRed, r.Status)
	require.Equal(t, "expected 3 tasks assigned, saw 0", r.Why)
}

func TestAnUnexpectedErrorIsRedNotGreen(t *testing.T) {
	sc := e2e.Scenario{ID: "P3", Steps: []e2e.Step{{ID: "P3.1", Run: func(context.Context, *e2e.Env) error { return errors.New("connection refused") }}}}
	r := runOne(t, &fakeWorld{}, story(), sc).Results[0]
	require.Equal(t, e2e.StatusRed, r.Status)
	require.Contains(t, r.Why, "connection refused")
}

func TestADriverThatIsNotBuiltBlocksOnItsTask(t *testing.T) {
	sc := e2e.Scenario{ID: "P1", Steps: []e2e.Step{{ID: "P1.1", Run: func(context.Context, *e2e.Env) error {
		return &e2e.NotBuiltError{Task: "T36", What: "the Jira driver"}
	}}}}
	r := runOne(t, &fakeWorld{}, story(), sc).Results[0]
	require.Equal(t, e2e.StatusBlocked, r.Status)
	require.Equal(t, "T36", r.Task)
	require.Contains(t, r.Why, "Jira driver")
}

func TestADependentStepIsNotReachedWhenItsPredecessorFailed(t *testing.T) {
	failing := e2e.Step{ID: "P1.1", Run: func(context.Context, *e2e.Env) error { return &e2e.Red{Expected: "a", Saw: "b"} }}
	dependent := okStep("P1.2")
	dependent.After = "P1.1"
	independent := okStep("P1.3")
	got := runOne(t, &fakeWorld{}, story(), e2e.Scenario{ID: "P1", Steps: []e2e.Step{failing, dependent, independent}}).Results
	require.Equal(t, e2e.StatusRed, got[0].Status)
	require.Equal(t, e2e.StatusNotReached, got[1].Status)
	require.Contains(t, got[1].Why, "P1.1")
	require.Equal(t, e2e.StatusPass, got[2].Status, "a step that does not depend on the failed one still runs")
}

func TestAMissingPrerequisiteWinsOverAFailedPredecessor(t *testing.T) {
	failing := e2e.Step{ID: "P1.1", Run: func(context.Context, *e2e.Env) error { return &e2e.Red{Expected: "a", Saw: "b"} }}
	dependent := okStep("P1.2")
	dependent.After = "P1.1"
	dependent.Needs = []e2e.Need{e2e.NeedLive}
	w := &fakeWorld{missing: map[e2e.Need]string{e2e.NeedLive: "AGENTS=scripted"}}
	got := runOne(t, w, story(), e2e.Scenario{ID: "P1", Steps: []e2e.Step{failing, dependent}}).Results
	require.Equal(t, e2e.StatusBlocked, got[1].Status, "every step shows its own blocker, so the board is honest about what is missing")
}

func TestAStepOverItsLatencyBarIsRedWithTheNumber(t *testing.T) {
	slow := okStep("P2.1")
	slow.Bar = 500 * time.Millisecond // the test clock advances one second per reading
	r := runOne(t, &fakeWorld{}, story(), e2e.Scenario{ID: "P2", Steps: []e2e.Step{slow}}).Results[0]
	require.Equal(t, e2e.StatusRed, r.Status)
	require.Contains(t, r.Why, "within 500ms")
	require.Contains(t, r.Why, "1s")
}

func TestAQualityViolationInWhatAPersonSawFailsTheStep(t *testing.T) {
	leaky := e2e.Step{ID: "P1.2", Run: func(_ context.Context, e *e2e.Env) error {
		e.Capture.AddMessages(e2e.Message{ID: "m1", Room: "kan-1", From: "heimdall", FromAgent: true, Kind: e2e.KindMilestone, Text: "Change is now IN_REVIEW (fnd_01HZX3K9QW7M2P5R8T0V4YB6CD)"})
		return nil
	}}
	r := runOne(t, &fakeWorld{}, story(), e2e.Scenario{ID: "P1", Steps: []e2e.Step{leaky}}).Results[0]
	require.Equal(t, e2e.StatusRed, r.Status)
	require.Contains(t, r.Why, "human words")
	require.NotEmpty(t, r.Violations)
}

func TestAnEmptyGateReasonFailsTheStep(t *testing.T) {
	st := e2e.Step{ID: "P3.5", Run: func(_ context.Context, e *e2e.Env) error {
		e.Capture.AddGate(e2e.GateState{State: "failure", Summary: "Not ready."})
		return nil
	}}
	r := runOne(t, &fakeWorld{}, story(), e2e.Scenario{ID: "P3", Steps: []e2e.Step{st}}).Results[0]
	require.Equal(t, e2e.StatusRed, r.Status)
	require.Contains(t, r.Why, "every red says why")
}

func TestAnUncheckableFindingFailsTheStep(t *testing.T) {
	st := e2e.Step{ID: "P3.3", Run: func(_ context.Context, e *e2e.Env) error {
		e.Capture.AddFinding(e2e.FindingView{ID: "F1", File: "a.go", Line: 9, FileLines: 3, Tier: "E1", Claim: "x", EvidenceRef: "run 1"})
		return nil
	}}
	r := runOne(t, &fakeWorld{}, story(), e2e.Scenario{ID: "P3", Steps: []e2e.Step{st}}).Results[0]
	require.Equal(t, e2e.StatusRed, r.Status)
	require.Contains(t, r.Why, "checkable")
}

func TestAScriptedStepThatSpentMoneyFailsTheStep(t *testing.T) {
	w := &fakeWorld{spend: []float64{1.00, 1.0123}}
	r := runOne(t, w, story(), e2e.Scenario{ID: "P2", Steps: []e2e.Step{okStep("P2.2")}}).Results[0]
	require.Equal(t, e2e.StatusRed, r.Status)
	require.Contains(t, r.Why, "cost")
	require.InDelta(t, 0.0123, r.CostUSD, 1e-9)
	require.True(t, r.CostKnown)
}

func TestAnUnmeasuredCostIsReportedAsUnknownNotAsZero(t *testing.T) {
	r := runOne(t, &fakeWorld{}, story(), e2e.Scenario{ID: "P2", Steps: []e2e.Step{okStep("P2.2")}}).Results[0]
	require.Equal(t, e2e.StatusPass, r.Status)
	require.False(t, r.CostKnown)
}

func TestScenarioLevelChecksQuietLeakAndReplay(t *testing.T) {
	chatty := e2e.Step{ID: "P3.2", Run: func(_ context.Context, e *e2e.Env) error {
		e.Capture.AddMessages(
			e2e.Message{ID: "1", Room: "r", From: "bob-reviewer", FromAgent: true, Kind: e2e.KindProgress, Text: "Reading file 3 of 9 with HEIMDALL canary CANARY-1"},
		)
		return nil
	}}
	w := &fakeWorld{counts: []e2e.Counts{
		{Messages: map[string]int{"r": 1}},
		{Messages: map[string]int{"r": 2}}, // the replay posted a duplicate
	}}
	res := runOne(t, w, story(), e2e.Scenario{ID: "P3", Steps: []e2e.Step{chatty}, Budget: e2e.Budget{Milestones: 1}})
	require.Equal(t, 1, w.replayed, "a scenario that ran is replayed once")
	checks := map[string]bool{}
	for _, v := range res.Quality {
		checks[v.Check] = true
	}
	require.True(t, checks["quiet"], "progress chatter")
	require.True(t, checks["agents stay out"], "a canary in an agent post")
	require.True(t, checks["idempotent replay"], "the replay changed the count")
}

func TestAScenarioWhereNothingRanIsNotReplayedOrJudged(t *testing.T) {
	w := &fakeWorld{missing: map[e2e.Need]string{e2e.NeedServer: "down"}}
	st := okStep("P1.1")
	st.Needs = []e2e.Need{e2e.NeedServer}
	res := runOne(t, w, story(), e2e.Scenario{ID: "P1", Steps: []e2e.Step{st}})
	require.Zero(t, w.replayed)
	require.Empty(t, res.Quality)
}

func TestSummaryNeverCountsBlockedAsGreen(t *testing.T) {
	results := []e2e.ScenarioResult{{ID: "P1", Results: []e2e.Result{
		{Step: "P1.1", Status: e2e.StatusPass},
		{Step: "P1.2", Status: e2e.StatusBlocked},
		{Step: "P1.3", Status: e2e.StatusRed},
		{Step: "P1.4", Status: e2e.StatusNotReached},
		{Step: "P1.5", Status: e2e.StatusManual},
	}}}
	s := e2e.Summarize(results)
	require.Equal(t, e2e.Totals{Steps: 5, Green: 1, Blocked: 1, Red: 1, NotReached: 1, Manual: 1, CostUnmeasured: 2}, s, "the two steps that ran have no measured cost, which is not the same as $0")
	require.Contains(t, s.Line(), "1/5 green")
}

func TestStepsOfOneScenarioShareValuesAndScenariosDoNot(t *testing.T) {
	setter := e2e.Step{ID: "P1.1", Run: func(_ context.Context, e *e2e.Env) error { e.Set("ticket", "KAN-7"); return nil }}
	var got string
	var found bool
	reader := e2e.Step{ID: "P1.2", Run: func(_ context.Context, e *e2e.Env) error {
		got, found = e2e.Get[string](e, "ticket")
		return nil
	}}
	var otherSaw bool
	other := e2e.Step{ID: "P2.1", Run: func(_ context.Context, e *e2e.Env) error {
		_, otherSaw = e2e.Get[string](e, "ticket")
		return nil
	}}
	b := &e2e.Board{World: &fakeWorld{}, Story: story(), Now: ticker(time.Second), Scripted: true}
	b.Run(context.Background(), []e2e.Scenario{
		{ID: "P1", Steps: []e2e.Step{setter, reader}},
		{ID: "P2", Steps: []e2e.Step{other}},
	})
	require.True(t, found)
	require.Equal(t, "KAN-7", got, "a later step of the same scenario sees the value")
	require.False(t, otherSaw, "another scenario does not")

	env := &e2e.Env{}
	env.Set("n", 3)
	_, ok := e2e.Get[string](env, "n")
	require.False(t, ok, "a value of another type is not there")
}

func TestABlockedStepListsEveryMissingPrerequisiteWithTheMostSpecificFirst(t *testing.T) {
	st := story()
	st.Pending = "T31"
	step := okStep("P2.1")
	step.Needs = []e2e.Need{e2e.NeedServer, e2e.NeedForward, e2e.NeedFixtureRepo}
	w := &fakeWorld{missing: map[e2e.Need]string{e2e.NeedServer: "no server is configured", e2e.NeedFixtureRepo: "repository o/r has no branch main"}}
	r := runOne(t, w, st, e2e.Scenario{ID: "P2", Steps: []e2e.Step{step}}).Results[0]
	require.Equal(t, e2e.StatusBlocked, r.Status)
	require.Equal(t, e2e.NeedServer, r.BlockedOn, "the first missing prerequisite, in the step's own order, is the label")
	require.Contains(t, r.Why, "no server is configured")
	require.Contains(t, r.Why, "also missing: fixture-repo, story")
	require.NotContains(t, r.Why, "forward", "a prerequisite that is available is not listed")
}

func TestAStoryFreeStepRunsWhileTheStoryIsIncomplete(t *testing.T) {
	st := story()
	st.Pending = "T31"
	free := okStep("P9.1")
	free.NoStory = true
	tied := okStep("P2.1")
	got := runOne(t, &fakeWorld{}, st, e2e.Scenario{ID: "P9", Steps: []e2e.Step{free, tied}}).Results
	require.Equal(t, e2e.StatusPass, got[0].Status, "a step that never reads the story is not blocked by it")
	require.Equal(t, e2e.StatusBlocked, got[1].Status)
}

func TestEachPrerequisiteIsProbedOncePerBoardRun(t *testing.T) {
	a, b := okStep("P1.1"), okStep("P1.2")
	a.Needs = []e2e.Need{e2e.NeedServer, e2e.NeedFixtureRepo}
	b.Needs = []e2e.Need{e2e.NeedServer, e2e.NeedFixtureRepo}
	w := &fakeWorld{}
	board := &e2e.Board{World: w, Story: story(), Now: ticker(time.Second)}
	board.Run(context.Background(), []e2e.Scenario{{ID: "P1", Steps: []e2e.Step{a, b}}, {ID: "P2", Steps: []e2e.Step{a}}})
	require.Equal(t, 1, w.probes[e2e.NeedServer], "probing the fixture repo calls GitHub, so a board must not do it for every step")
	require.Equal(t, 1, w.probes[e2e.NeedFixtureRepo])
}
