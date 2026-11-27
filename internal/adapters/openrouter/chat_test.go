package openrouter

import (
	"context"
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
