package openrouter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const chatModel = "deepseek/deepseek-v4-flash"

// chatFixture reads one recorded OpenAI-compatible response.
func chatFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "chat", name))
	require.NoError(t, err)
	return raw
}

// chatPrice is the price table the tests bill against.
func chatPrice() map[string]Price {
	return map[string]Price{chatModel: {Input: 0.27, CachedInput: 0.07, Output: 1.1}}
}

// chatAnswerRequest is the smallest request the loop would send.
func chatAnswerRequest() ports.ModelRequest {
	return ports.ModelRequest{
		Model: chatModel,
		Messages: []ports.ModelMessage{
			{Role: ports.ModelRoleSystem, Content: "You review changes."},
			{Role: ports.ModelRoleUser, Content: "Review this change."},
		},
		MaxOutputTokens: 400,
	}
}

// chatRespond starts a server that answers every call with status, headers and body.
func chatRespond(t *testing.T, status int, headers map[string]string, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for name, value := range headers {
			w.Header().Set(name, value)
		}
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestChatReturnsTheAnswerUsageAndCost(t *testing.T) {
	var path, auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, auth = r.URL.Path, r.Header.Get("Authorization")
		_, _ = w.Write(chatFixture(t, "answer.json"))
	}))
	t.Cleanup(srv.Close)
	client := ChatClient{
		BaseURL: srv.URL,
		Secrets: probeSecrets{"key": "sk-or-v1-test"},
		KeyName: "key",
		Prices:  chatPrice(),
	}

	got, err := client.Complete(context.Background(), chatAnswerRequest())

	require.NoError(t, err)
	require.Equal(t, "/chat/completions", path)
	require.Equal(t, "Bearer sk-or-v1-test", auth)
	require.Equal(t, "The change adds a null check before the refund lookup. I would test the missing-refund path first.", got.Content)
	require.Empty(t, got.ToolCalls)
	require.Equal(t, ports.ModelFinishStop, got.Finish)
	require.Equal(t, ports.ModelUsage{InputTokens: 1204, OutputTokens: 86}, got.Usage)
	require.InDelta(t, 0.00041968, got.CostUSD, 1e-12)
}

func TestChatReturnsToolCallsAndBillsCachedInput(t *testing.T) {
	srv := chatRespond(t, http.StatusOK, nil, chatFixture(t, "tool_calls.json"))
	client := ChatClient{
		BaseURL: srv.URL,
		Secrets: probeSecrets{"key": "k"},
		KeyName: "key",
		Prices:  chatPrice(),
	}

	got, err := client.Complete(context.Background(), chatAnswerRequest())

	require.NoError(t, err)
	require.Empty(t, got.Content)
	require.Equal(t, ports.ModelFinishToolCalls, got.Finish)
	require.Equal(t, []ports.ModelToolCall{
		{ID: "call_01Hq", Name: "read_code", Arguments: `{"path":"payments/refund.go","start_line":40,"end_line":90}`},
		{ID: "call_01Hr", Name: "search_code", Arguments: `{"query":"RefundNotFound"}`},
	}, got.ToolCalls)
	require.Equal(t, ports.ModelUsage{InputTokens: 2400, CachedInputTokens: 2048, OutputTokens: 64}, got.Usage)
	require.InDelta(t, 0.0003088, got.CostUSD, 1e-12)
}

func TestChatReportsAContentFilterFinish(t *testing.T) {
	srv := chatRespond(t, http.StatusOK, nil, chatFixture(t, "content_filter.json"))
	client := ChatClient{BaseURL: srv.URL, Prices: chatPrice()}

	got, err := client.Complete(context.Background(), chatAnswerRequest())

	require.NoError(t, err)
	require.Equal(t, ports.ModelFinishContentFilter, got.Finish)
	require.Empty(t, got.Content)
	require.InDelta(t, 0.000243, got.CostUSD, 1e-12)
}

// chatRequestDoc is the request shape these tests read back from the wire.
type chatRequestDoc struct {
	Model     string `json:"model"`
	MaxTokens int    `json:"max_tokens"`
	Messages  []struct {
		Role       string `json:"role"`
		Content    string `json:"content"`
		ToolCallID string `json:"tool_call_id"`
		ToolCalls  []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
	} `json:"messages"`
	Tools []struct {
		Type     string `json:"type"`
		Function struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			Parameters  map[string]any `json:"parameters"`
		} `json:"function"`
	} `json:"tools"`
}

func TestChatSendsTheToolsAndTheToolResults(t *testing.T) {
	var rawBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write(chatFixture(t, "answer.json"))
	}))
	t.Cleanup(srv.Close)
	client := ChatClient{BaseURL: srv.URL, Secrets: probeSecrets{"key": "k"}, KeyName: "key", Prices: chatPrice()}
	req := ports.ModelRequest{
		Model: chatModel,
		Messages: []ports.ModelMessage{
			{Role: ports.ModelRoleUser, Content: "Review this change."},
			{Role: ports.ModelRoleAssistant, ToolCalls: []ports.ModelToolCall{
				{ID: "call_1", Name: "read_code", Arguments: `{"path":"a.go"}`},
			}},
			{Role: ports.ModelRoleTool, ToolCallID: "call_1", Content: "package a"},
		},
		Tools: []ports.ModelTool{{
			Name:        "read_code",
			Description: "Read one file at a commit.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
		}},
		MaxOutputTokens: 400,
	}

	_, err := client.Complete(context.Background(), req)

	require.NoError(t, err)
	var body chatRequestDoc
	require.NoError(t, json.Unmarshal(rawBody, &body))
	require.Equal(t, chatModel, body.Model)
	require.Equal(t, 400, body.MaxTokens)
	require.Len(t, body.Messages, 3)
	require.Equal(t, "assistant", body.Messages[1].Role)
	require.Equal(t, "call_1", body.Messages[1].ToolCalls[0].ID)
	require.Equal(t, "function", body.Messages[1].ToolCalls[0].Type)
	require.Equal(t, "read_code", body.Messages[1].ToolCalls[0].Function.Name)
	require.JSONEq(t, `{"path":"a.go"}`, body.Messages[1].ToolCalls[0].Function.Arguments)
	require.Equal(t, "tool", body.Messages[2].Role)
	require.Equal(t, "call_1", body.Messages[2].ToolCallID)
	require.Equal(t, "package a", body.Messages[2].Content)
	require.Len(t, body.Tools, 1)
	require.Equal(t, "function", body.Tools[0].Type)
	require.Equal(t, "read_code", body.Tools[0].Function.Name)
	require.Equal(t, "Read one file at a commit.", body.Tools[0].Function.Description)
	require.Equal(t, "object", body.Tools[0].Function.Parameters["type"])
}

func TestChatMapsProviderFailuresToTypedErrors(t *testing.T) {
	tests := []struct {
		name       string
		file       string
		status     int
		headers    map[string]string
		kind       ports.ModelErrorKind
		retryAfter time.Duration
		sentinel   error
	}{
		{"rate limited", "rate_limited.json", http.StatusTooManyRequests, map[string]string{"Retry-After": "30"}, ports.ModelErrorRateLimited, 30 * time.Second, ports.ErrUnavailable},
		{"context too long", "context_length.json", http.StatusBadRequest, nil, ports.ModelErrorContextTooLong, 0, nil},
		{"refused", "refused.json", http.StatusBadRequest, nil, ports.ModelErrorRefused, 0, nil},
		{"auth", "auth.json", http.StatusUnauthorized, nil, ports.ModelErrorAuth, 0, ports.ErrUnauthorized},
		{"server", "server_error.json", http.StatusInternalServerError, nil, ports.ModelErrorServer, 0, ports.ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := chatRespond(t, tt.status, tt.headers, chatFixture(t, tt.file))
			client := ChatClient{BaseURL: srv.URL, Secrets: probeSecrets{"key": "sk-or-v1-secret"}, KeyName: "key"}

			_, err := client.Complete(context.Background(), chatAnswerRequest())

			var modelErr *ports.ModelError
			require.ErrorAs(t, err, &modelErr)
			require.Equal(t, tt.kind, modelErr.Kind)
			require.Equal(t, tt.status, modelErr.Status)
			require.Equal(t, tt.retryAfter, modelErr.RetryAfter)
			require.NotEmpty(t, modelErr.Message)
			require.NotContains(t, err.Error(), "sk-or-v1-secret")
			if tt.sentinel != nil {
				require.ErrorIs(t, err, tt.sentinel)
			}
		})
	}
}

func TestChatReadsAnHTTPDateRetryAfter(t *testing.T) {
	srv := chatRespond(t, http.StatusTooManyRequests, map[string]string{
		"Date":        "Mon, 20 Nov 2026 10:00:00 GMT",
		"Retry-After": "Mon, 20 Nov 2026 10:00:45 GMT",
	}, chatFixture(t, "rate_limited.json"))
	client := ChatClient{BaseURL: srv.URL}

	_, err := client.Complete(context.Background(), chatAnswerRequest())

	var modelErr *ports.ModelError
	require.ErrorAs(t, err, &modelErr)
	require.Equal(t, 45*time.Second, modelErr.RetryAfter)
}

func TestChatRefusesAnOversizedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, maxChatResponseBytes+1))
	}))
	t.Cleanup(srv.Close)
	client := ChatClient{BaseURL: srv.URL}

	_, err := client.Complete(context.Background(), chatAnswerRequest())

	require.ErrorContains(t, err, "too large")
}

func TestChatRefusesAnIncompleteRequestWithoutCalling(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write(chatFixture(t, "answer.json"))
	}))
	t.Cleanup(srv.Close)
	client := ChatClient{BaseURL: srv.URL}

	_, err := client.Complete(context.Background(), ports.ModelRequest{Messages: []ports.ModelMessage{{Role: ports.ModelRoleUser, Content: "hi"}}})
	require.Error(t, err)
	_, err = client.Complete(context.Background(), ports.ModelRequest{Model: chatModel})
	require.Error(t, err)
	require.Zero(t, calls.Load())
}

func TestChatCostBillsCachedInputSeparately(t *testing.T) {
	price := Price{Input: 0.5, CachedInput: 0.05, Output: 1.5}

	got := chatCost(ports.ModelUsage{InputTokens: 1000, CachedInputTokens: 200, OutputTokens: 500}, price)

	require.InDelta(t, 0.00116, got, 1e-12)
	require.InDelta(t, 0.00002, chatCost(ports.ModelUsage{InputTokens: 100, CachedInputTokens: 200}, Price{Input: 1, CachedInput: 0.1}), 1e-12)
}

func TestChatTreatsAMissingKeyAsAnError(t *testing.T) {
	client := ChatClient{BaseURL: "http://127.0.0.1:1", KeyName: "key"}

	_, err := client.Complete(context.Background(), chatAnswerRequest())

	require.Error(t, err)
	var modelErr *ports.ModelError
	require.NotErrorAs(t, err, &modelErr)
}
