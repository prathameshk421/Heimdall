package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// resendFixtureBody is a small saved request body with a system message, a user message and two tools.
const resendFixtureBody = `{"model":"m","messages":[
 {"role":"system","content":"you review a change"},
 {"role":"user","content":"review this"},
 {"role":"assistant","content":"reading"},
 {"role":"user","content":"go on"}],
 "tools":[{"type":"function","function":{"name":"run_test"}},{"type":"function","function":{"name":"finish_review"}}],
 "tool_choice":"required","max_tokens":4096}`

func parseResendBody(t *testing.T) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(resendFixtureBody), &body))
	return body
}

func TestResendTransformRefusesANegativeKeepLast(t *testing.T) {
	_, err := transformResendBody(parseResendBody(t), resendOptions{keepLast: -1})
	require.ErrorContains(t, err, "-keep-last")
}

func TestResendTransformRefusesABodyThatIsNotAnObject(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.json")
	require.NoError(t, os.WriteFile(path, []byte(`[1,2]`), 0o600))
	_, err := resendBody(path, "m", resendOptions{})
	require.ErrorContains(t, err, "not a JSON object")
}

// resendOne sends body once to srv through resendOnce and returns what it printed.
func resendOne(t *testing.T, srv *httptest.Server, body string, keyPath string) (string, int) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "body.json")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	var out bytes.Buffer
	code, err := resendOnce(context.Background(), baseURLOf(srv), "m", keyPath, path, resendOptions{}, &out)
	require.NoError(t, err)
	return out.String(), code
}

func baseURLOf(srv *httptest.Server) string { return srv.URL }

func writeTestKey(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "model_key")
	require.NoError(t, os.WriteFile(path, []byte(testKey+"\n"), 0o600))
	return path
}

func TestResendSendsTheBodyOnceEvenOnA500(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, code := resendOne(t, srv, resendFixtureBody, writeTestKey(t))
	require.Equal(t, exitAgrees, code)
	require.Equal(t, 1, calls, "resend never retries")
}

func TestResendRefusesAMissingKeyFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "body.json")
	require.NoError(t, os.WriteFile(path, []byte(resendFixtureBody), 0o600))
	var out bytes.Buffer
	_, err := resendOnce(context.Background(), srv.URL, "m", filepath.Join(dir, "absent"), path, resendOptions{}, &out)
	require.Error(t, err)
}

func TestResendRefusesABodyThatIsNotJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.json")
	require.NoError(t, os.WriteFile(path, []byte("not json"), 0o600))
	var out bytes.Buffer
	_, err := resendOnce(context.Background(), "http://127.0.0.1:1", "m", writeTestKey(t), path, resendOptions{}, &out)
	require.Error(t, err)
	require.NotContains(t, err.Error(), testKey)
}

func TestParseResendFlagsNeedsTheBodyAndTheKey(t *testing.T) {
	_, err := parseResendFlags([]string{"-model", "m"}, io.Discard)
	require.ErrorContains(t, err, "-body")
	_, err = parseResendFlags([]string{"-model", "m", "-body", "/tmp/b.json"}, io.Discard)
	require.ErrorContains(t, err, "-key-file")
	_, err = parseResendFlags([]string{"-key-file", "/tmp/k", "-body", "/tmp/b.json"}, io.Discard)
	require.ErrorContains(t, err, "-model")
}
