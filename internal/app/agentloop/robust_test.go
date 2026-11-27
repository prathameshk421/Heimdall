package agentloop

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

func text(content string) modelStep {
	return modelStep{resp: ports.ModelResponse{Content: content, Finish: ports.ModelFinishStop,
		Usage: ports.ModelUsage{InputTokens: 100, OutputTokens: 10}, CostUSD: 0.001}}
}

func TestNudgesCountTowardTheTurnCap(t *testing.T) {
	h := newHarness(t, text("one"), text("two"))
	h.cfg.MaxTurns = 2
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, StopTurnCap, res.Reason)
	assert.Equal(t, 2, res.Nudges)
}

func TestNudgesCountTowardTheBudget(t *testing.T) {
	h := newHarness(t, text("one"), text("two"), text("three"))
	// Each call's worst case is about $0.00084 here; the cap allows the first call and its nudge, not a third call.
	h.cfg.BudgetUSD = 0.0025
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, StopBudget, res.Reason)
	assert.Equal(t, 2, res.Turns)
}

func TestARequiredToolChoiceIsSentUntilTheAdapterFallsBack(t *testing.T) {
	first := answer(0.01, call("c1", "read_code", `{"path":"a.go"}`))
	first.resp.ToolChoice = ports.ModelToolChoiceAuto
	first.resp.Fallback = "tool_choice required refused: model invalid (HTTP 400)"
	h := newHarness(t, first,
		answer(0.01, call("c2", "read_code", `{"path":"b.go"}`)),
		answer(0.01, call("c3", "submit_plan", `{"plan":"p"}`)))
	h.cfg.RequireToolCall = true
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, StopSubmitted, res.Reason)
	assert.Equal(t, ports.ModelToolChoiceRequired, h.model.requests[0].ToolChoice)
	assert.Equal(t, ports.ModelToolChoiceAuto, h.model.requests[1].ToolChoice, "the run stops asking for what the endpoint refused")
	assert.Equal(t, ports.ModelToolChoiceAuto, h.model.requests[2].ToolChoice)
	assert.Equal(t, first.resp.Fallback, res.Turn[0].Fallback)
	assert.Equal(t, ports.ModelToolChoiceAuto, res.Turn[0].ToolChoice)
}
