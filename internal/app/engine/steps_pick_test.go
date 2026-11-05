package engine_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/engine"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

type pickFakeClock struct {
	now time.Time
}

func (c pickFakeClock) Now() time.Time { return c.now }

type pickFakeIDs struct {
	nextEvent change.EventID
}

func (i *pickFakeIDs) NewFinding() id.FindingID { return "" }
func (i *pickFakeIDs) NewRun() id.RunID         { return "" }
func (i *pickFakeIDs) NewCase() id.CaseID       { return "" }
func (i *pickFakeIDs) NewTask() id.TaskID       { return "" }
func (i *pickFakeIDs) NewEvent() change.EventID {
	if i.nextEvent != "" {
		res := i.nextEvent
		i.nextEvent = ""
		return res
	}
	return "ev-pick-42"
}

func TestPickReviewersContract(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	clock := pickFakeClock{now: now}
	ids := &pickFakeIDs{}

	step, err := engine.NewPickReviewers(engine.PickReviewersSpec{
		ID:      "pick",
		Timeout: 15 * time.Minute,
		Clock:   clock,
		IDs:     ids,
		DefaultReviewers: func(_ engine.ChangeView) []change.ActorID {
			return []change.ActorID{"alice", "bob"}
		},
	})
	require.NoError(t, err)
	require.Equal(t, "human.pick_reviewers", step.Type())

	view := engine.ChangeView{
		ID: id.ChangeID("PAY-142"),
		Events: []change.Event{
			{
				ID:            "ev-1",
				ChangeID:      id.ChangeID("PAY-142"),
				Type:          change.EventSlopEvaluated,
				OccurredAt:    now,
				CorrelationID: "corr-1",
			},
		},
	}
	require.False(t, step.Done(view))

	// Start sets the timer once, returns no events or effects
	res, err := step.Start(context.Background(), view, nil)
	require.NoError(t, err)
	require.Empty(t, res.Events)
	require.Empty(t, res.Effects)
	require.Len(t, res.Timers, 1)
	require.Equal(t, "pick", res.Timers[0].Step)
	require.Equal(t, now.Add(15*time.Minute), res.Timers[0].Due)

	// OnTimeout creates reviewers.picked with default method and injected reviewers
	res, err = step.OnTimeout(context.Background(), view)
	require.NoError(t, err)
	require.Len(t, res.Events, 1)
	require.Equal(t, change.EventReviewersPicked, res.Events[0].Type)

	var p change.ReviewersPicked
	require.NoError(t, json.Unmarshal(res.Events[0].Payload, &p))
	require.Equal(t, []change.ActorID{"alice", "bob"}, p.People)
	require.Equal(t, change.ActorID("heimdall"), p.By)
	require.Equal(t, change.PickDefault, p.Method)

	// Once reviewers.picked exists in the view, Done is true
	view.Events = append(view.Events, res.Events[0])
	require.True(t, step.Done(view))

	// When Done is true, Start and OnTimeout return empty results
	res, err = step.Start(context.Background(), view, nil)
	require.NoError(t, err)
	require.Empty(t, res.Timers)

	res, err = step.OnTimeout(context.Background(), view)
	require.NoError(t, err)
	require.Empty(t, res.Events)
}

func TestPickReviewersSpecValidation(t *testing.T) {
	clock := pickFakeClock{now: time.Now()}
	ids := &pickFakeIDs{}
	def := func(_ engine.ChangeView) []change.ActorID { return []change.ActorID{"alice"} }

	_, err := engine.NewPickReviewers(engine.PickReviewersSpec{})
	require.Error(t, err)

	_, err = engine.NewPickReviewers(engine.PickReviewersSpec{ID: "pick"})
	require.Error(t, err)

	_, err = engine.NewPickReviewers(engine.PickReviewersSpec{ID: "pick", Timeout: 15 * time.Minute, Clock: clock})
	require.Error(t, err)

	_, err = engine.NewPickReviewers(engine.PickReviewersSpec{ID: "pick", Timeout: 15 * time.Minute, Clock: clock, IDs: ids})
	require.Error(t, err)

	_, err = engine.NewPickReviewers(engine.PickReviewersSpec{ID: "pick", Timeout: 15 * time.Minute, Clock: clock, IDs: ids, DefaultReviewers: def})
	require.NoError(t, err)
}
