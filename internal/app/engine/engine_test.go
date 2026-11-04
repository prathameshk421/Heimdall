package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/engine"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// errBoom is the failure a test step returns.
var errBoom = errors.New("boom")

// chain is a workflow of the given steps, in the given declaration order.
func chain(steps ...workflow.Step) workflow.Workflow {
	return workflow.Workflow{Version: 1, Steps: steps}
}

// config is the configuration of workflow w over the given steps, on the harness's fakes.
func (h *harness) config(w workflow.Workflow, steps ...engine.Step) engine.Config {
	cfg := h.defaultConfig(steps)
	cfg.Workflow = w
	return cfg
}

// typesOf lists the types of the events, in order.
func typesOf(evs []change.Event) []change.EventType {
	out := make([]change.EventType, 0, len(evs))
	for _, e := range evs {
		out = append(out, e.Type)
	}
	return out
}

// count returns how many of types equal want.
func count(types []change.EventType, want change.EventType) int {
	n := 0
	for _, t := range types {
		if t == want {
			n++
		}
	}
	return n
}

// submission is the test-author's tests.submitted, as it arrives from the room or the agent runtime.
func submission(h *harness) []change.Event {
	return h.incoming(change.EventTestsSubmitted, agentActor, change.TestsSubmitted{
		Refs:     []string{"tests/heimdall/PAY-142/refund_test.go"},
		ACMap:    map[string][]string{"AC1": {"TestRefundReturnsMoney"}},
		SHA:      sha,
		TreeHash: sha,
	})
}

func TestNewRefusesAWorkflowThatDoesNotValidate(t *testing.T) {
	h := newHarness(t)
	// Only one of the workflow's step types is registered, so the rest of the default workflow cannot run.
	cfg := h.config(defaultWorkflow(), fake("switch.open_room", hasOf(change.EventRoomOpened), nil))
	_, err := engine.New(cfg)
	require.ErrorIs(t, err, workflow.ErrPolicyInvalid)
}

func TestNewRefusesAConditionTheWorkflowNamesButNoneRegistered(t *testing.T) {
	h := newHarness(t)
	cfg := h.defaultConfig(h.defaultSteps())
	cfg.Conditions = nil
	_, err := engine.New(cfg)
	require.ErrorIs(t, err, workflow.ErrPolicyInvalid, "signoff names sensitive_domain, so the config must supply it")
}

// A retry must name a step that exists and comes before the failing step (TECH_SPEC 5.1). New refuses the others.
func TestNewRefusesAnOnFailRetryThatNamesNoEarlierStep(t *testing.T) {
	writeTests := workflow.Step{ID: "write_tests", Do: "agent.task", Role: workflow.RoleTestAuthor}
	testQA := workflow.Step{ID: "test_qa", After: []string{"write_tests"}, Do: "sandbox.test_quality"}
	cases := map[string]func() workflow.Workflow{
		"unknown step": func() workflow.Workflow {
			qa := testQA
			qa.OnFail = &workflow.OnFail{Retry: "nope", Until: "task_budget", Then: "escalate"}
			return chain(writeTests, qa)
		},
		"a later step": func() workflow.Workflow {
			wt := writeTests
			wt.OnFail = &workflow.OnFail{Retry: "test_qa", Until: "task_budget", Then: "escalate"}
			return chain(wt, testQA)
		},
		"the step itself": func() workflow.Workflow {
			qa := testQA
			qa.OnFail = &workflow.OnFail{Retry: "test_qa", Until: "task_budget", Then: "escalate"}
			return chain(writeTests, qa)
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			steps := []engine.Step{
				fake("agent.task", hasOf(change.EventTestsSubmitted), nil),
				fake("sandbox.test_quality", hasOf(change.EventTestsVerified), nil),
			}
			_, err := engine.New(h.config(build(), steps...))
			require.ErrorIs(t, err, workflow.ErrPolicyInvalid)
		})
	}
}

func TestNewRefusesMissingStoreClockOrIDs(t *testing.T) {
	cases := map[string]func(*engine.Config){
		"store": func(c *engine.Config) { c.Store = nil },
		"clock": func(c *engine.Config) { c.Clock = nil },
		"ids":   func(c *engine.Config) { c.IDs = nil },
	}
	for name, drop := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			cfg := h.defaultConfig(h.defaultSteps())
			drop(&cfg)
			_, err := engine.New(cfg)
			require.Error(t, err)
		})
	}
}

func TestNewRefusesTwoStepsOfOneType(t *testing.T) {
	h := newHarness(t)
	steps := append(h.defaultSteps(), fake("switch.open_room", nil, nil))
	_, err := engine.New(h.defaultConfig(steps))
	require.Error(t, err)
}

func TestNewAcceptsTheDefaultWorkflowOnTheFakes(t *testing.T) {
	h := newHarness(t)
	e, err := engine.New(h.defaultConfig(h.defaultSteps()))
	require.NoError(t, err)
	require.NotNil(t, e)
}

func TestStepsRunInDependencyOrderAndEachAppendsAtTheVersionItRead(t *testing.T) {
	h := newHarness(t)
	var calls []string
	sawAtSlop := 0
	room := fake("switch.open_room", hasOf(change.EventRoomOpened), func(_ context.Context, _ engine.ChangeView, _ json.RawMessage) (engine.Result, error) {
		calls = append(calls, "room")
		return h.emit(change.EventRoomOpened, sysActor, change.RoomOpened{RoomID: "room-PAY-142"}), nil
	})
	branchStep := fake("github.ensure_branch", hasOf(change.EventBranchEnsured), func(_ context.Context, _ engine.ChangeView, _ json.RawMessage) (engine.Result, error) {
		calls = append(calls, "branch")
		return h.emit(change.EventBranchEnsured, ghActor, change.BranchEnsured{Repo: repo, Branch: branch, BaseSHA: sha}), nil
	})
	slop := fake("rule.slop", hasOf(change.EventSlopEvaluated), func(_ context.Context, c engine.ChangeView, _ json.RawMessage) (engine.Result, error) {
		calls = append(calls, "slop")
		sawAtSlop = len(c.Events)
		return h.emit(change.EventSlopEvaluated, sysActor, change.SlopEvaluated{Pass: true}), nil
	})
	// Declared out of order: slop first, room last. Each step runs once its after steps are finished.
	w := chain(
		workflow.Step{ID: "slop", After: []string{"branch"}, Do: "rule.slop"},
		workflow.Step{ID: "branch", After: []string{"room"}, Do: "github.ensure_branch"},
		workflow.Step{ID: "room", Do: "switch.open_room"},
	)
	e := h.build(h.config(w, room, branchStep, slop))

	out, err := e.Advance(context.Background(), ticket, h.triggered())
	require.NoError(t, err)
	require.Equal(t, []string{"room", "branch", "slop"}, calls)
	require.Equal(t, []change.EventType{
		change.EventChangeTriggered, change.EventRoomOpened, change.EventBranchEnsured, change.EventSlopEvaluated,
	}, h.types())
	require.Equal(t, []int64{0, 1, 2, 3}, h.store.expected, "the trigger and each step append at the version they read")
	require.Equal(t, 3, sawAtSlop, "slop decides on the view that holds the events of the steps before it")
	require.Equal(t, 4, out.Events, "the trigger and the three step events")
	require.Equal(t, int64(4), out.Version)
	require.True(t, out.Complete)
}

func TestVersionConflictMakesTheStepDecideAgainOnTheNewView(t *testing.T) {
	h := newHarness(t)
	ready := false
	var viewLens []int
	room := fake("switch.open_room", hasOf(change.EventRoomOpened), func(_ context.Context, c engine.ChangeView, _ json.RawMessage) (engine.Result, error) {
		if !ready {
			return engine.Result{}, nil
		}
		viewLens = append(viewLens, len(c.Events))
		return h.emit(change.EventRoomOpened, sysActor, change.RoomOpened{RoomID: "room-PAY-142"}), nil
	})
	e := h.build(h.config(chain(workflow.Step{ID: "room", Do: "switch.open_room"}), room))
	_, err := e.Advance(context.Background(), ticket, h.triggered())
	require.NoError(t, err)
	require.Empty(t, viewLens)

	ready = true
	h.store.conflicts = 2
	out, err := e.Advance(context.Background(), ticket, nil)
	require.NoError(t, err, "two refused appends are retried and the third is written")
	require.Equal(t, []int{1, 1, 1}, viewLens, "each try decides again on the view it read")
	require.Equal(t, []int64{0, 1, 1, 1}, h.store.expected)
	require.Equal(t, []change.EventType{change.EventChangeTriggered, change.EventRoomOpened}, h.types())
	require.Equal(t, 1, out.Events)
}

func TestVersionConflictShowsTheEventsAnotherWriterAdded(t *testing.T) {
	h := newHarness(t)
	ready := false
	var seen [][]change.EventType
	room := fake("switch.open_room", hasOf(change.EventRoomOpened), func(_ context.Context, c engine.ChangeView, _ json.RawMessage) (engine.Result, error) {
		if !ready {
			return engine.Result{}, nil
		}
		seen = append(seen, typesOf(c.Events))
		return h.emit(change.EventRoomOpened, sysActor, change.RoomOpened{RoomID: "room-PAY-142"}), nil
	})
	e := h.build(h.config(chain(workflow.Step{ID: "room", Do: "switch.open_room"}), room))
	_, err := e.Advance(context.Background(), ticket, h.triggered())
	require.NoError(t, err)

	ready = true
	// Another writer appends a ticket change between the engine's read and its append.
	h.store.race = func() {
		ev := h.event(change.EventTicketChanged, humanActor, change.TicketChanged{ChangedFields: []string{"title"}})
		ev.Version = int64(len(h.store.events)) + 1
		h.store.events = append(h.store.events, ev)
	}
	_, err = e.Advance(context.Background(), ticket, nil)
	require.NoError(t, err)
	require.Equal(t, [][]change.EventType{
		{change.EventChangeTriggered},
		{change.EventChangeTriggered, change.EventTicketChanged},
	}, seen, "the retry sees the other writer's event")
	require.Equal(t, []change.EventType{
		change.EventChangeTriggered, change.EventTicketChanged, change.EventRoomOpened,
	}, h.types())
	require.Equal(t, []int64{0, 1, 2}, h.store.expected)
}

func TestVersionConflictGivesUpAfterFiveRetries(t *testing.T) {
	h := newHarness(t)
	ready := false
	tries := 0
	room := fake("switch.open_room", hasOf(change.EventRoomOpened), func(_ context.Context, _ engine.ChangeView, _ json.RawMessage) (engine.Result, error) {
		if !ready {
			return engine.Result{}, nil
		}
		tries++
		return h.emit(change.EventRoomOpened, sysActor, change.RoomOpened{RoomID: "room-PAY-142"}), nil
	})
	e := h.build(h.config(chain(workflow.Step{ID: "room", Do: "switch.open_room"}), room))
	_, err := e.Advance(context.Background(), ticket, h.triggered())
	require.NoError(t, err)

	ready = true
	h.store.conflicts = 100
	_, err = e.Advance(context.Background(), ticket, nil)
	require.ErrorIs(t, err, ports.ErrVersionConflict)
	require.Equal(t, 6, tries, "the first try and five retries (TECH_SPEC 2.4)")
	require.Equal(t, []change.EventType{change.EventChangeTriggered}, h.types(), "nothing was written")
}

func TestAFailedAgentStepRecordsTaskFailedOnce(t *testing.T) {
	h := newHarness(t)
	// The first decision assigns the task. Once the submission is in, the step cannot read it and fails.
	write := fake("agent.task", hasOf(change.EventTestsVerified), func(_ context.Context, c engine.ChangeView, _ json.RawMessage) (engine.Result, error) {
		if !c.Has(change.EventTaskAssigned) {
			return h.emit(change.EventTaskAssigned, sysActor, change.TaskAssigned{
				TaskID: taskID, Agent: "test-author", Role: workflow.RoleTestAuthor,
				Deadline: dayStart.Add(2 * time.Hour), BundleRef: blob,
			}), nil
		}
		if c.Has(change.EventTestsSubmitted) {
			return engine.Result{}, errBoom
		}
		return engine.Result{}, nil
	})
	w := chain(workflow.Step{ID: "write_tests", Do: "agent.task", Role: workflow.RoleTestAuthor, Timeout: 2 * time.Hour})
	e := h.build(h.config(w, write))
	ctx := context.Background()

	_, err := e.Advance(ctx, ticket, h.triggered())
	require.NoError(t, err)
	out, err := e.Advance(ctx, ticket, submission(h))
	require.NoError(t, err, "a failed step is recorded as task.failed and is not returned as an error")
	require.Equal(t, []string{"write_tests"}, out.Failed)
	require.Equal(t, []change.EventType{
		change.EventChangeTriggered, change.EventTaskAssigned, change.EventTestsSubmitted, change.EventTaskFailed,
	}, h.types())

	require.Len(t, h.store.events, 4)
	failed := h.store.events[3]
	p, err := change.Decode(failed)
	require.NoError(t, err)
	require.Equal(t, change.TaskFailed{
		TaskID: taskID, Agent: "test-author", Role: workflow.RoleTestAuthor, Reason: errBoom.Error(),
	}, p)
	require.NotNil(t, failed.CausationID)
	require.Equal(t, h.store.events[1].ID, *failed.CausationID, "the failure is caused by the task assignment")
	require.Equal(t, sysActor, failed.Actor)
	require.True(t, failed.OccurredAt.Equal(dayStart))

	// The next run tries the step again and fails again, but the failure is recorded once.
	out, err = e.Advance(ctx, ticket, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"write_tests"}, out.Failed)
	require.Equal(t, 1, count(h.types(), change.EventTaskFailed))
}

func TestAFailedStepWithNoTaskReturnsItsErrorAndWritesNothing(t *testing.T) {
	h := newHarness(t)
	branchStep := fake("github.ensure_branch", nil, func(_ context.Context, _ engine.ChangeView, _ json.RawMessage) (engine.Result, error) {
		return engine.Result{}, errBoom
	})
	e := h.build(h.config(chain(workflow.Step{ID: "branch", Do: "github.ensure_branch"}), branchStep))

	_, err := e.Advance(context.Background(), ticket, h.triggered())
	require.ErrorIs(t, err, errBoom, "without a task there is no task.failed to record, so the error goes to the caller")
	require.Equal(t, []change.EventType{change.EventChangeTriggered}, h.types())
}

func TestATimeoutStampsTaskFailedWithTheInjectedClock(t *testing.T) {
	h := newHarness(t)
	later := dayStart.Add(3 * time.Hour)
	write := fake("agent.task", hasOf(change.EventTestsVerified), func(_ context.Context, c engine.ChangeView, _ json.RawMessage) (engine.Result, error) {
		if c.Has(change.EventTaskAssigned) {
			return engine.Result{}, nil
		}
		return h.emit(change.EventTaskAssigned, sysActor, change.TaskAssigned{
			TaskID: taskID, Agent: "test-author", Role: workflow.RoleTestAuthor,
			Deadline: dayStart.Add(2 * time.Hour), BundleRef: blob,
		}), nil
	})
	write.timeout = func(engine.ChangeView) (engine.Result, error) { return engine.Result{}, errBoom }
	cfg := h.config(chain(workflow.Step{ID: "write_tests", Do: "agent.task", Role: workflow.RoleTestAuthor, Timeout: 2 * time.Hour}), write)
	cfg.Clock = fixedClock{now: later}
	e := h.build(cfg)

	_, err := e.Advance(context.Background(), ticket, h.triggered())
	require.NoError(t, err)
	out, err := e.Timeout(context.Background(), ticket, "write_tests")
	require.NoError(t, err)
	require.Equal(t, []string{"write_tests"}, out.Failed)
	require.Len(t, h.store.events, 3)
	failed := h.store.events[2]
	require.Equal(t, change.EventTaskFailed, failed.Type)
	require.True(t, failed.OccurredAt.Equal(later), "the engine stamps the events it writes with the injected clock")
}

func TestATimeoutWritesTheStepsEventsAndThenRunsTheRestOfTheRun(t *testing.T) {
	h := newHarness(t)
	write := fake("agent.task", hasOf(change.EventTaskTimedOut), func(_ context.Context, c engine.ChangeView, _ json.RawMessage) (engine.Result, error) {
		if c.Has(change.EventTaskAssigned) {
			return engine.Result{}, nil
		}
		return h.emit(change.EventTaskAssigned, sysActor, change.TaskAssigned{
			TaskID: taskID, Agent: "test-author", Role: workflow.RoleTestAuthor,
			Deadline: dayStart.Add(2 * time.Hour), BundleRef: blob,
		}), nil
	})
	write.timeout = func(engine.ChangeView) (engine.Result, error) {
		return h.emit(change.EventTaskTimedOut, sysActor, change.TaskTimedOut{
			TaskID: taskID, Agent: "test-author", Role: workflow.RoleTestAuthor, Reason: "no submission before the deadline",
		}), nil
	}
	room := fake("switch.open_room", hasOf(change.EventRoomOpened), func(_ context.Context, _ engine.ChangeView, _ json.RawMessage) (engine.Result, error) {
		return h.emit(change.EventRoomOpened, sysActor, change.RoomOpened{RoomID: "room-PAY-142"}), nil
	})
	w := chain(
		workflow.Step{ID: "write_tests", Do: "agent.task", Role: workflow.RoleTestAuthor, Timeout: 2 * time.Hour},
		workflow.Step{ID: "room", After: []string{"write_tests"}, Do: "switch.open_room"},
	)
	e := h.build(h.config(w, write, room))
	_, err := e.Advance(context.Background(), ticket, h.triggered())
	require.NoError(t, err)

	out, err := e.Timeout(context.Background(), ticket, "write_tests")
	require.NoError(t, err)
	require.Equal(t, []change.EventType{
		change.EventChangeTriggered, change.EventTaskAssigned, change.EventTaskTimedOut, change.EventRoomOpened,
	}, h.types(), "the timeout's event comes first, then the room, which waited for the step")
	require.Equal(t, 2, out.Events)
	require.True(t, out.Complete)
}

func TestATimeoutOfAFinishedStepDoesNothing(t *testing.T) {
	h := newHarness(t)
	timeouts := 0
	room := fake("switch.open_room", hasOf(change.EventRoomOpened), func(_ context.Context, _ engine.ChangeView, _ json.RawMessage) (engine.Result, error) {
		return h.emit(change.EventRoomOpened, sysActor, change.RoomOpened{RoomID: "room-PAY-142"}), nil
	})
	room.timeout = func(engine.ChangeView) (engine.Result, error) {
		timeouts++
		return engine.Result{}, nil
	}
	e := h.build(h.config(chain(workflow.Step{ID: "room", Do: "switch.open_room"}), room))
	_, err := e.Advance(context.Background(), ticket, h.triggered())
	require.NoError(t, err)
	appends := h.store.appends

	out, err := e.Timeout(context.Background(), ticket, "room")
	require.NoError(t, err)
	require.Zero(t, timeouts, "a timer that fires after the step is done does not call the step")
	require.Zero(t, out.Events)
	require.Equal(t, appends, h.store.appends)
}

func TestATimeoutNamingNoStepIsAnError(t *testing.T) {
	h := newHarness(t)
	e := h.build(h.defaultConfig(h.defaultSteps()))
	_, err := e.Timeout(context.Background(), ticket, "no_such_step")
	require.Error(t, err)
	require.Empty(t, h.store.expected)
}

func TestAFinishedStepIsNotStartedAgainAndAStepWithNothingToRecordWritesNothing(t *testing.T) {
	h := newHarness(t)
	room := fake("switch.open_room", hasOf(change.EventRoomOpened), func(_ context.Context, _ engine.ChangeView, _ json.RawMessage) (engine.Result, error) {
		return h.emit(change.EventRoomOpened, sysActor, change.RoomOpened{RoomID: "room-PAY-142"}), nil
	})
	// The branch waits for something outside the engine: it is never done here and writes nothing.
	waiting := fake("github.ensure_branch", hasOf(change.EventBranchEnsured), nil)
	w := chain(
		workflow.Step{ID: "room", Do: "switch.open_room"},
		workflow.Step{ID: "branch", After: []string{"room"}, Do: "github.ensure_branch"},
	)
	e := h.build(h.config(w, room, waiting))
	ctx := context.Background()

	_, err := e.Advance(ctx, ticket, h.triggered())
	require.NoError(t, err)
	roomStarts, waitingStarts, appends := room.starts, waiting.starts, h.store.appends

	out, err := e.Advance(ctx, ticket, nil)
	require.NoError(t, err)
	require.Equal(t, roomStarts, room.starts, "a done step is not started again")
	require.Greater(t, waiting.starts, waitingStarts, "an unfinished step is started on each run")
	require.Equal(t, appends, h.store.appends, "a run that finds recorded work writes nothing")
	require.Zero(t, out.Events)
	require.False(t, out.Complete)
}

func TestIncomingEventsAreOfferedOnlyToStepsThatAreReady(t *testing.T) {
	h := newHarness(t)
	first := fake("switch.open_room", nil, nil)
	second := fake("github.ensure_branch", nil, nil)
	w := chain(
		workflow.Step{ID: "open", Do: "switch.open_room"},
		workflow.Step{ID: "branch", After: []string{"open"}, Do: "github.ensure_branch"},
	)
	e := h.build(h.config(w, first, second))

	_, err := e.Advance(context.Background(), ticket, h.triggered())
	require.NoError(t, err)
	require.Equal(t, []change.EventType{change.EventChangeTriggered}, first.offered)
	require.Empty(t, second.offered, "the branch waits for open, so the event is not offered to it")
}

func TestAdvanceRefusesACancelledContextAndWritesNothing(t *testing.T) {
	h := newHarness(t)
	e := h.build(h.defaultConfig(h.defaultSteps()))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := e.Advance(ctx, ticket, h.triggered())
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, h.store.expected)
}
