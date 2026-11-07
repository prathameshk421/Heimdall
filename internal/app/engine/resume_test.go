package engine_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

// A person can resume an escalated step (A10 item 5). The resume releases the hold: the step is offered again and
// called, and its view starts again at the resume.
func TestAResumedStepIsCalledAgain(t *testing.T) {
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
	_, err = e.Advance(ctx, ticket, submission(h))
	require.NoError(t, err)
	require.Equal(t, change.EventStepEscalated, lastEvent(t, h).Type)

	starts := qa.starts
	_, err = e.Advance(ctx, ticket, nil)
	require.NoError(t, err)
	require.Equal(t, starts, qa.starts, "before the resume, an escalated step is not called")

	ana := change.Actor{Kind: change.ActorHuman, ID: "ana"}
	_, err = e.Advance(ctx, ticket, h.incoming(change.EventStepResumed, ana, change.StepResumed{Step: "test_qa", By: "ana"}))
	require.NoError(t, err)
	require.Greater(t, qa.starts, starts, "the resumed step is called again")
}
