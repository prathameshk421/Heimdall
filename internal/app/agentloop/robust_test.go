package agentloop

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

func text(content string) modelStep {
	return modelStep{resp: ports.ModelResponse{Content: content, Finish: ports.ModelFinishStop,
		Usage: ports.ModelUsage{InputTokens: 100, OutputTokens: 10}, CostUSD: 0.001}}
}

const dsmlRead = "Reading.\n<｜DSML｜function_calls>\n<｜DSML｜invoke name=\"read_code\">\n" +
	"<｜DSML｜parameter name=\"path\" string=\"true\">a.go</｜DSML｜parameter>\n</｜DSML｜invoke>\n</｜DSML｜function_calls>"

func TestATextToolCallRunsLikeAToolCall(t *testing.T) {
	h := newHarness(t, text(dsmlRead), answer(0.01, call("c2", "submit_plan", `{"plan":"p"}`)))
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, StopSubmitted, res.Reason)
	assert.Equal(t, []string{`{"path":"a.go"}`}, h.read.got)
	assert.Zero(t, res.Nudges, "a parsed call needs no nudge")
	require.Len(t, res.Turn, 2)
	assert.True(t, res.Turn[0].TextCalls)
	assert.Equal(t, []ports.AgentCallBrief{{Name: "read_code", ArgumentBytes: len(`{"path":"a.go"}`)}}, res.Turn[0].Calls)
	assert.Equal(t, 1, res.Turn[0].ToolCalls)
	// The model sees its text call as a real call with a result, so the next answer follows the wire form.
	second := h.model.requests[1].Messages
	require.Len(t, second, 3)
	assert.Equal(t, ports.ModelRoleAssistant, second[1].Role)
	require.Len(t, second[1].ToolCalls, 1)
	assert.Equal(t, "read_code", second[1].ToolCalls[0].Name)
	assert.Equal(t, second[1].ToolCalls[0].ID, second[2].ToolCallID)
	assert.Equal(t, "package x", second[2].Content)
}

func TestATextSubmissionIsValidatedLikeAToolCall(t *testing.T) {
	h := newHarness(t, text("```json\n{\"name\":\"submit_plan\",\"arguments\":{\"plan\":\"p\"}}\n```"))
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, StopSubmitted, res.Reason)
	assert.Equal(t, []string{`{"plan":"p"}`}, h.sub.got)
}

func TestATextAnswerGetsANudgeAndTheRunGoesOn(t *testing.T) {
	h := newHarness(t, text("Let me think about the change."), answer(0.01, call("c2", "submit_plan", `{"plan":"p"}`)))
	h.cfg.Nudge = "Call a tool now. If you found no bug you can prove, call finish_review."
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, StopSubmitted, res.Reason)
	assert.Equal(t, 1, res.Nudges)
	require.Len(t, res.Turn, 2)
	assert.True(t, res.Turn[0].Nudged)
	assert.False(t, res.Turn[1].Nudged)
	second := h.model.requests[1].Messages
	last := second[len(second)-1]
	assert.Equal(t, ports.ModelRoleUser, last.Role)
	assert.Equal(t, h.cfg.Nudge, last.Content)
	assert.Equal(t, "Let me think about the change.", second[len(second)-2].Content)
}

func TestTheDefaultNudgeNamesTheFinalTools(t *testing.T) {
	h := newHarness(t, text("hmm"), answer(0.01, call("c2", "submit_plan", `{"plan":"p"}`)))
	_, err := h.run(context.Background(), t)
	require.NoError(t, err)
	second := h.model.requests[1].Messages
	assert.Equal(t, "Call a tool now. When you are done, call submit_plan.", second[len(second)-1].Content)
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

func TestNoToolChoiceIsSentUnlessTheRunRequiresACall(t *testing.T) {
	h := newHarness(t, answer(0.01, call("c1", "submit_plan", `{"plan":"p"}`)))
	_, err := h.run(context.Background(), t)
	require.NoError(t, err)
	assert.Empty(t, h.model.requests[0].ToolChoice)
}

func TestEachTurnKeepsABoundedTranscript(t *testing.T) {
	long := strings.Repeat("é", turnTextBytes) // two bytes a rune, so a byte cut would split one
	step := answer(0.01, call("c1", "read_code", `{"path":"a.go"}`))
	step.resp.Content = long
	h := newHarness(t, step, text("short"), answer(0.01, call("c3", "submit_plan", `{"plan":"p"}`)))
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	require.Len(t, res.Turn, 3)
	t0 := res.Turn[0]
	assert.LessOrEqual(t, len(t0.Text), turnTextBytes)
	assert.True(t, utf8.ValidString(t0.Text))
	assert.True(t, strings.HasPrefix(long, t0.Text))
	assert.Equal(t, ports.ModelFinishToolCalls, t0.Finish)
	assert.Equal(t, []ports.AgentCallBrief{{Name: "read_code", ArgumentBytes: 15}}, t0.Calls)
	assert.False(t, t0.Nudged)
	assert.False(t, t0.TextCalls)
	assert.Equal(t, "short", res.Turn[1].Text)
	assert.True(t, res.Turn[1].Nudged)
	assert.Equal(t, ports.ModelFinishStop, res.Turn[1].Finish)
}

func TestTheTranscriptKeepsABoundedNumberOfCallsPerTurn(t *testing.T) {
	var calls []ports.ModelToolCall
	for i := 0; i < turnCallBriefs+5; i++ {
		calls = append(calls, call("c", "read_code", `{}`))
	}
	h := newHarness(t, answer(0.01, calls...), answer(0.01, call("c2", "submit_plan", `{"plan":"p"}`)))
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	assert.Len(t, res.Turn[0].Calls, turnCallBriefs)
	assert.Equal(t, turnCallBriefs+5, res.Turn[0].ToolCalls)
}

func TestContractTheRunCarriesTheNudgeAndTheToolChoice(t *testing.T) {
	c := newContract(t, text("thinking"), answer(0.01, call("c2", "finish", `{}`)))
	c.run.RequireToolCall = true
	c.run.Nudge = "Call a tool now."
	res, err := c.do(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, ports.AgentStopDone, res.Stop)
	require.NotNil(t, res.Submission)
	assert.Equal(t, 1, res.Nudges)
	assert.Equal(t, ports.ModelToolChoiceRequired, c.model.requests[0].ToolChoice)
	msgs := c.model.requests[1].Messages
	assert.Equal(t, "Call a tool now.", msgs[len(msgs)-1].Content)
	assert.True(t, res.Turns[0].Nudged)
}
