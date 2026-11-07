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

func TestSlopStepContract(t *testing.T) {
	slopStep := engine.NewSlop()
	require.Equal(t, "rule.slop", slopStep.Type())

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
	// No slop.evaluated in window -> Done is false
	require.False(t, slopStep.Done(view))

	// Start returns nothing and no error
	res, err := slopStep.Start(context.Background(), view, nil)
	require.NoError(t, err)
	require.Empty(t, res.Events)
	require.Empty(t, res.Effects)

	// slop.evaluated with pass false
	failPayload, err := json.Marshal(change.SlopEvaluated{Pass: false, FailingRules: []string{"ticket_linked"}})
	require.NoError(t, err)
	view.Events = append(view.Events, change.Event{
		ID:       "ev-2",
		ChangeID: id.ChangeID("PAY-142"),
		Type:     change.EventSlopEvaluated,
		Payload:  failPayload,
	})

	// Failed slop leaves step not Done and Start returns nothing (waits for new push)
	require.False(t, slopStep.Done(view))
	res, err = slopStep.Start(context.Background(), view, nil)
	require.NoError(t, err)
	require.Empty(t, res.Events)
	require.Empty(t, res.Effects)

	// A new push arrives with pass: true
	passPayload, err := json.Marshal(change.SlopEvaluated{Pass: true})
	require.NoError(t, err)
	view.Events = append(view.Events, change.Event{
		ID:       "ev-3",
		ChangeID: id.ChangeID("PAY-142"),
		Type:     change.EventSlopEvaluated,
		Payload:  passPayload,
	})

	// Now Done is true
	require.True(t, slopStep.Done(view))
	res, err = slopStep.Start(context.Background(), view, nil)
	require.NoError(t, err)
	require.Empty(t, res.Events)
	require.Empty(t, res.Effects)

	// OnEvent and OnTimeout return nothing
	res, err = slopStep.OnEvent(context.Background(), view, view.Events[0])
	require.NoError(t, err)
	require.Empty(t, res.Events)

	res, err = slopStep.OnTimeout(context.Background(), view)
	require.NoError(t, err)
	require.Empty(t, res.Events)
}

func TestSlopStepReversal(t *testing.T) {
	slopStep := engine.NewSlop()

	passPayload, err := json.Marshal(change.SlopEvaluated{Pass: true})
	require.NoError(t, err)
	failPayload, err := json.Marshal(change.SlopEvaluated{Pass: false, FailingRules: []string{"scope"}})
	require.NoError(t, err)

	// First passed, then newer failed -> Done is false
	view := engine.ChangeView{
		ID: id.ChangeID("PAY-142"),
		Events: []change.Event{
			{ID: "ev-1", Type: change.EventSlopEvaluated, Payload: passPayload},
			{ID: "ev-2", Type: change.EventSlopEvaluated, Payload: failPayload},
		},
	}
	require.False(t, slopStep.Done(view))
}
