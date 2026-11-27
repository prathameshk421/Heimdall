package openrouter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// reasoningTurn is one answer of the fake thinking-mode provider: the tool it calls, and the reasoning it gives in the
// named wire field (empty field for none). details is OpenRouter's reasoning_details, sent and expected back raw.
type reasoningTurn struct {
	tool    string
	field   string
	text    string
	details string
}

// thinkingServer is an OpenAI-compatible endpoint in thinking mode with tools. Like DeepSeek
// (https://api-docs.deepseek.com/guides/thinking_mode), it refuses any call whose earlier assistant turn lacks the
// reasoning it gave with that turn. It refuses with the HTTP 500 the Deploy endpoint sent in live run 3.
type thinkingServer struct {
	turns []reasoningTurn
	mu    sync.Mutex
	calls int
	// given maps a tool call id to the turn that made it, so a later call is checked against what was given.
	given   map[string]reasoningTurn
	refused []string
}

const deployRefusal = `{"status_code":500,"message":"Internal Server Error"}`

func (s *thinkingServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var body struct {
		Messages []map[string]json.RawMessage `json:"messages"`
	}
	if raw, err := io.ReadAll(r.Body); err != nil || json.Unmarshal(raw, &body) != nil {
		http.Error(w, `{"error":{"message":"unreadable request"}}`, http.StatusBadRequest)
		return
	}
	if why := s.missing(body.Messages); why != "" {
		s.refused = append(s.refused, why)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, deployRefusal)
		return
	}
	turn := s.turns[s.calls]
	s.calls++
	id := fmt.Sprintf("call_%d", s.calls)
	s.given[id] = turn
	msg := fmt.Sprintf(`{"role":"assistant","content":"","tool_calls":[{"id":%q,"type":"function","function":{"name":%q,"arguments":"{}"}}]`, id, turn.tool)
	if turn.field != "" {
		msg += fmt.Sprintf(`,%q:%q`, turn.field, turn.text)
	}
	if turn.details != "" {
		msg += `,"reasoning_details":` + turn.details
	}
	msg += "}"
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"choices":[{"message":%s,"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`, msg)
}

// missing names the first earlier assistant turn whose reasoning is not sent back as given, or is empty.
func (s *thinkingServer) missing(msgs []map[string]json.RawMessage) string {
	for i, m := range msgs {
		if string(m["role"]) != `"assistant"` {
			continue
		}
		var calls []struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(m["tool_calls"], &calls) != nil || len(calls) == 0 {
			return fmt.Sprintf("message %d has no tool call", i)
		}
		turn := s.given[calls[0].ID]
		if turn.field != "" {
			var got string
			if json.Unmarshal(m[turn.field], &got) != nil || got != turn.text {
				return fmt.Sprintf("message %d lacks its %s", i, turn.field)
			}
		}
		for _, other := range []string{"reasoning_content", "reasoning"} {
			if _, sent := m[other]; sent && other != turn.field {
				return fmt.Sprintf("message %d carries %s it was never given", i, other)
			}
		}
		if !bytes.Equal(m["reasoning_details"], []byte(turn.details)) {
			return fmt.Sprintf("message %d changed its reasoning_details: %s", i, m["reasoning_details"])
		}
	}
	return ""
}

// converse plays a tool conversation the way the agent loop does: it appends each answer as an assistant message with
// its reasoning, then one tool result per call.
func converse(t *testing.T, client ChatClient, turns int) ([]ports.ModelMessage, error) {
	t.Helper()
	msgs := []ports.ModelMessage{{Role: ports.ModelRoleUser, Content: "Review this change."}}
	tools := []ports.ModelTool{{Name: "list_changes", Parameters: json.RawMessage(`{"type":"object"}`)}}
	for range turns {
		resp, err := client.Complete(context.Background(), ports.ModelRequest{Model: chatModel, Messages: msgs, Tools: tools, MaxOutputTokens: 400})
		if err != nil {
			return msgs, err
		}
		msgs = append(msgs, ports.ModelMessage{Role: ports.ModelRoleAssistant, Content: resp.Content, ToolCalls: resp.ToolCalls, Reasoning: resp.Reasoning})
		for _, c := range resp.ToolCalls {
			msgs = append(msgs, ports.ModelMessage{Role: ports.ModelRoleTool, ToolCallID: c.ID, Content: "ok"})
		}
	}
	return msgs, nil
}

func thinkingClient(t *testing.T, turns []reasoningTurn) (*thinkingServer, ChatClient) {
	t.Helper()
	srv := &thinkingServer{turns: turns, given: map[string]reasoningTurn{}}
	hs := httptest.NewServer(srv)
	t.Cleanup(hs.Close)
	return srv, ChatClient{BaseURL: hs.URL, Secrets: probeSecrets{"key": "k"}, KeyName: "key", Prices: chatPrice()}
}

func TestChatSendsBackTheOpenRouterReasoningAndItsDetailsUnchanged(t *testing.T) {
	details := `[{"type":"reasoning.encrypted","data":"gAAAAB&x<y>z==","id":"rs_1","format":"anthropic-claude-v1","index":0}]`
	srv, client := thinkingClient(t, []reasoningTurn{
		{tool: "list_changes", field: "reasoning", text: "List first.", details: details},
		{tool: "list_changes"},
		{tool: "list_changes", field: "reasoning", text: "Now read compress.go.", details: `[{"type":"reasoning.text","text":"Now read compress.go.","signature":"sig/+=","index":0}]`},
		{tool: "list_changes"},
	})

	msgs, err := converse(t, client, 4)

	require.NoError(t, err, "refusals: %v", srv.refused)
	require.Equal(t, 4, srv.calls)
	require.Equal(t, details, string(msgs[1].Reasoning.Details))
}

func TestChatRefusalRepeatsWhenTheReasoningIsDropped(t *testing.T) {
	// The defect of live run 3: the conversation loses the reasoning, and every later call is refused the same way.
	srv, client := thinkingClient(t, []reasoningTurn{{tool: "list_changes", field: "reasoning_content", text: "think"}, {tool: "list_changes"}})
	resp, err := client.Complete(context.Background(), ports.ModelRequest{Model: chatModel, MaxOutputTokens: 400,
		Messages: []ports.ModelMessage{{Role: ports.ModelRoleUser, Content: "Review."}}})
	require.NoError(t, err)
	dropped := ports.ModelRequest{Model: chatModel, MaxOutputTokens: 400, Messages: []ports.ModelMessage{
		{Role: ports.ModelRoleUser, Content: "Review."},
		{Role: ports.ModelRoleAssistant, ToolCalls: resp.ToolCalls},
		{Role: ports.ModelRoleTool, ToolCallID: resp.ToolCalls[0].ID, Content: "ok"},
	}}

	var hashes []string
	for range 2 {
		_, err := client.Complete(context.Background(), dropped)
		var modelErr *ports.ModelError
		require.ErrorAs(t, err, &modelErr)
		require.Equal(t, ports.ModelErrorServer, modelErr.Kind)
		hashes = append(hashes, modelErr.RequestHash)
	}

	require.Len(t, srv.refused, 2)
	require.Len(t, hashes[0], 64)
	require.Equal(t, hashes[0], hashes[1], "the same body has the same hash, so the repeat shows in the record")
}

func TestChatEchoesAnEmptyReasoningFieldItWasGiven(t *testing.T) {
	// An answer that carries the field with an empty value gets the same field back, so the endpoint sees what it sent.
	srv, client := thinkingClient(t, []reasoningTurn{{tool: "list_changes", field: "reasoning_content"}, {tool: "list_changes"}})
	msgs, err := converse(t, client, 2)
	require.NoError(t, err, "refusals: %v", srv.refused)
	require.Equal(t, ports.ModelReasoning{Field: "reasoning_content"}, msgs[1].Reasoning)

	raw, err := chatRequestBody(ports.ModelRequest{Model: chatModel, Messages: msgs}, "")
	require.NoError(t, err)
	require.Contains(t, string(raw), `"reasoning_content":""`)
}

func TestChatHashesTheRefusedBody(t *testing.T) {
	var sent []byte
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, deployRefusal)
	}))
	t.Cleanup(hs.Close)
	client := ChatClient{BaseURL: hs.URL, Secrets: probeSecrets{"key": "k"}, KeyName: "key"}
	_, err := client.Complete(context.Background(), chatAnswerRequest())
	var modelErr *ports.ModelError
	require.ErrorAs(t, err, &modelErr)
	sum := sha256.Sum256(sent)
	require.Equal(t, hex.EncodeToString(sum[:]), modelErr.RequestHash)
	require.Equal(t, len(sent), modelErr.RequestBytes)
}
