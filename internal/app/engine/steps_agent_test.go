package engine_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/bundle"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/engine"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/stretchr/testify/require"
)

const zeros64 = "0000000000000000000000000000000000000000000000000000000000000000"

func makeTestEvent(t *testing.T, typ change.EventType, payload change.Payload) change.Event {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	return change.Event{
		ID:         change.EventID("evt_01HZX3K9QW7M2P5R8T0V4YB6CD"),
		ChangeID:   id.ChangeID("PAY-142"),
		Version:    1,
		Type:       typ,
		Actor:      change.Actor{Kind: change.ActorSystem, ID: "heimdall"},
		OccurredAt: time.Date(2026, 11, 5, 14, 0, 0, 0, time.UTC),
		Payload:    raw,
	}
}

func testBundle(role workflow.Role, changeID id.ChangeID, events []change.Event) ([]byte, workflow.BlobRef, error) {
	built, err := bundle.BuildFromEvents(role, changeID, events)
	if err != nil {
		return nil, "", err
	}
	return built.Bytes, bundle.Hash(built), nil
}

type stepFakeIDs struct {
	nextTask  int
	nextEvent int
}

func (f *stepFakeIDs) NewTask() id.TaskID {
	f.nextTask++
	return id.TaskID(fmt.Sprintf("tsk_01HZX3K9QW7M2P5R8T0V4YB%03d", f.nextTask))
}

func (f *stepFakeIDs) NewEvent() change.EventID {
	f.nextEvent++
	return change.EventID(fmt.Sprintf("10000000-0000-4000-8000-%012d", f.nextEvent))
}

type stepFakeClock struct {
	now time.Time
}

func (f stepFakeClock) Now() time.Time { return f.now }

func TestNewAgentTaskUnknownRoleRefused(t *testing.T) {
	spec := engine.AgentTaskSpec{
		ID:       "write-tests",
		Role:     workflow.Role("wizard"),
		Timeout:  15 * time.Minute,
		Output:   change.EventTestsSubmitted,
		Clock:    stepFakeClock{now: time.Date(2026, 11, 5, 14, 0, 0, 0, time.UTC)},
		IDs:      &stepFakeIDs{},
		Bundle:   testBundle,
		AgentFor: func(_ workflow.Role) (id.AgentName, bool) { return "", false },
	}
	_, err := engine.NewAgentTask(spec)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown agent role")
}

func TestAgentTaskDone(t *testing.T) {
	clock := stepFakeClock{now: time.Date(2026, 11, 5, 14, 0, 0, 0, time.UTC)}
	ids := &stepFakeIDs{}
	agentFor := func(role workflow.Role) (id.AgentName, bool) {
		if role == workflow.RoleTestAuthor {
			return id.AgentName("test-author"), true
		}
		return "", false
	}
	spec := engine.AgentTaskSpec{
		ID:       "write-tests",
		Role:     workflow.RoleTestAuthor,
		Timeout:  15 * time.Minute,
		Output:   change.EventTestsSubmitted,
		Clock:    clock,
		IDs:      ids,
		Bundle:   testBundle,
		AgentFor: agentFor,
	}
	step, err := engine.NewAgentTask(spec)
	require.NoError(t, err)

	ticket := id.ChangeID("PAY-142")
	view := engine.ChangeView{ID: ticket}

	// Done is false initially
	require.False(t, step.Done(view))

	// OnEvent task.acked does nothing to Done
	ackedEv := makeTestEvent(t, change.EventTaskAcked, change.TaskAcked{
		TaskID: id.TaskID("tsk_01HZX3K9QW7M2P5R8T0V4YB001"),
		Agent:  id.AgentName("test-author"),
		Role:   workflow.RoleTestAuthor,
	})
	res, err := step.OnEvent(context.Background(), view, ackedEv)
	require.NoError(t, err)
	require.Empty(t, res.Events)
	require.Empty(t, res.Effects)
	view.Events = append(view.Events, ackedEv)
	require.False(t, step.Done(view))

	// Done is true once output (tests.submitted) exists
	testsEv := makeTestEvent(t, change.EventTestsSubmitted, change.TestsSubmitted{
		Refs:     []string{"test1"},
		ACMap:    map[string][]string{"AC1": {"Test1"}},
		SHA:      id.SHA("9f1c2d3e4b5a6c7d8e9f0a1b2c3d4e5f6a7b8c9d"),
		TreeHash: id.SHA("1111222233334444555566667777888899990000"),
	})
	view.Events = append(view.Events, testsEv)
	require.True(t, step.Done(view))
}

func TestAgentTaskStartAndDeadline(t *testing.T) {
	now := time.Date(2026, 11, 5, 14, 0, 0, 0, time.UTC)
	timeout := 15 * time.Minute
	expectedDeadline := now.Add(timeout)
	clock := stepFakeClock{now: now}
	ids := &stepFakeIDs{}
	agentFor := func(role workflow.Role) (id.AgentName, bool) {
		if role == workflow.RoleTestAuthor {
			return id.AgentName("test-author"), true
		}
		return "", false
	}
	spec := engine.AgentTaskSpec{
		ID:       "write-tests",
		Role:     workflow.RoleTestAuthor,
		Timeout:  timeout,
		Output:   change.EventTestsSubmitted,
		Clock:    clock,
		IDs:      ids,
		Bundle:   testBundle,
		AgentFor: agentFor,
	}
	step, err := engine.NewAgentTask(spec)
	require.NoError(t, err)

	ticket := id.ChangeID("PAY-142")
	trigEv := makeTestEvent(t, change.EventChangeTriggered, change.Triggered{
		Ticket:   ticket,
		Repo:     change.Repo("acme/pay-demo"),
		Assignee: change.ActorID("ana"),
		Source:   change.TriggerFile,
	})
	roomEv := makeTestEvent(t, change.EventRoomOpened, change.RoomOpened{
		RoomID: change.RoomID("room_142"),
	})
	view := engine.ChangeView{
		ID:     ticket,
		Events: []change.Event{trigEv, roomEv},
	}

	res, err := step.Start(context.Background(), view, nil)
	require.NoError(t, err)

	// Verifies: returns task.assigned, ONE effect of kind task_assign, and a Timer
	require.Len(t, res.Events, 1)
	require.Equal(t, change.EventTaskAssigned, res.Events[0].Type)

	var ta change.TaskAssigned
	err = json.Unmarshal(res.Events[0].Payload, &ta)
	require.NoError(t, err)
	require.Equal(t, id.TaskID("tsk_01HZX3K9QW7M2P5R8T0V4YB001"), ta.TaskID)
	require.Equal(t, id.AgentName("test-author"), ta.Agent)
	require.Equal(t, workflow.RoleTestAuthor, ta.Role)
	require.Equal(t, expectedDeadline, ta.Deadline)
	require.NotEmpty(t, ta.BundleRef)

	// Verifies effect
	require.Len(t, res.Effects, 1)
	fx := res.Effects[0]
	require.Equal(t, change.EffectTaskAssign, fx.Kind)
	require.Equal(t, string(ta.BundleRef), fx.Target)

	payload, err := change.DecodeTaskAssignPayload(fx.Payload)
	require.NoError(t, err)
	require.Equal(t, id.AgentName("test-author"), payload.Agent)
	require.Equal(t, workflow.RoleTestAuthor, payload.Role)
	require.Equal(t, ta.TaskID, payload.TaskID)
	require.Equal(t, change.RoomID("room_142"), payload.RoomID)
	require.NotEmpty(t, payload.Bundle)

	// Verifies timer
	require.Len(t, res.Timers, 1)
	require.Equal(t, "write-tests", res.Timers[0].Step)
	require.Equal(t, expectedDeadline, res.Timers[0].Due)

	// Idempotent: Start returns nothing once task.assigned is in the window
	view.Events = append(view.Events, res.Events[0])
	res2, err := step.Start(context.Background(), view, nil)
	require.NoError(t, err)
	require.Empty(t, res2.Events)
	require.Empty(t, res2.Effects)
	require.Empty(t, res2.Timers)
}

func TestAgentTaskRetryGetsNewTaskID(t *testing.T) {
	now := time.Date(2026, 11, 5, 14, 0, 0, 0, time.UTC)
	clock := stepFakeClock{now: now}
	ids := &stepFakeIDs{}
	agentFor := func(_ workflow.Role) (id.AgentName, bool) {
		return id.AgentName("test-author"), true
	}
	spec := engine.AgentTaskSpec{
		ID:       "write-tests",
		Role:     workflow.RoleTestAuthor,
		Timeout:  15 * time.Minute,
		Output:   change.EventTestsSubmitted,
		Clock:    clock,
		IDs:      ids,
		Bundle:   testBundle,
		AgentFor: agentFor,
	}
	step, err := engine.NewAgentTask(spec)
	require.NoError(t, err)

	ticket := id.ChangeID("PAY-142")
	view1 := engine.ChangeView{
		ID: ticket,
		Events: []change.Event{
			makeTestEvent(t, change.EventChangeTriggered, change.Triggered{
				Ticket:   ticket,
				Repo:     change.Repo("acme/pay-demo"),
				Assignee: change.ActorID("ana"),
				Source:   change.TriggerFile,
			}),
		},
	}
	res1, err := step.Start(context.Background(), view1, nil)
	require.NoError(t, err)
	var ta1 change.TaskAssigned
	require.NoError(t, json.Unmarshal(res1.Events[0].Payload, &ta1))
	require.Equal(t, id.TaskID("tsk_01HZX3K9QW7M2P5R8T0V4YB001"), ta1.TaskID)

	// Now simulate a retry cutting the window at step.retried
	retryEv := makeTestEvent(t, change.EventStepRetried, change.StepRetried{
		Step:    "write-tests",
		Attempt: 2,
		Reasons: []string{"check failed"},
	})
	viewAfterRetry := engine.ChangeView{
		ID:     ticket,
		Events: []change.Event{retryEv}, // window starts at retry
	}

	res2, err := step.Start(context.Background(), viewAfterRetry, nil)
	require.NoError(t, err)
	require.Len(t, res2.Events, 1)

	var ta2 change.TaskAssigned
	require.NoError(t, json.Unmarshal(res2.Events[0].Payload, &ta2))
	require.Equal(t, id.TaskID("tsk_01HZX3K9QW7M2P5R8T0V4YB002"), ta2.TaskID)
	require.NotEqual(t, ta1.TaskID, ta2.TaskID)
}

func TestAgentTaskOnTimeout(t *testing.T) {
	now := time.Date(2026, 11, 5, 14, 0, 0, 0, time.UTC)
	clock := stepFakeClock{now: now}
	ids := &stepFakeIDs{}
	agentFor := func(_ workflow.Role) (id.AgentName, bool) {
		return id.AgentName("test-author"), true
	}
	spec := engine.AgentTaskSpec{
		ID:       "write-tests",
		Role:     workflow.RoleTestAuthor,
		Timeout:  15 * time.Minute,
		Output:   change.EventTestsSubmitted,
		Clock:    clock,
		IDs:      ids,
		Bundle:   testBundle,
		AgentFor: agentFor,
	}
	step, err := engine.NewAgentTask(spec)
	require.NoError(t, err)

	ticket := id.ChangeID("PAY-142")
	assignedEv := makeTestEvent(t, change.EventTaskAssigned, change.TaskAssigned{
		TaskID:    id.TaskID("tsk_01HZX3K9QW7M2P5R8T0V4YB001"),
		Agent:     id.AgentName("test-author"),
		Role:      workflow.RoleTestAuthor,
		Deadline:  now,
		BundleRef: workflow.BlobRef("blob:sha256:" + zeros64),
	})

	view := engine.ChangeView{
		ID:     ticket,
		Events: []change.Event{assignedEv},
	}

	// OnTimeout when task is not done: returns task.timed_out
	res, err := step.OnTimeout(context.Background(), view)
	require.NoError(t, err)
	require.Len(t, res.Events, 1)
	require.Equal(t, change.EventTaskTimedOut, res.Events[0].Type)

	var to change.TaskTimedOut
	require.NoError(t, json.Unmarshal(res.Events[0].Payload, &to))
	require.Equal(t, id.TaskID("tsk_01HZX3K9QW7M2P5R8T0V4YB001"), to.TaskID)
	require.Equal(t, id.AgentName("test-author"), to.Agent)
	require.Equal(t, workflow.RoleTestAuthor, to.Role)
	require.NotEmpty(t, to.Reason)

	// If task is done: OnTimeout returns nothing
	testsEv := makeTestEvent(t, change.EventTestsSubmitted, change.TestsSubmitted{
		Refs:     []string{"test1"},
		ACMap:    map[string][]string{"AC1": {"Test1"}},
		SHA:      id.SHA("9f1c2d3e4b5a6c7d8e9f0a1b2c3d4e5f6a7b8c9d"),
		TreeHash: id.SHA("1111222233334444555566667777888899990000"),
	})
	viewDone := engine.ChangeView{
		ID:     ticket,
		Events: []change.Event{assignedEv, testsEv},
	}
	resDone, err := step.OnTimeout(context.Background(), viewDone)
	require.NoError(t, err)
	require.Empty(t, resDone.Events)
}

// The reassignment after a retry posts to the change's room, which was opened before the retry window.
func TestAgentTaskRetryAssignsIntoTheRoomOpenedBeforeTheRetry(t *testing.T) {
	spec := engine.AgentTaskSpec{
		ID: "write-tests", Role: workflow.RoleTestAuthor, Timeout: 15 * time.Minute, Output: change.EventTestsSubmitted,
		Clock: stepFakeClock{now: time.Date(2026, 11, 5, 14, 0, 0, 0, time.UTC)}, IDs: &stepFakeIDs{}, Bundle: testBundle,
		AgentFor: func(workflow.Role) (id.AgentName, bool) { return "test-author", true },
	}
	step, err := engine.NewAgentTask(spec)
	require.NoError(t, err)
	ticket := id.ChangeID("KAN-9")
	triggered := makeTestEvent(t, change.EventChangeTriggered, change.Triggered{Ticket: ticket, Repo: "acme/pay-demo", Assignee: "ana", Source: change.TriggerFile})
	room := makeTestEvent(t, change.EventRoomOpened, change.RoomOpened{RoomID: "b674ce26-aecf-4bc4-afe1-07d012985d8b"})
	retry := makeTestEvent(t, change.EventStepRetried, change.StepRetried{Step: "write-tests", Attempt: 2, Reasons: []string{"no package clause"}})
	view := engine.ChangeView{ID: ticket, Events: []change.Event{retry}, History: []change.Event{triggered, room, retry}}

	res, err := step.Start(context.Background(), view, nil)
	require.NoError(t, err)
	require.Len(t, res.Effects, 1)
	var payload change.TaskAssignPayload
	require.NoError(t, json.Unmarshal(res.Effects[0].Payload, &payload))
	require.Equal(t, change.RoomID("b674ce26-aecf-4bc4-afe1-07d012985d8b"), payload.RoomID)
	require.Contains(t, string(res.Effects[0].Key), "KAN-9:4:", "the key's version counts the whole change, not the window")
}
