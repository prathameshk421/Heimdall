package main

import (
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
