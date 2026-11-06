package graphiti

import (
	"context"
	"encoding/json"
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
