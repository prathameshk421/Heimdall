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
	"strings"
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

func messagesOf(t *testing.T, body map[string]any) []any {
	t.Helper()
	msgs, ok := body["messages"].([]any)
	require.True(t, ok, "the body keeps its messages")
	return msgs
}

func rolesOf(t *testing.T, body map[string]any) []string {
	t.Helper()
	var out []string
	for _, m := range messagesOf(t, body) {
		msg, ok := m.(map[string]any)
		require.True(t, ok)
		out = append(out, msg["role"].(string))
	}
	return out
}

func TestResendTransformDropsTheTools(t *testing.T) {
	body := parseResendBody(t)
	got, err := transformResendBody(body, resendOptions{dropTools: true})
	require.NoError(t, err)
	require.NotContains(t, got, "tools", "the tools are gone")
	require.NotContains(t, got, "tool_choice", "the tool choice goes with the tools")
	require.NotContains(t, got, "run_test")
	require.Equal(t, "m", got["model"], "the rest of the body stays")
	require.Len(t, messagesOf(t, got), 4)
}

func TestResendTransformDropsTheSystemMessage(t *testing.T) {
	body := parseResendBody(t)
	got, err := transformResendBody(body, resendOptions{dropSystem: true})
	require.NoError(t, err)
	require.Equal(t, []string{"user", "assistant", "user"}, rolesOf(t, got))
}

func TestResendTransformKeepsTheLastNMessages(t *testing.T) {
	body := parseResendBody(t)
	got, err := transformResendBody(body, resendOptions{keepLast: 2})
	require.NoError(t, err)
	msgs := messagesOf(t, got)
	require.Len(t, msgs, 2)
	require.Equal(t, "assistant", msgs[0].(map[string]any)["role"], "the last two, in order")
	require.Equal(t, "user", msgs[1].(map[string]any)["role"])
}

func TestResendTransformCombinesItsParts(t *testing.T) {
	body := parseResendBody(t)
	got, err := transformResendBody(body, resendOptions{dropTools: true, dropSystem: true, keepLast: 3})
	require.NoError(t, err)
	require.Equal(t, []string{"user", "assistant", "user"}, rolesOf(t, got))
	require.NotContains(t, got, "tools")
}

func TestResendTransformKeepsEveryMessageWhenKeepLastIsPastTheList(t *testing.T) {
	body := parseResendBody(t)
	got, err := transformResendBody(body, resendOptions{keepLast: 99})
	require.NoError(t, err)
	require.Len(t, messagesOf(t, got), 4)
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

func TestResendPrintsTheStatusContentTypeAndABoundedBody(t *testing.T) {
	answer := strings.Repeat("a", 5000)
	var gotBody []byte
	var gotAuth string
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		gotAuth = r.Header.Get("Authorization")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, answer)
	}))
	defer srv.Close()

	out, code := resendOne(t, srv, resendFixtureBody, writeTestKey(t))

	require.Equal(t, exitAgrees, code)
	require.Equal(t, 1, calls, "one request per call")
	require.Contains(t, out, "500")
	require.Contains(t, out, "application/json")
	require.Contains(t, out, strings.Repeat("a", resendBodyPrefix), "the first bytes of the answer")
	require.NotContains(t, out, strings.Repeat("a", resendBodyPrefix+1), "the answer is cut")
	require.NotContains(t, out, testKey, "the key never appears in the output")
	require.Equal(t, "Bearer "+testKey, gotAuth, "the key goes in the header, read from the file")

	var sent map[string]any
	require.NoError(t, json.Unmarshal(gotBody, &sent))
	require.Equal(t, "m", sent["model"])
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

func TestResendDoesNotFollowARedirect(t *testing.T) {
	// A redirect would be a second request, and it would carry the key to a host the operator did not name.
	var landed int
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		landed++
	}))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()

	out, code := resendOne(t, srv, resendFixtureBody, writeTestKey(t))
	require.Equal(t, exitAgrees, code)
	require.Contains(t, out, "307")
	require.Zero(t, landed, "the redirect is not followed")
}

func TestResendDropsTheToolsInTheSentBody(t *testing.T) {
	var sent map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &sent)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "body.json")
	require.NoError(t, os.WriteFile(path, []byte(resendFixtureBody), 0o600))
	var out bytes.Buffer
	code, err := resendOnce(context.Background(), srv.URL, "m", writeTestKey(t), path, resendOptions{dropTools: true, dropSystem: true, keepLast: 2}, &out)
	require.NoError(t, err)
	require.Equal(t, exitAgrees, code)
	require.NotContains(t, sent, "tools")
	require.Len(t, sent["messages"].([]any), 2)
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

func TestParseResendFlagsReadsTheParts(t *testing.T) {
	o, err := parseResendFlags([]string{"-base-url", "https://example.test/v1", "-model", "m",
		"-key-file", "/tmp/k", "-body", "/tmp/b.json", "-drop-tools", "-drop-system", "-keep-last", "5"},
		io.Discard)
	require.NoError(t, err)
	require.Equal(t, "https://example.test/v1", o.baseURL)
	require.Equal(t, "m", o.model)
	require.Equal(t, "/tmp/k", o.keyFile)
	require.Equal(t, "/tmp/b.json", o.body)
	require.True(t, o.opts.dropTools)
	require.True(t, o.opts.dropSystem)
	require.Equal(t, 5, o.opts.keepLast)
}

func TestParseResendFlagsNeedsTheBodyAndTheKey(t *testing.T) {
	_, err := parseResendFlags([]string{"-model", "m"}, io.Discard)
	require.ErrorContains(t, err, "-body")
	_, err = parseResendFlags([]string{"-model", "m", "-body", "/tmp/b.json"}, io.Discard)
	require.ErrorContains(t, err, "-key-file")
	_, err = parseResendFlags([]string{"-key-file", "/tmp/k", "-body", "/tmp/b.json"}, io.Discard)
	require.ErrorContains(t, err, "-model")
}

func TestRunCLIDispatchesResend(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runCLI(context.Background(), []string{"resend", "-model", "m"}, &stdout, &stderr, execRunner{})
	require.Equal(t, exitUsage, code)
	require.Contains(t, stderr.String(), "-body", "the usage error names the missing flag")
	require.NotContains(t, stdout.String(), testKey)
}
