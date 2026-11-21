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
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// The failure shapes the first live run met (BENCH.md, "Diagnosis of the first live run"), played over HTTP through
// the real OpenAI-compatible client, the real agent loop and the real review pipeline on one real case.

const htmlBadRequest = "<html><body><h1>400 Bad request</h1> Your browser sent an invalid request. </body></html>"

// shapeServer is an OpenAI-compatible endpoint that refuses tool_choice "required" with an HTML 400, answers once in
// plain text, then plays a recorded script with its first turns written as text tool calls in three formats.
type shapeServer struct {
	t       *testing.T
	s       script
	mu      sync.Mutex
	turn    int
	texted  bool
	choices []string
	always  bool // refuse every call with the HTML 400, whatever its tool choice
}

func (s *shapeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := io.ReadAll(r.Body)
	require.NoError(s.t, err)
	var body struct {
		ToolChoice string `json:"tool_choice"`
		Messages   []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	require.NoError(s.t, json.Unmarshal(raw, &body))
	s.choices = append(s.choices, body.ToolChoice)
	if s.always || body.ToolChoice == "required" {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, htmlBadRequest)
		return
	}
	if !s.texted {
		s.texted = true
		s.answer(w, "Let me look at the change before I call anything.", nil, 0.0001)
		return
	}
	if s.turn >= len(s.s.Turns) {
		http.Error(w, `{"error":{"message":"no recorded turn left"}}`, http.StatusInternalServerError)
		return
	}
	turn := s.s.Turns[s.turn]
	s.turn++
	if turn.Expect != "" {
		var tools []string
		for i := len(body.Messages) - 1; i >= 0 && body.Messages[i].Role == "tool"; i-- {
			tools = append(tools, body.Messages[i].Content)
		}
		if !strings.Contains(strings.Join(tools, "\n"), turn.Expect) {
			http.Error(w, fmt.Sprintf(`{"error":{"message":"tool results lack %q"}}`, turn.Expect), http.StatusInternalServerError)
			return
		}
	}
	call := turn.Calls[0]
	switch s.turn {
	case 1:
		s.answer(w, "I will list the changes.\n"+dsmlCall(s.t, call.Name, call.Arguments), nil, turn.CostUSD)
	case 2:
		s.answer(w, fmt.Sprintf("```json\n{\"name\": %q, \"arguments\": %s}\n```", call.Name, call.Arguments), nil, turn.CostUSD)
	case 3:
		s.answer(w, fmt.Sprintf("<tool_call>{\"name\": %q, \"arguments\": %s}</tool_call>", call.Name, call.Arguments), nil, turn.CostUSD)
	default:
		var calls []map[string]any
		for i, c := range turn.Calls {
			calls = append(calls, map[string]any{"id": fmt.Sprintf("call-%d-%d", s.turn, i), "type": "function",
				"function": map[string]any{"name": c.Name, "arguments": string(c.Arguments)}})
		}
		s.answer(w, "", calls, turn.CostUSD)
	}
}

func (s *shapeServer) answer(w http.ResponseWriter, content string, calls []map[string]any, cost float64) {
	finish := "stop"
	if len(calls) > 0 {
		finish = "tool_calls"
	}
	msg := map[string]any{"role": "assistant", "content": content}
	if len(calls) > 0 {
		msg["tool_calls"] = calls
	}
	// Tokens chosen so that the client bills about the recorded cost at the replay price ($0.18 per million out).
	out := int64(cost / 0.18e-6)
	resp := map[string]any{"choices": []any{map[string]any{"message": msg, "finish_reason": finish}},
		"usage": map[string]any{"prompt_tokens": 0, "completion_tokens": out}}
	w.Header().Set("Content-Type", "application/json")
	require.NoError(s.t, json.NewEncoder(w).Encode(resp))
}

// dsmlCall writes a call the way DeepSeek prints it as text: string arguments raw, others as JSON.
func dsmlCall(t *testing.T, name string, args json.RawMessage) string {
	t.Helper()
	var obj map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(args, &obj))
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	fmt.Fprintf(&b, "<｜DSML｜function_calls>\n<｜DSML｜invoke name=%q>\n", name)
	for _, k := range keys {
		var str string
		if json.Unmarshal(obj[k], &str) == nil {
			fmt.Fprintf(&b, "<｜DSML｜parameter name=%q string=\"true\">%s</｜DSML｜parameter>\n", k, str)
			continue
		}
		fmt.Fprintf(&b, "<｜DSML｜parameter name=%q string=\"false\">%s</｜DSML｜parameter>\n", k, obj[k])
	}
	b.WriteString("</｜DSML｜invoke>\n</｜DSML｜function_calls>")
	return b.String()
}

func shapeOptions(t *testing.T, url string) reviewOptions {
	t.Helper()
	o := replayOptions(t, "testify-1207", testingCapUSD)
	o.out = filepath.Join(t.TempDir(), "shapes.json")
	o.md = ""
	o.baseURL, o.requiredToolChoice = url, true
	o.keyFile = filepath.Join(t.TempDir(), "model_key")
	require.NoError(t, os.WriteFile(o.keyFile, []byte("sk-test-not-a-real-key\n"), 0o600))
	return o
}

func TestReplayTheLiveFailureShapesEndToEnd(t *testing.T) {
	srv := &shapeServer{t: t, s: loadScripts(t, "testify-1207").scripts[0]}
	hs := httptest.NewServer(srv)
	t.Cleanup(hs.Close)
	o := shapeOptions(t, hs.URL)
	model, err := liveModel(o)
	require.NoError(t, err)

	code, err := reviewRun(context.Background(), o, execRunner{}, model, testWriter{t})
	require.NoError(t, err)
	require.Equal(t, exitAgrees, code)

	rep := readReport(t, o.out)
	require.Len(t, rep.Cases, 1)
	c := rep.Cases[0]
	require.True(t, c.Scored, c.Error)
	require.Equal(t, 1, c.Proposed)
	require.Equal(t, 1, c.Proven, "the finding proposed through the shapes is proven on the real case")
	require.True(t, c.Hit)
	require.Equal(t, 1, c.Nudges, "the plain text answer got one nudge")

	tr := c.Transcript
	require.Len(t, tr, 1+len(srv.s.Turns))
	require.Equal(t, "auto", tr[0].ToolChoice, "the endpoint refused required, so the client sent auto")
	require.Contains(t, tr[0].Fallback, "HTTP 400")
	require.Contains(t, tr[0].Fallback, "text/html")
	require.True(t, tr[0].Nudged)
	require.Zero(t, tr[0].ToolCalls)
	for i := 1; i <= 3; i++ {
		require.True(t, tr[i].TextCalls, "turn %d was a text tool call", i)
		require.Len(t, tr[i].Calls, 1)
		require.Positive(t, tr[i].Calls[0].ArgumentBytes+len(tr[i].Calls[0].Name))
	}
	require.Equal(t, "list_changes", tr[1].Calls[0].Name)
	require.Equal(t, "read_code", tr[2].Calls[0].Name)
	require.Equal(t, "search_code", tr[3].Calls[0].Name)
	require.False(t, tr[4].TextCalls)
	require.Equal(t, "finish_review", tr[len(tr)-1].Calls[0].Name)

	require.Equal(t, "required", srv.choices[0])
	for _, ch := range srv.choices[1:] {
		require.Equal(t, "auto", ch, "after one fallback the run asks for auto only")
	}
	t.Logf("tool choices sent: %v", srv.choices)
}

func TestReplayAnHTMLRefusalIsRecordedWithStatusTypeAndSize(t *testing.T) {
	srv := &shapeServer{t: t, s: loadScripts(t, "testify-1207").scripts[0], always: true}
	hs := httptest.NewServer(srv)
	t.Cleanup(hs.Close)
	o := shapeOptions(t, hs.URL)
	model, err := liveModel(o)
	require.NoError(t, err)

	code, err := reviewRun(context.Background(), o, execRunner{}, model, testWriter{t})
	require.NoError(t, err)
	require.Equal(t, exitDisagree, code, "a case the endpoint refused is not scored")

	c := readReport(t, o.out).Cases[0]
	require.False(t, c.Scored)
	require.Equal(t, "error", string(c.Stop))
	for _, want := range []string{"HTTP 400", "text/html", "request ", " bytes", "tool_choice required was refused", "400 Bad request"} {
		require.Contains(t, c.Error, want)
	}
	require.NotContains(t, c.Error, "sk-test-not-a-real-key")
	require.Equal(t, []string{"required", "auto"}, srv.choices, "the client falls back once, the loop does not retry")
}
