package graphiti

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHTTPCallerUsesMCPToolsCallAndDecodesStructuredContent(t *testing.T) {
	var request struct {
		JSONRPC string `json:"jsonrpc"`
		ID      uint64 `json:"id"`
		Method  string `json:"method"`
		Params  struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		} `json:"params"`
	}
	var requestMethod string
	var accept string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestMethod = r.Method
		accept = r.Header.Get("Accept")
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"structuredContent": map[string]any{"facts": []any{}}}})
	}))
	defer server.Close()
	caller, err := NewHTTPCaller(server.URL, nil)
	require.NoError(t, err)
	result, err := caller.Call(context.Background(), "search_memory_facts", map[string]any{"group_ids": []string{"payments"}})
	require.NoError(t, err)
	require.Equal(t, http.MethodPost, requestMethod)
	require.Contains(t, accept, "text/event-stream")
	require.Equal(t, "tools/call", request.Method)
	require.Equal(t, "search_memory_facts", request.Params.Name)
	require.Equal(t, "payments", request.Params.Arguments["group_ids"].([]any)[0])
	require.JSONEq(t, `{"facts":[]}`, string(result))
}

func TestHTTPCallerRefusesMismatchedResponseID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":999,"result":{"content":[{"text":"{}"}]}}`))
	}))
	defer server.Close()
	caller, err := NewHTTPCaller(server.URL, nil)
	require.NoError(t, err)
	_, err = caller.Call(context.Background(), "search_memory_facts", map[string]any{})
	require.ErrorContains(t, err, "id does not match")
}

// TestHTTPCallerOpensASessionAndReopensItWhenRefused follows the live Graphiti server, which refuses a call without
// the Mcp-Session-Id that initialize returned, and forgets sessions on restart.
func TestHTTPCallerOpensASessionAndReopensItWhenRefused(t *testing.T) {
	sessions := 0
	live := ""
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg struct {
			ID     *uint64 `json:"id"`
			Method string  `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			http.Error(w, "bad json", http.StatusUnprocessableEntity)
			return
		}
		calls = append(calls, msg.Method)
		if msg.Method == "initialize" {
			sessions++
			live = fmt.Sprintf("s%d", sessions)
			w.Header().Set("Mcp-Session-Id", live)
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": *msg.ID, "result": map[string]any{}})
			return
		}
		if r.Header.Get("Mcp-Session-Id") != live {
			http.Error(w, `{"error":"Missing session ID"}`, http.StatusBadRequest)
			return
		}
		if msg.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": *msg.ID, "result": map[string]any{"structuredContent": map[string]any{"ok": true}}})
	}))
	defer server.Close()
	caller, err := NewHTTPCaller(server.URL, nil)
	require.NoError(t, err)

	_, err = caller.Call(context.Background(), "get_status", map[string]any{})
	require.NoError(t, err)
	_, err = caller.Call(context.Background(), "get_status", map[string]any{})
	require.NoError(t, err)
	require.Equal(t, 1, sessions, "a second call reuses the session")

	live = "restarted"
	result, err := caller.Call(context.Background(), "get_status", map[string]any{})
	require.NoError(t, err)
	require.JSONEq(t, `{"ok":true}`, string(result))
	require.Equal(t, 2, sessions, "a refused session is opened again once")
	require.Equal(t, []string{"initialize", "notifications/initialized", "tools/call", "tools/call", "tools/call", "initialize", "notifications/initialized", "tools/call"}, calls)
}

func TestHTTPCallerUnwrapsTheResultKeyTheLiveServerAddsToStructuredContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg rpc
		_ = json.NewDecoder(r.Body).Decode(&msg)
		if msg.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		result := map[string]any{"serverInfo": 1}
		if msg.Method == "tools/call" {
			result = map[string]any{"structuredContent": map[string]any{"result": map[string]any{"message": "ok", "facts": []any{map[string]any{"uuid": "f-1"}}}}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": *msg.ID, "result": result})
	}))
	defer server.Close()
	caller, err := NewHTTPCaller(server.URL, server.Client())
	require.NoError(t, err)
	raw, err := caller.Call(context.Background(), "search_memory_facts", map[string]any{})
	require.NoError(t, err)
	items, err := decodeItems(raw)
	require.NoError(t, err)
	require.Len(t, items, 1)
}
