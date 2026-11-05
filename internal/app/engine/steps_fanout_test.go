package engine_test

import (
	"context"
	"encoding/json"
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

func TestAgentFanoutAssignsEachReviewerExactlyOnceAcrossTwoRuns(t *testing.T) {
	now := time.Date(2026, 11, 5, 14, 0, 0, 0, time.UTC)
	clock := stepFakeClock{now: now}
	ids := &stepFakeIDs{}
	spec := engine.AgentFanoutSpec{
		ID:      "fanout-reviewers",
		Timeout: 30 * time.Minute,
		Clock:   clock,
		IDs:     ids,
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

func TestAgentFanoutOnTimeoutWithoutAck(t *testing.T) {
	now := time.Date(2026, 11, 5, 14, 0, 0, 0, time.UTC)
	clock := stepFakeClock{now: now}
	ids := &stepFakeIDs{}
	spec := engine.AgentFanoutSpec{
		ID:      "fanout-reviewers",
		Timeout: 30 * time.Minute,
		Clock:   clock,
		IDs:     ids,
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

	// Bob should have review.unavailable
	var foundUnavailable bool
	for _, ev := range res.Events {
		if ev.Type == change.EventReviewUnavailable {
			var unavail change.ReviewUnavailable
			require.NoError(t, json.Unmarshal(ev.Payload, &unavail))
			if unavail.Reviewer == "bob-reviewer" {
				foundUnavailable = true
			}
			require.NotEqual(t, id.AgentName("alice-reviewer"), unavail.Reviewer)
		}
	}
	require.True(t, foundUnavailable, "bob-reviewer should be marked review.unavailable")
}
