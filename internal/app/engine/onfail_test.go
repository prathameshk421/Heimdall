package engine_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/engine"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// stepOf returns the fake step of the given type from steps.
func stepOf(t *testing.T, steps []engine.Step, typ string) *fakeStep {
	t.Helper()
	for _, s := range steps {
		if s.Type() == typ {
			f, ok := s.(*fakeStep)
			require.True(t, ok, "%s is not a fake", typ)
			return f
		}
	}
	require.FailNow(t, "no step of type "+typ)
	return nil
}

// lastEvent returns the newest stored event.
func lastEvent(t *testing.T, h *harness) change.Event {
	t.Helper()
	require.NotEmpty(t, h.store.events)
	return h.store.events[len(h.store.events)-1]
}

func decodeStepRetried(t *testing.T, ev change.Event) change.StepRetried {
	t.Helper()
	require.Equal(t, change.EventStepRetried, ev.Type)
	p, err := change.Decode(ev)
	require.NoError(t, err)
	return p.(change.StepRetried)
}

func decodeStepEscalated(t *testing.T, ev change.Event) change.StepEscalated {
	t.Helper()
	require.Equal(t, change.EventStepEscalated, ev.Type)
	p, err := change.Decode(ev)
	require.NoError(t, err)
	return p.(change.StepEscalated)
}

// TestStepFailedErrorIsFoundThroughWrapping is the contract of the failure type: the engine finds it with errors.As.
func TestStepFailedErrorIsFoundThroughWrapping(t *testing.T) {
	err := fmt.Errorf("step test_qa: %w", engine.StepFailedError{Reasons: []string{"AC2 has no test"}})
	var sf engine.StepFailedError
	require.ErrorAs(t, err, &sf)
	require.Equal(t, []string{"AC2 has no test"}, sf.Reasons)
	require.NotErrorAs(t, errBoom, &sf)
}

// TestRetryInsideBudgetPassesTheReasonsAndTheStepRuns: the sandbox rejects two submissions, and the third passes. Each
// rejection reopens the test-author with the reasons, and the attempt goes up by one each time.
func TestRetryInsideBudgetPassesTheReasonsAndTheStepRuns(t *testing.T) {
	h := newHarness(t)
	h.qaRejections = 2
	e := h.build(h.defaultConfig(h.defaultSteps()))
	ctx := context.Background()

	_, err := e.Advance(ctx, ticket, h.triggered())
	require.NoError(t, err)

	out, err := e.Advance(ctx, ticket, submission(h))
	require.NoError(t, err, "a failed check with budget left is not an error")
	require.Equal(t, []string{"test_qa"}, out.Failed)
	first := decodeStepRetried(t, lastEvent(t, h))
	require.Equal(t, change.StepRetried{Step: "write_tests", Attempt: 2, Reasons: []string{rejectionReason}}, first)

	_, err = e.Advance(ctx, ticket, submission(h))
	require.NoError(t, err)
	second := decodeStepRetried(t, lastEvent(t, h))
	require.Equal(t, change.StepRetried{Step: "write_tests", Attempt: 3, Reasons: []string{rejectionReason}}, second)

	out, err = e.Advance(ctx, ticket, submission(h))
	require.NoError(t, err)
	require.Empty(t, out.Failed)
	require.True(t, h.has(change.EventTestsVerified))
	require.True(t, h.has(change.EventTestsCommitted))
	require.Equal(t, 2, count(h.types(), change.EventTestsRejected))
	require.Equal(t, 2, count(h.types(), change.EventStepRetried))
	require.Contains(t, h.seenRetries, first, "the test-author was offered the reasons of the first retry")
	require.Contains(t, h.seenRetries, second, "and of the second")
}

// TestBudgetUsedUpEscalatesAndTheStepIsNotCalledAgain: with two attempts allowed, a second rejection is the end of the
// budget. The change gets step.escalated, the step is not called again, and the change is not complete.
func TestBudgetUsedUpEscalatesAndTheStepIsNotCalledAgain(t *testing.T) {
	h := newHarness(t)
	h.qaRejections = 9
	steps := h.defaultSteps()
	cfg := h.config(defaultWorkflow(), steps...)
	cfg.MaxAttempts = 2
	e := h.build(cfg)
	qa := stepOf(t, steps, "sandbox.test_quality")
	ctx := context.Background()

	_, err := e.Advance(ctx, ticket, h.triggered())
	require.NoError(t, err)
	_, err = e.Advance(ctx, ticket, submission(h))
	require.NoError(t, err)
	require.Equal(t, 1, count(h.types(), change.EventStepRetried))

	out, err := e.Advance(ctx, ticket, submission(h))
	require.NoError(t, err)
	require.Equal(t, []string{"test_qa"}, out.Failed)
	require.Equal(t, change.StepEscalated{Step: "test_qa", Attempts: 2, Reason: rejectionReason},
		decodeStepEscalated(t, lastEvent(t, h)))
	require.False(t, out.Complete, "an escalated change is not complete")
	require.Equal(t, 1, count(h.types(), change.EventStepRetried), "no retry after the budget is used up")

	starts, appends := qa.starts, h.store.appends
	out, err = e.Advance(ctx, ticket, nil)
	require.NoError(t, err)
	require.Equal(t, starts, qa.starts, "an escalated step is not called again")
	require.Equal(t, appends, h.store.appends, "and nothing is written for it")
	require.Zero(t, out.Events)
	require.False(t, out.Complete)
}

// TestRejectWritesOnlyTheRecordAndDoesNotLoop: slop has on_fail reject. The failing record is stored, the run does not
// fail, and a later run with nothing new writes nothing, even though the step is offered again.
func TestRejectWritesOnlyTheRecordAndDoesNotLoop(t *testing.T) {
	h := newHarness(t)
	steps := h.defaultSteps()
	slop := fake("rule.slop", func(c engine.ChangeView) bool { return slopPassed(h, c) },
		func(_ context.Context, c engine.ChangeView, _ json.RawMessage) (engine.Result, error) {
			if c.Has(change.EventSlopEvaluated) {
				return engine.Result{}, nil
			}
			rec := h.emit(change.EventSlopEvaluated, sysActor, change.SlopEvaluated{Pass: false, FailingRules: []string{"ticket-link"}})
			return rec, engine.StepFailedError{Reasons: []string{"the PR names no ticket"}}
		})
	replaceStep(steps, slop)
	e := h.build(h.config(defaultWorkflow(), steps...))
	ctx := context.Background()

	_, err := e.Advance(ctx, ticket, h.triggered())
	require.NoError(t, err)
	_, err = e.Advance(ctx, ticket, submission(h))
	require.NoError(t, err)

	out, err := e.Advance(ctx, ticket, h.incoming(change.EventPROpened, ghActor, prOpened()))
	require.NoError(t, err, "a rejected check is not an error")
	require.Equal(t, []string{"slop"}, out.Failed)
	require.Equal(t, change.EventSlopEvaluated, lastEvent(t, h).Type)
	require.Equal(t, 1, count(h.types(), change.EventSlopEvaluated))
	require.Zero(t, count(h.types(), change.EventStepRetried))
	require.Zero(t, count(h.types(), change.EventStepEscalated))
	require.Empty(t, out.Timers, "the reviewer pick waits for a passing slop")
	require.False(t, out.Complete)

	starts, appends := slop.starts, h.store.appends
	out, err = e.Advance(ctx, ticket, nil)
	require.NoError(t, err)
	require.Greater(t, slop.starts, starts, "the step is offered its run again")
	require.Equal(t, appends, h.store.appends, "and it writes nothing again")
	require.Empty(t, out.Failed)
	require.Zero(t, out.Events)
}

// TestReplayOfAStoredRetryMakesTheSameDecisionAndWritesNothing: an engine started on the stored events of a change in
// the middle of a retry finds the retry already recorded. It writes nothing and does not retry twice.
func TestReplayOfAStoredRetryMakesTheSameDecisionAndWritesNothing(t *testing.T) {
	h := newHarness(t)
	h.qaRejections = 1
	e := h.build(h.config(defaultWorkflow(), h.defaultSteps()...))
	ctx := context.Background()
	_, err := e.Advance(ctx, ticket, h.triggered())
	require.NoError(t, err)
	_, err = e.Advance(ctx, ticket, submission(h))
	require.NoError(t, err)
	require.Equal(t, 1, count(h.types(), change.EventStepRetried))

	// A restarted server holds the same events and fresh steps.
	store := &fakeStore{events: slices.Clone(h.store.events)}
	cfg := h.config(defaultWorkflow(), h.defaultSteps()...)
	cfg.Store = store
	replayed := h.build(cfg)

	out, err := replayed.Advance(ctx, ticket, nil)
	require.NoError(t, err)
	require.Zero(t, out.Events)
	require.Zero(t, store.appends, "the replay writes nothing")
	require.Equal(t, 1, count(typesOf(store.events), change.EventStepRetried), "the retry is not recorded twice")
	require.Empty(t, out.Failed)
	require.False(t, out.Complete)
}

// TestVersionConflictMidRetryIsRetriedWithoutDuplicatingTheRetry: another writer's event lands between the sandbox's
// rejection and its append. The engine loads the change again, decides again, and stores the rejection and the retry
// once each, after the other writer's event.
func TestVersionConflictMidRetryIsRetriedWithoutDuplicatingTheRetry(t *testing.T) {
	h := newHarness(t)
	h.qaRejections = 1
	steps := h.defaultSteps()
	qa := stepOf(t, steps, "sandbox.test_quality")
	orig := qa.start
	calls := 0
	qa.start = func(ctx context.Context, c engine.ChangeView, with json.RawMessage) (engine.Result, error) {
		calls++
		if calls == 1 {
			h.store.race = func() {
				ev := h.event(change.EventTicketChanged, humanActor, change.TicketChanged{ChangedFields: []string{"title"}})
				ev.Version = int64(len(h.store.events)) + 1
				h.store.events = append(h.store.events, ev)
			}
		}
		return orig(ctx, c, with)
	}
	e := h.build(h.config(defaultWorkflow(), steps...))
	ctx := context.Background()
	_, err := e.Advance(ctx, ticket, h.triggered())
	require.NoError(t, err)

	out, err := e.Advance(ctx, ticket, submission(h))
	require.NoError(t, err)
	require.Equal(t, []string{"test_qa"}, out.Failed)
	require.Equal(t, 2, calls, "the rejection is decided again on the view after the conflict")
	require.Equal(t, 1, count(h.types(), change.EventTestsRejected))
	require.Equal(t, 1, count(h.types(), change.EventStepRetried))
	types := h.types()
	require.Equal(t, []change.EventType{
		change.EventTicketChanged, change.EventTestsRejected, change.EventStepRetried,
	}, types[len(types)-3:])
}

// TestDownstreamStepReopensAfterARetry: branch and room are done, then slop fails and retries branch. Branch runs
// again, and room, which comes after branch, must run again too. Its done event from before the retry does not count.
func TestDownstreamStepReopensAfterARetry(t *testing.T) {
	h := newHarness(t)
	branchStep := fake("github.ensure_branch", hasOf(change.EventBranchEnsured), func(_ context.Context, _ engine.ChangeView, _ json.RawMessage) (engine.Result, error) {
		return h.emit(change.EventBranchEnsured, ghActor, change.BranchEnsured{Repo: repo, Branch: branch2, BaseSHA: sha}), nil
	})
	room := fake("switch.open_room", hasOf(change.EventRoomOpened), func(_ context.Context, _ engine.ChangeView, _ json.RawMessage) (engine.Result, error) {
		return h.emit(change.EventRoomOpened, sysActor, change.RoomOpened{RoomID: "room-PAY-142"}), nil
	})
	slopCalls := 0
	slop := fake("rule.slop", hasOf(change.EventSlopEvaluated), func(_ context.Context, _ engine.ChangeView, _ json.RawMessage) (engine.Result, error) {
		slopCalls++
		if slopCalls == 1 {
			rec := h.emit(change.EventSlopEvaluated, sysActor, change.SlopEvaluated{Pass: false, FailingRules: []string{"stale"}})
			return rec, engine.StepFailedError{Reasons: []string{"stale branch"}}
		}
		return h.emit(change.EventSlopEvaluated, sysActor, change.SlopEvaluated{Pass: true}), nil
	})
	w := chain(
		workflow.Step{ID: "branch", Do: "github.ensure_branch"},
		workflow.Step{ID: "room", After: []string{"branch"}, Do: "switch.open_room"},
		workflow.Step{ID: "slop", After: []string{"room"}, Do: "rule.slop",
			OnFail: &workflow.OnFail{Retry: "branch", Until: "task_budget", Then: "escalate"}},
	)
	e := h.build(h.config(w, branchStep, room, slop))

	out, err := e.Advance(context.Background(), ticket, h.triggered())
	require.NoError(t, err)
	require.Equal(t, 2, count(h.types(), change.EventBranchEnsured), "branch runs again after the retry")
	require.Equal(t, 2, count(h.types(), change.EventRoomOpened), "room runs again after the retry")
	require.Equal(t, 2, room.starts, "room was not finished for the view after the retry")
	require.Equal(t, 1, count(h.types(), change.EventStepRetried))
	require.Equal(t, change.StepRetried{Step: "branch", Attempt: 2, Reasons: []string{"stale branch"}},
		decodeStepRetried(t, retriedEvent(h)))
	require.Equal(t, []string{"slop"}, out.Failed)
	require.True(t, out.Complete)
}

// branch2 is the branch the downstream test writes; it is not the fixture branch.
const branch2 = "PAY-142-retry"

// retriedEvent returns the first step.retried event of the store.
func retriedEvent(h *harness) change.Event {
	for _, ev := range h.store.events {
		if ev.Type == change.EventStepRetried {
			return ev
		}
	}
	require.FailNow(h.t, "no step.retried in the store")
	return change.Event{}
}

// TestRetryReasonsAreBoundedToEightAndTheTextLimit: a check can report more reasons than a retry may carry, and each
// reason can be longer than a payload allows. The retry carries at most eight, each cut to the text limit.
func TestRetryReasonsAreBoundedToEightAndTheTextLimit(t *testing.T) {
	h := newHarness(t)
	long := strings.Repeat("x", 300)
	var reasons []string
	for i := range 10 {
		reasons = append(reasons, fmt.Sprintf("reason %d", i))
	}
	reasons[0] = long
	write := fake("agent.task", hasOf(change.EventTestsSubmitted), nil)
	calls := 0
	qa := fake("sandbox.test_quality", hasOf(change.EventTestsVerified), func(_ context.Context, _ engine.ChangeView, _ json.RawMessage) (engine.Result, error) {
		calls++
		if calls == 1 {
			return engine.Result{}, engine.StepFailedError{Reasons: reasons}
		}
		return h.emit(change.EventTestsVerified, sysActor, change.TestsVerified{RunIDs: []id.RunID{runID}, SHA: sha, TreeHash: sha}), nil
	})
	w := chain(
		workflow.Step{ID: "write_tests", Do: "agent.task", Role: workflow.RoleTestAuthor},
		workflow.Step{ID: "test_qa", After: []string{"write_tests"}, Do: "sandbox.test_quality",
			OnFail: &workflow.OnFail{Retry: "write_tests", Until: "task_budget", Then: "escalate"}},
	)
	e := h.build(h.config(w, write, qa))
	ctx := context.Background()
	_, err := e.Advance(ctx, ticket, h.triggered())
	require.NoError(t, err)
	_, err = e.Advance(ctx, ticket, submission(h))
	require.NoError(t, err)

	got := decodeStepRetried(t, retriedEvent(h))
	require.Len(t, got.Reasons, 8)
	require.Equal(t, strings.Repeat("x", 256), got.Reasons[0])
	require.Equal(t, "reason 1", got.Reasons[1])
}

// TestAStepFailureWithoutOnFailEscalatesOnce: a check that fails on a step with no on_fail is not left silent. The
// change escalates once, and the step is not called again.
func TestAStepFailureWithoutOnFailEscalatesOnce(t *testing.T) {
	h := newHarness(t)
	branchStep := fake("github.ensure_branch", nil, func(_ context.Context, _ engine.ChangeView, _ json.RawMessage) (engine.Result, error) {
		return engine.Result{}, engine.StepFailedError{Reasons: []string{"no branch"}}
	})
	e := h.build(h.config(chain(workflow.Step{ID: "branch", Do: "github.ensure_branch"}), branchStep))
	ctx := context.Background()

	out, err := e.Advance(ctx, ticket, h.triggered())
	require.NoError(t, err)
	require.Equal(t, []string{"branch"}, out.Failed)
	require.Equal(t, change.StepEscalated{Step: "branch", Attempts: 1, Reason: "no branch"}, decodeStepEscalated(t, lastEvent(t, h)))

	starts := branchStep.starts
	out, err = e.Advance(ctx, ticket, nil)
	require.NoError(t, err)
	require.Equal(t, starts, branchStep.starts)
	require.Zero(t, out.Events)
}

// TestNewRefusesARetryingWorkflowWithoutAnAttemptBudget: a retry needs MaxAttempts, or the retry would never stop.
func TestNewRefusesARetryingWorkflowWithoutAnAttemptBudget(t *testing.T) {
	h := newHarness(t)
	cfg := h.defaultConfig(h.defaultSteps())
	cfg.MaxAttempts = 0
	_, err := engine.New(cfg)
	require.ErrorIs(t, err, workflow.ErrPolicyInvalid)
}

// TestNewRefusesAnUntilThatIsNotTaskBudget: the only budget the spec names is the task budget (TECH_SPEC 5.1).
func TestNewRefusesAnUntilThatIsNotTaskBudget(t *testing.T) {
	h := newHarness(t)
	w := defaultWorkflow()
	for i := range w.Steps {
		if o := w.Steps[i].OnFail; o != nil && o.Retry != "" {
			o.Until = "forever"
		}
	}
	_, err := engine.New(h.config(w, h.defaultSteps()...))
	require.ErrorIs(t, err, workflow.ErrPolicyInvalid)
}

// slopPassed reports whether the view holds a passing slop.evaluated.
func slopPassed(h *harness, c engine.ChangeView) bool {
	for _, ev := range c.Events {
		if ev.Type != change.EventSlopEvaluated {
			continue
		}
		p, err := change.Decode(ev)
		require.NoError(h.t, err)
		if p.(change.SlopEvaluated).Pass {
			return true
		}
	}
	return false
}

// replaceStep puts repl in place of the step of the same type in steps.
func replaceStep(steps []engine.Step, repl engine.Step) {
	for i, s := range steps {
		if s.Type() == repl.Type() {
			steps[i] = repl
		}
	}
}
