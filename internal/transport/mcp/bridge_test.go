package mcp

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

func quietLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// outputLines splits the bridge's stdout into lines, dropping the final empty piece after the last newline.
func outputLines(out string) []string {
	if out == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(out, "\n"), "\n")
}

// bridgeRun runs the bridge over input against url with the test token and returns its stdout and its error.
func bridgeRun(t *testing.T, url, token, input string, logs *bytes.Buffer) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := Bridge(t.Context(), BridgeConfig{
		URL:    url,
		Token:  token,
		In:     strings.NewReader(input),
		Out:    &out,
		Logger: quietLogger(logs),
		HTTP:   http.DefaultClient,
	})
	return out.String(), err
}

func TestBridgeRelaysEachRequestAndWritesEachAnswer(t *testing.T) {
	r := newRig(t, workflow.RoleReviewer)
	srv := httptest.NewServer(r.handler)
	t.Cleanup(srv.Close)
	input := `{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n"
	var logs bytes.Buffer
	out, err := bridgeRun(t, srv.URL+"/mcp", testToken, input, &logs)
	require.NoError(t, err)
	lines := outputLines(out)
	require.Len(t, lines, 2)
	var first, second rpcReply
	require.NoError(t, decodeInto([]byte(lines[0]), &first))
	require.NoError(t, decodeInto([]byte(lines[1]), &second))
	require.JSONEq(t, "1", string(first.ID))
	require.JSONEq(t, "{}", string(first.Result))
	require.JSONEq(t, "2", string(second.ID))
	require.Contains(t, string(second.Result), "get_task")
}

func TestBridgeSendsTheTokenAsAHeaderOnly(t *testing.T) {
	r := newRig(t, workflow.RoleReviewer)
	var gotAuth string
	var gotBody []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotAuth = req.Header.Get("Authorization")
		b, _ := io.ReadAll(req.Body)
		gotBody = append(gotBody, string(b))
		req.Body = io.NopCloser(bytes.NewReader(b))
		r.handler.ServeHTTP(w, req)
	}))
	t.Cleanup(srv.Close)
	var logs bytes.Buffer
	_, err := bridgeRun(t, srv.URL+"/mcp", testToken, `{"jsonrpc":"2.0","id":1,"method":"ping"}`+"\n", &logs)
	require.NoError(t, err)
	require.Equal(t, "Bearer "+testToken, gotAuth)
	require.Len(t, gotBody, 1)
	require.NotContains(t, gotBody[0], testToken)
}

func TestANotificationIsPostedAndProducesNoOutput(t *testing.T) {
	r := newRig(t, workflow.RoleReviewer)
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		hits++
		r.handler.ServeHTTP(w, req)
	}))
	t.Cleanup(srv.Close)
	var logs bytes.Buffer
	out, err := bridgeRun(t, srv.URL+"/mcp", testToken, `{"jsonrpc":"2.0","method":"notifications/initialized"}`+"\n", &logs)
	require.NoError(t, err)
	require.Equal(t, 1, hits, "the notification must reach the server")
	require.Empty(t, out)
}

func TestANotificationThatFailsIsLoggedAndWritesNothing(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL + "/mcp"
	srv.Close()
	var logs bytes.Buffer
	out, err := bridgeRun(t, url, testToken, `{"jsonrpc":"2.0","method":"notifications/initialized"}`+"\n", &logs)
	require.NoError(t, err)
	require.Empty(t, out, "a notification has no id to answer, so a failure writes nothing")
	require.NotEmpty(t, logs.String(), "the failure is logged")
}

func TestAnUnreachableServerAnswersEachRequestWithAnErrorForItsID(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL + "/mcp"
	srv.Close()
	input := `{"jsonrpc":"2.0","id":3,"method":"ping"}` + "\n" + `{"jsonrpc":"2.0","id":4,"method":"ping"}` + "\n"
	var logs bytes.Buffer
	out, err := bridgeRun(t, url, testToken, input, &logs)
	require.NoError(t, err, "the bridge keeps running after a failed request")
	lines := outputLines(out)
	require.Len(t, lines, 2)
	var first, second rpcReply
	require.NoError(t, decodeInto([]byte(lines[0]), &first))
	require.NoError(t, decodeInto([]byte(lines[1]), &second))
	require.NotNil(t, first.Error)
	require.Equal(t, codeUnreachable, first.Error.Code)
	require.JSONEq(t, "3", string(first.ID))
	require.JSONEq(t, "4", string(second.ID))
}

func TestAServerThatRefusesTheTokenGivesAnUnauthorizedErrorWithoutTheToken(t *testing.T) {
	r := newRig(t, workflow.RoleReviewer)
	srv := httptest.NewServer(r.handler)
	t.Cleanup(srv.Close)
	var logs bytes.Buffer
	out, err := bridgeRun(t, srv.URL+"/mcp", "wrong-"+testToken, `{"jsonrpc":"2.0","id":5,"method":"ping"}`+"\n", &logs)
	require.NoError(t, err)
	var rep rpcReply
	require.NoError(t, decodeInto([]byte(strings.TrimSuffix(out, "\n")), &rep))
	require.NotNil(t, rep.Error)
	require.Equal(t, codeUnauthorized, rep.Error.Code)
	require.JSONEq(t, "5", string(rep.ID))
	require.NotContains(t, out, testToken)
	require.NotContains(t, logs.String(), testToken)
}

func TestAServerErrorStatusGivesAnUnreachableErrorForTheRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	var logs bytes.Buffer
	out, err := bridgeRun(t, srv.URL+"/mcp", testToken, `{"jsonrpc":"2.0","id":6,"method":"ping"}`+"\n", &logs)
	require.NoError(t, err)
	var rep rpcReply
	require.NoError(t, decodeInto([]byte(strings.TrimSuffix(out, "\n")), &rep))
	require.NotNil(t, rep.Error)
	require.Equal(t, codeUnreachable, rep.Error.Code)
}

func TestAnAnswerThatIsNotJSONGivesAnUnreachableErrorForTheRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "<html>proxy error</html>")
	}))
	t.Cleanup(srv.Close)
	var logs bytes.Buffer
	out, err := bridgeRun(t, srv.URL+"/mcp", testToken, `{"jsonrpc":"2.0","id":7,"method":"ping"}`+"\n", &logs)
	require.NoError(t, err)
	var rep rpcReply
	require.NoError(t, decodeInto([]byte(strings.TrimSuffix(out, "\n")), &rep))
	require.NotNil(t, rep.Error)
	require.Equal(t, codeUnreachable, rep.Error.Code)
	require.JSONEq(t, "7", string(rep.ID))
}

func TestAMalformedLineIsAParseErrorAndTheBridgeGoesOn(t *testing.T) {
	r := newRig(t, workflow.RoleReviewer)
	srv := httptest.NewServer(r.handler)
	t.Cleanup(srv.Close)
	input := `{oops` + "\n" + `{"jsonrpc":"2.0","id":8,"method":"ping"}` + "\n"
	var logs bytes.Buffer
	out, err := bridgeRun(t, srv.URL+"/mcp", testToken, input, &logs)
	require.NoError(t, err)
	lines := outputLines(out)
	require.Len(t, lines, 2)
	var parse, pong rpcReply
	require.NoError(t, decodeInto([]byte(lines[0]), &parse))
	require.NoError(t, decodeInto([]byte(lines[1]), &pong))
	require.NotNil(t, parse.Error)
	require.Equal(t, codeParseError, parse.Error.Code)
	require.JSONEq(t, "null", string(parse.ID))
	require.JSONEq(t, "8", string(pong.ID))
}

func TestABatchLineIsRefusedLocallyWithoutCallingTheServer(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	var logs bytes.Buffer
	out, err := bridgeRun(t, srv.URL+"/mcp", testToken, `[{"jsonrpc":"2.0","id":9,"method":"ping"}]`+"\n", &logs)
	require.NoError(t, err)
	require.Zero(t, hits, "a batch must not reach the server")
	var rep rpcReply
	require.NoError(t, decodeInto([]byte(strings.TrimSuffix(out, "\n")), &rep))
	require.NotNil(t, rep.Error)
	require.Equal(t, codeInvalidRequest, rep.Error.Code)
}

func TestALineOverTheBodyLimitIsRefusedAndTheNextLineStillRuns(t *testing.T) {
	r := newRig(t, workflow.RoleReviewer)
	srv := httptest.NewServer(r.handler)
	t.Cleanup(srv.Close)
	huge := `{"jsonrpc":"2.0","id":10,"method":"ping","params":{"pad":"` + strings.Repeat("a", 8<<20) + `"}}`
	input := huge + "\n" + `{"jsonrpc":"2.0","id":11,"method":"ping"}` + "\n"
	var logs bytes.Buffer
	out, err := bridgeRun(t, srv.URL+"/mcp", testToken, input, &logs)
	require.NoError(t, err)
	lines := outputLines(out)
	require.Len(t, lines, 2)
	var big, pong rpcReply
	require.NoError(t, decodeInto([]byte(lines[0]), &big))
	require.NoError(t, decodeInto([]byte(lines[1]), &pong))
	require.NotNil(t, big.Error)
	require.JSONEq(t, "11", string(pong.ID))
	require.Nil(t, pong.Error)
}

func TestCancellingTheContextEndsTheBridgeEvenWhileInputIsOpen(t *testing.T) {
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	ctx, cancel := context.WithCancel(t.Context())
	var logs bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- Bridge(ctx, BridgeConfig{URL: "http://127.0.0.1:1/mcp", Token: testToken, In: pr,
			Out: io.Discard, Logger: quietLogger(&logs), HTTP: http.DefaultClient})
	}()
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(10 * time.Second):
		t.Fatal("the bridge did not return after its context was cancelled")
	}
}

func TestTheBridgeLogsGoToItsLoggerAndNeverToStdout(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL + "/mcp"
	srv.Close()
	var logs bytes.Buffer
	out, err := bridgeRun(t, url, testToken, `{"jsonrpc":"2.0","id":12,"method":"ping"}`+"\n", &logs)
	require.NoError(t, err)
	for _, line := range outputLines(out) {
		var rep rpcReply
		require.NoError(t, decodeInto([]byte(line), &rep), "stdout must hold only JSON-RPC lines: %q", line)
	}
	require.NotEmpty(t, logs.String(), "the failure must be logged")
	require.NotContains(t, logs.String(), testToken)
}
