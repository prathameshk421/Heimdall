package engine_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/engine"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

func TestWaitStepContract(t *testing.T) {
	with := json.RawMessage(`{"event":"pr.opened"}`)
	waitStep, err := engine.NewWait(with)
	require.NoError(t, err)
	require.Equal(t, "wait", waitStep.Type())

	view := engine.ChangeView{
		ID: id.ChangeID("PAY-142"),
		Events: []change.Event{
			{
				ID:       "ev-1",
				ChangeID: id.ChangeID("PAY-142"),
				Type:     change.EventChangeTriggered,
			},
		},
	}
	require.False(t, waitStep.Done(view))

	// Start does nothing and produces no effects
	res, err := waitStep.Start(context.Background(), view, with)
	require.NoError(t, err)
	require.Empty(t, res.Events)
	require.Empty(t, res.Effects)
	require.Empty(t, res.Timers)

	// OnEvent does nothing
	res, err = waitStep.OnEvent(context.Background(), view, view.Events[0])
	require.NoError(t, err)
	require.Empty(t, res.Events)
	require.Empty(t, res.Effects)
	require.Empty(t, res.Timers)

	// OnTimeout does nothing
	res, err = waitStep.OnTimeout(context.Background(), view)
	require.NoError(t, err)
	require.Empty(t, res.Events)
	require.Empty(t, res.Effects)
	require.Empty(t, res.Timers)

	// Once the event exists in the window, Done is true
	view.Events = append(view.Events, change.Event{
		ID:       "ev-2",
		ChangeID: id.ChangeID("PAY-142"),
		Type:     change.EventPROpened,
	})
	require.True(t, waitStep.Done(view))
}

func TestWaitStepWithValidation(t *testing.T) {
	// Empty with
	_, err := engine.NewWait(nil)
	require.Error(t, err)

	// Missing event key
	_, err = engine.NewWait(json.RawMessage(`{}`))
	require.Error(t, err)

	// Unknown event type
	_, err = engine.NewWait(json.RawMessage(`{"event":"nonexistent.event"}`))
	require.Error(t, err)

	// Extra fields
	_, err = engine.NewWait(json.RawMessage(`{"event":"pr.opened","extra":123}`))
	require.Error(t, err)
}
