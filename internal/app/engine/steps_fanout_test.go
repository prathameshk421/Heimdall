package engine_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/engine"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/stretchr/testify/require"
)

func TestAgentFanoutNotReadyUntilReviewersPicked(t *testing.T) {
	clock := stepFakeClock{now: time.Date(2026, 11, 5, 14, 0, 0, 0, time.UTC)}
	ids := &stepFakeIDs{}
	spec := engine.AgentFanoutSpec{
		ID:      "fanout-reviewers",
		Timeout: 30 * time.Minute,
		Clock:   clock,
		IDs:     ids,
		Bundle:  testBundle,
	}
	step, err := engine.NewAgentFanout(spec)
	require.NoError(t, err)

	ticket := id.ChangeID("PAY-142")
	view := engine.ChangeView{ID: ticket}

	// Not ready: Done is false and Start returns nothing
	require.False(t, step.Done(view))
	res, err := step.Start(context.Background(), view, nil)
	require.NoError(t, err)
	require.Empty(t, res.Events)
	require.Empty(t, res.Effects)
}

func TestResolverFanoutCreatesCaseScopedTaskAndBundleOnce(t *testing.T) {
	caseID := id.CaseID("case_01HZX3K9QW7M2P5R8T0V4YB6CD")
	opened := makeTestEvent(t, change.EventCaseOpened, change.CaseOpened{CaseID: caseID, Finding: id.FindingRef(2)})
	var builtFor id.CaseID
	step, err := engine.NewResolverFanout("deliberate", "resolver-one", time.Minute, stepFakeClock{now: dayStart}, &stepFakeIDs{}, func(_ id.ChangeID, got id.CaseID, _ []change.Event) ([]byte, workflow.BlobRef, error) {
		builtFor = got
		return []byte(`{"case_id":"` + string(got) + `"}`), blob, nil
	})
	require.NoError(t, err)
	view := engine.ChangeView{ID: ticket, Events: []change.Event{opened}}
	result, err := step.Start(context.Background(), view, nil)
	require.NoError(t, err)
	require.Equal(t, caseID, builtFor)
	require.Len(t, result.Events, 1)
	require.Len(t, result.Effects, 1)
	var assignment change.TaskAssigned
	require.NoError(t, json.Unmarshal(result.Events[0].Payload, &assignment))
	require.Equal(t, caseID, assignment.CaseID)
	require.Equal(t, workflow.RoleResolver, assignment.Role)
	view.Events = append(view.Events, result.Events...)
	replay, err := step.Start(context.Background(), view, nil)
	require.NoError(t, err)
	require.Empty(t, replay.Events, "an open case gets only one active resolver task")
}

func TestResolverFanoutAssignsEachCaseItsOwnResolverTask(t *testing.T) {
	first := id.CaseID("case_01HZX3K9QW7M2P5R8T0V4YB6CD")
	second := id.CaseID("case_01HZX3K9QW7M2P5R8T0V4YB6CE")
	opened := func(n int, caseID id.CaseID) change.Event {
		ev := makeTestEvent(t, change.EventCaseOpened, change.CaseOpened{CaseID: caseID, Finding: id.FindingRef(n)})
		ev.ID = change.EventID(fmt.Sprintf("20000000-0000-4000-8000-%012d", n))
		return ev
	}
	var built []id.CaseID
	step, err := engine.NewResolverFanout("deliberate", "resolver-one", time.Minute, stepFakeClock{now: dayStart}, &stepFakeIDs{},
		func(_ id.ChangeID, got id.CaseID, _ []change.Event) ([]byte, workflow.BlobRef, error) {
			built = append(built, got)
			return []byte(`{"case_id":"` + string(got) + `"}`), blob, nil
		})
	require.NoError(t, err)

	view := engine.ChangeView{ID: ticket, Events: []change.Event{opened(1, first), opened(2, second)}}
	result, err := step.Start(context.Background(), view, nil)
	require.NoError(t, err)
	require.ElementsMatch(t, []id.CaseID{first, second}, built, "each open case builds its own bundle")
	require.Len(t, result.Events, 2)
	require.Len(t, result.Effects, 2)
	require.Len(t, result.Timers, 2)

	tasks := make(map[id.CaseID]id.TaskID)
	for _, ev := range result.Events {
		var assignment change.TaskAssigned
		require.NoError(t, json.Unmarshal(ev.Payload, &assignment))
		require.Equal(t, workflow.RoleResolver, assignment.Role)
		require.NotEmpty(t, assignment.CaseID, "every resolver task is scoped to a case")
		tasks[assignment.CaseID] = assignment.TaskID
	}
	require.Len(t, tasks, 2, "two cases get two tasks, not one task reused across cases")
	require.Contains(t, tasks, first)
	require.Contains(t, tasks, second)
	require.NotEqual(t, tasks[first], tasks[second], "a task settles exactly one case")
	require.NotEqual(t, result.Effects[0].Key, result.Effects[1].Key)

	view.Events = append(view.Events, result.Events...)
	replay, err := step.Start(context.Background(), view, nil)
	require.NoError(t, err)
	require.Empty(t, replay.Events, "a second pass must not assign an already assigned case again")

	// A third challenge wakes only its own case: neither earlier case gets a second task.
	third := id.CaseID("case_01HZX3K9QW7M2P5R8T0V4YB6CF")
	thirdEvent := opened(3, third)
	view.Events = append(view.Events, thirdEvent)
	more, err := step.OnEvent(context.Background(), view, thirdEvent)
	require.NoError(t, err)
	require.Len(t, more.Events, 1)
	var thirdAssignment change.TaskAssigned
	require.NoError(t, json.Unmarshal(more.Events[0].Payload, &thirdAssignment))
	require.Equal(t, third, thirdAssignment.CaseID)
	require.NotContains(t, tasks, thirdAssignment.CaseID)
	require.NotEqual(t, tasks[first], thirdAssignment.TaskID)
	require.NotEqual(t, tasks[second], thirdAssignment.TaskID)

	other, err := step.OnEvent(context.Background(), view, makeTestEvent(t, change.EventReviewSubmitted, change.ReviewSubmitted{Reviewer: "reviewer-one"}))
	require.NoError(t, err)
	require.Empty(t, other.Events, "an event that opens no case wakes no resolver")
}

func TestAgentFanoutAssignsEachReviewerExactlyOnceAcrossTwoRuns(t *testing.T) {
	now := time.Date(2026, 11, 5, 14, 0, 0, 0, time.UTC)
	clock := stepFakeClock{now: now}
	ids := &stepFakeIDs{}
	spec := engine.AgentFanoutSpec{
		ID:      "fanout-reviewers",
		Timeout: 30 * time.Minute,
		Clock:   clock,
		IDs:     ids,
		Bundle:  testBundle,
	}
	step, err := engine.NewAgentFanout(spec)
	require.NoError(t, err)

	ticket := id.ChangeID("PAY-142")
	pickEv := makeTestEvent(t, change.EventReviewersPicked, change.ReviewersPicked{
		People: []change.ActorID{"alice-reviewer", "bob-reviewer"},
		By:     "author",
		Method: change.PickCommand,
	})
	view := engine.ChangeView{
		ID:     ticket,
		Events: []change.Event{pickEv},
	}

	// Run 1: assigns alice and bob
	res1, err := step.Start(context.Background(), view, nil)
	require.NoError(t, err)
	require.Len(t, res1.Events, 2)
	require.Len(t, res1.Effects, 2)
	require.Len(t, res1.Timers, 2)

	var ta1, ta2 change.TaskAssigned
	require.NoError(t, json.Unmarshal(res1.Events[0].Payload, &ta1))
	require.NoError(t, json.Unmarshal(res1.Events[1].Payload, &ta2))
	require.Equal(t, id.AgentName("alice-reviewer"), ta1.Agent)
	require.Equal(t, id.AgentName("bob-reviewer"), ta2.Agent)
	require.Equal(t, workflow.RoleReviewer, ta1.Role)
	require.Equal(t, workflow.RoleReviewer, ta2.Role)

	firstPayload, err := change.DecodeTaskAssignPayload(res1.Effects[0].Payload)
	require.NoError(t, err)
	require.Contains(t, string(firstPayload.Bundle), `"skill":"reviewer@1.0.0"`)
	replayBytes, replayRef, err := testBundle(workflow.RoleReviewer, ticket, view.Events)
	require.NoError(t, err)
	require.Equal(t, firstPayload.Bundle, replayBytes)
	require.Equal(t, ta1.BundleRef, replayRef)

	// Append run 1 events to view
	view.Events = append(view.Events, res1.Events...)

	// Run 2: Start returns nothing because both reviewers are already assigned
	res2, err := step.Start(context.Background(), view, nil)
	require.NoError(t, err)
	require.Empty(t, res2.Events)
	require.Empty(t, res2.Effects)
	require.Empty(t, res2.Timers)

	// Now add a second reviewers.picked with charlie added
	pickEv2 := makeTestEvent(t, change.EventReviewersPicked, change.ReviewersPicked{
		People: []change.ActorID{"alice-reviewer", "bob-reviewer", "charlie-reviewer"},
		By:     "author",
		Method: change.PickCommand,
	})
	view.Events = append(view.Events, pickEv2)

	// Run 3: only charlie gets assigned
	res3, err := step.Start(context.Background(), view, nil)
	require.NoError(t, err)
	require.Len(t, res3.Events, 1)
	require.Len(t, res3.Effects, 1)

	var ta3 change.TaskAssigned
	require.NoError(t, json.Unmarshal(res3.Events[0].Payload, &ta3))
	require.Equal(t, id.AgentName("charlie-reviewer"), ta3.Agent)
}

func TestAgentFanoutDoneCondition(t *testing.T) {
	clock := stepFakeClock{now: time.Date(2026, 11, 5, 14, 0, 0, 0, time.UTC)}
	ids := &stepFakeIDs{}
	spec := engine.AgentFanoutSpec{
		ID:      "fanout-reviewers",
		Timeout: 30 * time.Minute,
		Clock:   clock,
		IDs:     ids,
		Bundle:  testBundle,
	}
	step, err := engine.NewAgentFanout(spec)
	require.NoError(t, err)

	ticket := id.ChangeID("PAY-142")
	pickEv := makeTestEvent(t, change.EventReviewersPicked, change.ReviewersPicked{
		People: []change.ActorID{"alice-reviewer", "bob-reviewer", "charlie-reviewer"},
		By:     "author",
		Method: change.PickCommand,
	})
	view := engine.ChangeView{
		ID:     ticket,
		Events: []change.Event{pickEv},
	}

	require.False(t, step.Done(view))

	// Alice submits review
	subEv := makeTestEvent(t, change.EventReviewSubmitted, change.ReviewSubmitted{
		Reviewer:    id.AgentName("alice-reviewer"),
		FindingRefs: []id.FindingRef{},
		PackDate:    "2026-11-05",
	})
	view.Events = append(view.Events, subEv)
	require.False(t, step.Done(view))

	// Bob is unavailable
	unavailEv := makeTestEvent(t, change.EventReviewUnavailable, change.ReviewUnavailable{
		Reviewer: id.AgentName("bob-reviewer"),
		Reason:   "timed out",
	})
	view.Events = append(view.Events, unavailEv)
	require.False(t, step.Done(view))

	// Charlie is waived
	waivedEv := makeTestEvent(t, change.EventReviewWaived, change.ReviewWaived{
		Reviewer: id.AgentName("charlie-reviewer"),
		Reason:   "not critical",
		Approver: "lead",
	})
	view.Events = append(view.Events, waivedEv)

	// Now all three picked reviewers have review.submitted, review.unavailable or review.waived
	require.True(t, step.Done(view))
}

func TestAgentFanoutReassignsReviewersAfterHeadPush(t *testing.T) {
	now := time.Date(2026, 11, 5, 14, 0, 0, 0, time.UTC)
	step, err := engine.NewAgentFanout(engine.AgentFanoutSpec{
		ID: "fanout-reviewers", Timeout: 30 * time.Minute, Clock: stepFakeClock{now: now}, IDs: &stepFakeIDs{}, Bundle: testBundle,
	})
	require.NoError(t, err)
	view := engine.ChangeView{
		ID: "PAY-142",
		Events: []change.Event{makeTestEvent(t, change.EventPROpened, change.PROpened{
			Repo: "acme/pay", Number: 42, HeadSHA: "a100000000000000000000000000000000000001", Author: "author", Ticket: "PAY-142",
		}), makeTestEvent(t, change.EventReviewersPicked, change.ReviewersPicked{
			People: []change.ActorID{"alice-reviewer", "bob-reviewer"}, By: "author", Method: change.PickCommand,
		})},
	}
	first, err := step.Start(context.Background(), view, nil)
	require.NoError(t, err)
	view.Events = append(view.Events, first.Events...)
	for _, agent := range []id.AgentName{"alice-reviewer", "bob-reviewer"} {
		view.Events = append(view.Events, makeTestEvent(t, change.EventReviewSubmitted, change.ReviewSubmitted{
			Reviewer: agent, FindingRefs: []id.FindingRef{}, PackDate: "2026-11-05",
		}))
	}
	require.True(t, step.Done(view), "all selected reviewers completed the first head")
	view.Events = append(view.Events, makeTestEvent(t, change.EventPRPushed, change.PRPushed{
		Repo: "acme/pay", Number: 42, HeadSHA: "b100000000000000000000000000000000000002",
	}))
	require.False(t, step.Done(view), "a new head makes prior submissions stale")
	second, err := step.Start(context.Background(), view, nil)
	require.NoError(t, err)
	require.Len(t, second.Events, 2, "both reviewers must receive tasks for the new head")
}

func TestAgentFanoutOnTimeoutWithoutAck(t *testing.T) {
	now := time.Date(2026, 11, 5, 14, 0, 0, 0, time.UTC)
	clock := stepFakeClock{now: now}
	ids := &stepFakeIDs{}
	spec := engine.AgentFanoutSpec{
		ID:      "fanout-reviewers",
		Timeout: 30 * time.Minute,
		Clock:   clock,
		IDs:     ids,
		Bundle:  testBundle,
	}
	step, err := engine.NewAgentFanout(spec)
	require.NoError(t, err)

	ticket := id.ChangeID("PAY-142")
	pickEv := makeTestEvent(t, change.EventReviewersPicked, change.ReviewersPicked{
		People: []change.ActorID{"alice-reviewer", "bob-reviewer"},
		By:     "author",
		Method: change.PickCommand,
	})

	aliceTask := makeTestEvent(t, change.EventTaskAssigned, change.TaskAssigned{
		TaskID:    id.TaskID("tsk_01HZX3K9QW7M2P5R8T0V4YB001"),
		Agent:     id.AgentName("alice-reviewer"),
		Role:      workflow.RoleReviewer,
		Deadline:  now,
		BundleRef: workflow.BlobRef("blob:sha256:" + zeros64),
	})
	bobTask := makeTestEvent(t, change.EventTaskAssigned, change.TaskAssigned{
		TaskID:    id.TaskID("tsk_01HZX3K9QW7M2P5R8T0V4YB002"),
		Agent:     id.AgentName("bob-reviewer"),
		Role:      workflow.RoleReviewer,
		Deadline:  now,
		BundleRef: workflow.BlobRef("blob:sha256:" + zeros64),
	})

	// Alice acked
	aliceAck := makeTestEvent(t, change.EventTaskAcked, change.TaskAcked{
		TaskID: id.TaskID("tsk_01HZX3K9QW7M2P5R8T0V4YB001"),
		Agent:  id.AgentName("alice-reviewer"),
		Role:   workflow.RoleReviewer,
	})

	// Bob did NOT ack
	view := engine.ChangeView{
		ID:     ticket,
		Events: []change.Event{pickEv, aliceTask, bobTask, aliceAck},
	}

	res, err := step.OnTimeout(context.Background(), view)
	require.NoError(t, err)

	// Both are released at the deadline: Bob never acked, Alice acked but submitted nothing (KAN-19: an acked reviewer
	// hung on a question and would have held the review open for ever).
	reasons := map[id.AgentName]string{}
	for _, ev := range res.Events {
		if ev.Type == change.EventReviewUnavailable {
			var unavail change.ReviewUnavailable
			require.NoError(t, json.Unmarshal(ev.Payload, &unavail))
			reasons[unavail.Reviewer] = unavail.Reason
		}
	}
	require.Equal(t, "task timed out without ack", reasons["bob-reviewer"])
	require.Equal(t, "task timed out without a review", reasons["alice-reviewer"])
}
