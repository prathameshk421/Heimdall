package agentloop

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

func withReasoning(step modelStep, r ports.ModelReasoning) modelStep {
	step.resp.Reasoning = r
	return step
}

func textAnswer(content string) modelStep {
	return modelStep{resp: ports.ModelResponse{Content: content, Finish: ports.ModelFinishStop,
		Usage: ports.ModelUsage{InputTokens: 100, OutputTokens: 10}, CostUSD: 0.001}}
}

// assistants returns the assistant messages of a request, in order.
func assistants(req ports.ModelRequest) []ports.ModelMessage {
	var out []ports.ModelMessage
	for _, m := range req.Messages {
		if m.Role == ports.ModelRoleAssistant {
			out = append(out, m)
		}
	}
	return out
}

func TestTheConversationKeepsTheReasoningOfEachAnswer(t *testing.T) {
	first := ports.ModelReasoning{Text: "List the changes first.", Field: "reasoning_content"}
	third := ports.ModelReasoning{Text: "Now submit.", Field: "reasoning", Details: json.RawMessage(`[{"type":"reasoning.text","signature":"s"}]`)}
	h := newHarness(t,
		withReasoning(answer(0.01, call("c1", "read_code", `{"path":"a.go"}`)), first),
		answer(0.01, call("c2", "read_code", `{"path":"b.go"}`)),
		withReasoning(answer(0.01, call("c3", "read_code", `{"path":"c.go"}`)), third),
		submitted(0.01),
	)
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	require.Equal(t, StopSubmitted, res.Reason)

	last := assistants(h.model.requests[3])
	require.Len(t, last, 3)
	assert.Equal(t, first, last[0].Reasoning)
	assert.True(t, last[1].Reasoning.IsZero(), "an answer without reasoning keeps none")
	assert.Equal(t, third, last[2].Reasoning)
	assert.Equal(t, first, assistants(h.model.requests[1])[0].Reasoning, "the next call already carries it")
}

func TestATextCallAndANudgeKeepTheReasoning(t *testing.T) {
	plain := ports.ModelReasoning{Text: "I will think in prose.", Field: "reasoning_content"}
	texted := ports.ModelReasoning{Text: "Call it as text.", Field: "reasoning_content"}
	h := newHarness(t,
		withReasoning(textAnswer("Let me look first."), plain),
		withReasoning(textAnswer(`<tool_call>{"name": "submit_plan", "arguments": {"plan": "p"}}</tool_call>`), texted),
	)
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	require.Equal(t, StopSubmitted, res.Reason)
	require.Equal(t, 1, res.Nudges)

	got := assistants(ports.ModelRequest{Messages: res.Transcript})
	require.Len(t, got, 2)
	assert.Equal(t, plain, got[0].Reasoning, "the nudged answer keeps its reasoning")
	assert.Equal(t, texted, got[1].Reasoning, "the text call is sent back as a real call with its reasoning")
	assert.Empty(t, got[1].Content)
	require.Len(t, got[1].ToolCalls, 1)
}

func TestTheTurnRecordHoldsTheFirstTwoKilobytesOfTheReasoning(t *testing.T) {
	long := strings.Repeat("é", 1500) // 3000 bytes
	h := newHarness(t, withReasoning(submitted(0.01), ports.ModelReasoning{Text: long, Field: "reasoning_content"}))
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	require.Len(t, res.Turn, 1)
	assert.Equal(t, len(long), res.Turn[0].ReasoningBytes)
	assert.Len(t, res.Turn[0].Reasoning, 2048)
	assert.True(t, strings.HasPrefix(long, res.Turn[0].Reasoning))
	assert.Equal(t, long, assistants(ports.ModelRequest{Messages: res.Transcript})[0].Reasoning.Text, "the conversation is never cut")
}

func hashedServerError(hash string) modelStep {
	return modelStep{err: &ports.ModelError{Kind: ports.ModelErrorServer, Status: http.StatusInternalServerError,
		RequestBytes: 4267, RequestHash: hash}}
}

func TestAFailedAttemptRecordsWhetherItSentTheSameBody(t *testing.T) {
	h := newHarness(t, hashedServerError("aaa"), hashedServerError("aaa"), hashedServerError("bbb"))
	h.withRetry()
	h.cfg.Deadline = h.clock.now.Add(30 * time.Second) // the third wait (24 s) does not fit, so the run ends there
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, StopDeadline, res.Reason)
	assert.Equal(t, []ports.AgentAttempt{
		{Kind: ports.ModelErrorServer, Status: 500, RequestBytes: 4267, RequestHash: "aaa", Wait: 6 * time.Second},
		{Kind: ports.ModelErrorServer, Status: 500, RequestBytes: 4267, RequestHash: "aaa", SameRequest: true, Wait: 12 * time.Second},
		{Kind: ports.ModelErrorServer, Status: 500, RequestBytes: 4267, RequestHash: "bbb"},
	}, res.FailedCall)
}
