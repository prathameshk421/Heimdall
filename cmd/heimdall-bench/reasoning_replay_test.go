//go:build benchreplay

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// The shape of chi-1051-clean in live run 3 (BENCH.md, "Diagnosis of live run 3"), played over HTTP through the real
// OpenAI-compatible client, the real agent loop and the real review pipeline on the real case. The model reasons and
// calls list_changes on turn 1; the endpoint, in thinking mode with tools, refuses turn 2 with the Deploy HTTP 500
// unless turn 1's reasoning_content comes back.

const chiReasoning = "The change swaps a prefix check for strings.CutPrefix. I should list the changes first."

// thinkingEndpoint answers list_changes with reasoning, then finish_review. It refuses any call whose earlier
// assistant turn lacks the reasoning it gave, as DeepSeek does (https://api-docs.deepseek.com/guides/thinking_mode).
// refuseAll refuses every call after the first, to show how a refusal on an identical body is recorded.
type thinkingEndpoint struct {
	mu        sync.Mutex
	answered  int
	refused   int
	refuseAll bool
}

func (s *thinkingEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var body struct {
		Messages []struct {
			Role             string  `json:"role"`
			ReasoningContent *string `json:"reasoning_content"`
		} `json:"messages"`
	}
	if raw, err := io.ReadAll(r.Body); err != nil || json.Unmarshal(raw, &body) != nil {
		http.Error(w, `{"error":{"message":"unreadable request"}}`, http.StatusBadRequest)
		return
	}
	lacks := false
	for _, m := range body.Messages {
		if m.Role == "assistant" && (m.ReasoningContent == nil || *m.ReasoningContent != chiReasoning) {
			lacks = true
		}
	}
	if lacks || (s.refuseAll && s.answered > 0) {
		s.refused++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"status_code":500,"message":"Internal Server Error"}`)
		return
	}
	s.answered++
	call, args, reasoning := "list_changes", "{}", fmt.Sprintf(`,"reasoning_content":%q`, chiReasoning)
	if s.answered > 1 {
		call, args, reasoning = "finish_review", `{"summary":"A refactor with no behaviour change."}`, ""
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":""%s,"tool_calls":[{"id":"call_%d","type":"function",`+
		`"function":{"name":%q,"arguments":%q}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":1022,"completion_tokens":17}}`,
		reasoning, s.answered, call, args)
}

func thinkingOptions(t *testing.T, url string) reviewOptions {
	t.Helper()
	o := replayOptions(t, "chi-1051-clean", testingCapUSD)
	o.out = filepath.Join(t.TempDir(), "reasoning.json")
	o.md = ""
	o.baseURL, o.requiredToolChoice = url, true
	o.keyFile = filepath.Join(t.TempDir(), "model_key")
	require.NoError(t, os.WriteFile(o.keyFile, []byte("sk-test-not-a-real-key\n"), 0o600))
	return o
}

func TestReplayTheChi1051ShapeSendsTheReasoningBack(t *testing.T) {
	srv := &thinkingEndpoint{}
	hs := httptest.NewServer(srv)
	t.Cleanup(hs.Close)
	o := thinkingOptions(t, hs.URL)
	model, err := liveModel(o)
	require.NoError(t, err)

	code, err := reviewRun(context.Background(), o, execRunner{}, model, testWriter{t})
	require.NoError(t, err)
	require.Equal(t, exitAgrees, code)

	require.Zero(t, srv.refused, "turn 2 carried turn 1's reasoning_content")
	c := readReport(t, o.out).Cases[0]
	require.Equal(t, "chi-1051-clean", c.ID)
	require.True(t, c.Scored, c.Error)
	require.Equal(t, "done", string(c.Stop))
	require.False(t, c.FalsePositive)
	require.Zero(t, c.Retries)
	require.Len(t, c.Transcript, 2)
	require.Equal(t, "list_changes", c.Transcript[0].Calls[0].Name)
	require.Equal(t, chiReasoning, c.Transcript[0].Reasoning)
	require.Equal(t, len(chiReasoning), c.Transcript[0].ReasoningBytes)
	require.Empty(t, c.Transcript[1].Reasoning)
}

func TestReplayARefusalOnTheSameBodyIsRecordedAsARepeat(t *testing.T) {
	srv := &thinkingEndpoint{refuseAll: true}
	hs := httptest.NewServer(srv)
	t.Cleanup(hs.Close)
	o := thinkingOptions(t, hs.URL)
	model, err := liveModel(o)
	require.NoError(t, err)

	code, err := reviewRun(context.Background(), o, execRunner{}, model, testWriter{t})
	require.NoError(t, err)
	require.Equal(t, exitDisagree, code, "a case the endpoint refused is not scored")

	c := readReport(t, o.out).Cases[0]
	require.False(t, c.Scored)
	require.Equal(t, "error", string(c.Stop))
	require.Equal(t, 3, srv.refused, "the retry rule is unchanged: 3 attempts")
	require.Len(t, c.FailedCall, 3)
	first := c.FailedCall[0]
	require.Positive(t, first.RequestBytes)
	require.Len(t, first.RequestSHA256, 64)
	require.False(t, first.SameRequest)
	for _, a := range c.FailedCall[1:] {
		require.Equal(t, first.RequestSHA256, a.RequestSHA256)
		require.True(t, a.SameRequest, "the same body was refused again")
	}
	raw, err := os.ReadFile(o.out)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"same_request": true`)
	require.NotContains(t, string(raw), "sk-test-not-a-real-key")
}
