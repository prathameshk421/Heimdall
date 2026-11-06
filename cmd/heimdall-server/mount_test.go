package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport"
)

// get issues a GET request with the test context.
func get(t *testing.T, client *http.Client, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	return resp
}

// mountHTTP serves /healthz with no slice selected, and 404 on /mcp.
func TestMountHTTPHealthzOnlyWithoutASlice(t *testing.T) {
	router, err := transport.NewRouter(slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	ts := httptest.NewServer(mountHTTP(router, nil))
	defer ts.Close()

	resp := get(t, ts.Client(), ts.URL+"/healthz")
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.JSONEq(t, `{"status":"ok"}`, string(body))

	for _, path := range []string{"/mcp", "/healthz/", "/anything"} {
		r := get(t, ts.Client(), ts.URL+path)
		_ = r.Body.Close()
		require.Equal(t, http.StatusNotFound, r.StatusCode, "%s must 404 with no slice", path)
	}
}

// mountHTTP serves /mcp from the app when a slice selected one, and keeps /healthz.
func TestMountHTTPServesHealthzAndMCP(t *testing.T) {
	router, err := transport.NewRouter(slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	mcpMux := http.NewServeMux()
	mcpMux.HandleFunc("POST /mcp", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	})
	a := &app{mcpHandler: mcpMux}
	ts := httptest.NewServer(mountHTTP(router, a))
	defer ts.Close()

	r := get(t, ts.Client(), ts.URL+"/healthz")
	_ = r.Body.Close()
	require.Equal(t, http.StatusOK, r.StatusCode)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, ts.URL+"/mcp", nil)
	require.NoError(t, err)
	r, err = ts.Client().Do(req)
	require.NoError(t, err)
	_ = r.Body.Close()
	require.Equal(t, http.StatusOK, r.StatusCode)

	// GET on /mcp is refused by the MCP mux, not routed elsewhere.
	r = get(t, ts.Client(), ts.URL+"/mcp")
	_ = r.Body.Close()
	require.Equal(t, http.StatusMethodNotAllowed, r.StatusCode)
}

// mountHTTP with a slice selected but no MCP handler mounted serves /healthz only.
func TestMountHTTPWithNilMCPHandler(t *testing.T) {
	router, err := transport.NewRouter(slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	a := &app{}
	ts := httptest.NewServer(mountHTTP(router, a))
	defer ts.Close()
	r := get(t, ts.Client(), ts.URL+"/healthz")
	_ = r.Body.Close()
	require.Equal(t, http.StatusOK, r.StatusCode)
	r = get(t, ts.Client(), ts.URL+"/mcp")
	_ = r.Body.Close()
	require.Equal(t, http.StatusNotFound, r.StatusCode)
}
