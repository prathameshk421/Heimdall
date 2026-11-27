package agentloop

import (
	"context"
	"net/http"
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
