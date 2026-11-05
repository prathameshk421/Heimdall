package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// answer serves one fixed answer to every request. It is the smallest shape a test needs; the wire tests below drive
// the real server.
func answer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(hs.Close)
	return hs
}

// TestTheClientSpeaksTheProtocolTheServerImplements checks the JSON-RPC envelope and the bearer header the MCP edge
// expects (TECH_SPEC 14.1, internal/transport/mcp/server.go).
func TestTheClientSpeaksTheProtocolTheServerImplements(t *testing.T) {
	var path, auth, contentType, method string
	var body map[string]any
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, auth, method = r.URL.Path, r.Header.Get("Authorization"), r.Method
		contentType = r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"hello"}],"isError":false}}`)
	}))
	t.Cleanup(hs.Close)

	c, err := newClient(hs.URL+"/mcp", testToken)
	require.NoError(t, err)
	res, err := c.callTool(t.Context(), toolAck, map[string]any{})
	require.NoError(t, err)
	require.Equal(t, "hello", res.text())
	require.False(t, res.IsError)
	require.Equal(t, "/mcp", path)
	require.Equal(t, http.MethodPost, method)
	require.Equal(t, "Bearer "+testToken, auth)
	require.Equal(t, "application/json", contentType)
	require.Equal(t, "2.0", body["jsonrpc"])
	require.Equal(t, "tools/call", body["method"])
	params, ok := body["params"].(map[string]any)
	require.True(t, ok, "params must be an object: %v", body["params"])
	require.Equal(t, toolAck, params["name"])
	arguments, ok := params["arguments"].(map[string]any)
	require.True(t, ok, "arguments must be an object: %v", params["arguments"])
	require.Empty(t, arguments)
}

// TestConnectInitializesThenSendsTheInitializedNotification is the handshake every MCP client performs.
func TestConnectInitializesThenSendsTheInitializedNotification(t *testing.T) {
	var methods []string
	var ids []json.RawMessage
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg struct {
			Method string          `json:"method"`
			ID     json.RawMessage `json:"id"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &msg)
		methods = append(methods, msg.Method)
		ids = append(ids, msg.ID)
		w.Header().Set("Content-Type", "application/json")
		if msg.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18"}}`)
	}))
	t.Cleanup(hs.Close)

	c, err := newClient(hs.URL+"/mcp", testToken)
	require.NoError(t, err)
	require.NoError(t, c.connect(t.Context()))
	require.Equal(t, []string{"initialize", "notifications/initialized"}, methods)
	require.JSONEq(t, "1", string(ids[0]), "a request carries a numeric id")
	require.Empty(t, ids[1], "the initialized notification carries no id")
}

// TestAToolRefusalIsAResultAndNotAnError keeps the agent's budget intact: a refusal is an isError result.
func TestAToolRefusalIsAResultAndNotAnError(t *testing.T) {
	hs := answer(t, http.StatusOK, `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"The submission is not valid."}],"isError":true}}`)
	c, err := newClient(hs.URL, testToken)
	require.NoError(t, err)
	res, err := c.callTool(t.Context(), toolSubmitTests, map[string]any{})
	require.NoError(t, err)
	require.True(t, res.IsError)
	require.Equal(t, "The submission is not valid.", res.text())
}

// TestAnUnauthorizedAnswerIsAnErrorWithoutTheToken is the refusal path the token must survive.
func TestAnUnauthorizedAnswerIsAnErrorWithoutTheToken(t *testing.T) {
	hs := answer(t, http.StatusUnauthorized, "the token is unknown or revoked\n")
	c, err := newClient(hs.URL, testToken)
	require.NoError(t, err)
	_, err = c.call(t.Context(), "initialize", map[string]any{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "refused the agent token")
	require.NotContains(t, err.Error(), testToken)
}

// TestAJSONRPCErrorIsAnError is a protocol error, unlike a tool refusal.
func TestAJSONRPCErrorIsAnError(t *testing.T) {
	hs := answer(t, http.StatusOK, `{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"invalid params"}}`)
	c, err := newClient(hs.URL, testToken)
	require.NoError(t, err)
	_, err = c.call(t.Context(), "tools/call", map[string]any{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid params")
	require.Contains(t, err.Error(), "-32602")
}

// TestAnAnswerWithAnotherIDIsRefused guards against reading a reply to another request.
func TestAnAnswerWithAnotherIDIsRefused(t *testing.T) {
	hs := answer(t, http.StatusOK, `{"jsonrpc":"2.0","id":99,"result":{}}`)
	c, err := newClient(hs.URL, testToken)
	require.NoError(t, err)
	_, err = c.call(t.Context(), "ping", map[string]any{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "answer")
}

// TestAnAnswerThatIsNotJSONIsAnError covers a proxy or a wrong URL.
func TestAnAnswerThatIsNotJSONIsAnError(t *testing.T) {
	hs := answer(t, http.StatusOK, "<html>not an MCP server</html>")
	c, err := newClient(hs.URL, testToken)
	require.NoError(t, err)
	_, err = c.call(t.Context(), "ping", map[string]any{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "server answer")
}

// TestAnHTTPFailureIsAnError carries the status, never the body (which can hold untrusted text).
func TestAnHTTPFailureIsAnError(t *testing.T) {
	hs := answer(t, http.StatusInternalServerError, "postgres://heimdall:hunter2@db")
	c, err := newClient(hs.URL, testToken)
	require.NoError(t, err)
	_, err = c.call(t.Context(), "ping", map[string]any{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "500")
	require.NotContains(t, err.Error(), "hunter2")
}

// TestNewClientRefusesAURLThatIsNotHTTPAndABlankToken keeps a mistake in the command line loud.
func TestNewClientRefusesAURLThatIsNotHTTPAndABlankToken(t *testing.T) {
	for _, raw := range []string{"", "ftp://example.test/mcp", "127.0.0.1:8080/mcp", "http:///mcp"} {
		_, err := newClient(raw, testToken)
		require.Error(t, err, "URL %q must be refused", raw)
	}
	_, err := newClient("http://127.0.0.1:1/mcp", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "token")
}

// TestReadTokenReadsTheFileAndTrimsItsEndOfLine is the only place the token comes from.
func TestReadTokenReadsTheFileAndTrimsItsEndOfLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(path, []byte("  "+testToken+"\r\n"), 0o600))
	got, err := readToken(path)
	require.NoError(t, err)
	require.Equal(t, testToken, got)
}

// TestReadTokenRefusesWhatItCannotRead without ever echoing content.
func TestReadTokenRefusesWhatItCannotRead(t *testing.T) {
	_, err := readToken("")
	require.Error(t, err)
	require.Contains(t, err.Error(), "token-file")

	_, err = readToken(filepath.Join(t.TempDir(), "absent"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "read token file")

	empty := filepath.Join(t.TempDir(), "empty")
	require.NoError(t, os.WriteFile(empty, []byte("\n"), 0o600))
	_, err = readToken(empty)
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty")
	require.NotContains(t, err.Error(), testToken)
}

// TestToolResultTextJoinsEveryPart keeps a server that splits its text readable.
func TestToolResultTextJoinsEveryPart(t *testing.T) {
	res := toolResult{Content: []textContent{{Type: "text", Text: "one "}, {Type: "text", Text: "two"}}}
	require.Equal(t, "one two", res.text())
	require.False(t, res.IsError)
	require.Contains(t, res.text(), "two")
}
