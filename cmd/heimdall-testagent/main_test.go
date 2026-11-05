package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// runScenario runs one scenario the way main does: flags in, exit status and output out.
func runScenario(t *testing.T, url, tokenFile, name string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), []string{"-url", url, "-token-file", tokenFile, "-scenario", name}, noEnvironment, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// noEnvironment is the environment of a test that does not set the scenario variable.
func noEnvironment(string) string { return "" }

// TestEachSubmissionScenarioAgainstTheRealServer plays every canned submission against the real MCP edge. A refusal
// is an expected outcome and still exits 0; only a scenario that cannot talk to the server fails.
func TestEachSubmissionScenarioAgainstTheRealServer(t *testing.T) {
	cases := []struct {
		name       string
		wantVerb   string
		wantText   string
		wantSubmit bool
	}{
		{name: "valid", wantVerb: "recorded", wantText: "The submission is recorded.", wantSubmit: true},
		{name: "missing-ac", wantVerb: "refused", wantText: "acceptance_criteria[AC2] has no test"},
		{name: "import-error", wantVerb: "recorded", wantText: "The submission is recorded.", wantSubmit: true},
		{name: "outside-dir", wantVerb: "refused", wantText: "is outside the tests directory " + testsDir},
		{name: "schema-bad", wantVerb: "refused", wantText: `unknown field "ticket_key"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, tasks, hs := newWorld(t)
			code, stdout, stderr := runScenario(t, hs.URL+"/mcp", writeToken(t), c.name)
			require.Equal(t, exitOK, code, "stderr: %s", stderr)
			require.Contains(t, stdout, c.name+": "+c.wantVerb+":")
			require.Contains(t, stdout, c.wantText)
			if c.wantSubmit {
				require.Len(t, tasks.submits, 1)
			} else {
				require.Empty(t, tasks.submits, "a refused submission must not reach the application")
			}
		})
	}
}

// TestTheValidScenarioCoversEveryAcceptanceCriterion pins the one rule the "valid" name promises.
func TestTheValidScenarioCoversEveryAcceptanceCriterion(t *testing.T) {
	_, tasks, hs := newWorld(t)
	code, _, stderr := runScenario(t, hs.URL+"/mcp", writeToken(t), "valid")
	require.Equal(t, exitOK, code, "stderr: %s", stderr)
	require.Len(t, tasks.submits, 1)
	sub := tasks.submits[0]
	require.ElementsMatch(t, []string{"AC1", "AC2"}, criteriaKeys(sub.AcceptanceCriteria))
	for _, ac := range []string{"AC1", "AC2"} {
		require.NotEmpty(t, sub.AcceptanceCriteria[ac], "criterion %s has no test", ac)
	}
	require.Len(t, sub.Files, 1)
	require.True(t, strings.HasPrefix(sub.Files[0].Path, testsDir+"/"), "path %q is not under %s", sub.Files[0].Path, testsDir)
	require.NotEmpty(t, tasks.progress, "the valid run reports progress after the submission")
}

// TestTheMissingACScenarioLeavesExactlyOneCriterionOut names the criterion it drops, so the scenario cannot silently
// cover the whole task.
func TestTheMissingACScenarioLeavesExactlyOneCriterionOut(t *testing.T) {
	_, tasks, hs := newWorld(t)
	code, stdout, stderr := runScenario(t, hs.URL+"/mcp", writeToken(t), "missing-ac")
	require.Equal(t, exitOK, code, "stderr: %s", stderr)
	require.Contains(t, stdout, "acceptance_criteria[AC2]")
	require.NotContains(t, stdout, "acceptance_criteria[AC1]")
	require.Empty(t, tasks.submits)
}

// TestTheSilentScenarioNeverAcks is the ack-window failure path.
func TestTheSilentScenarioNeverAcks(t *testing.T) {
	_, tasks, hs := newWorld(t)
	code, stdout, stderr := runScenario(t, hs.URL+"/mcp", writeToken(t), "silent")
	require.Equal(t, exitOK, code, "stderr: %s", stderr)
	require.Zero(t, tasks.acks, "the silent scenario must never acknowledge")
	require.Empty(t, tasks.submits)
	require.Contains(t, stdout, "silent:")
	require.Contains(t, stdout, "no ack")
}

// TestTheSlowScenarioStopsWhenItsContextEnds is the deadline path: it acknowledges, reports progress, then waits, and
// a cancelled context ends it without a submission.
func TestTheSlowScenarioStopsWhenItsContextEnds(t *testing.T) {
	_, tasks, hs := newWorld(t)
	tasks.ackReady = make(chan struct{}, 1)
	tokenFile := writeToken(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	stdout := &signalWriter{wrote: make(chan struct{}, 1)}
	var stderr bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- run(ctx, []string{"-url", hs.URL + "/mcp", "-token-file", tokenFile, "-scenario", "slow"}, noEnvironment, stdout, &stderr)
	}()

	wait, stopWaiting := context.WithTimeout(t.Context(), 5*time.Second)
	defer stopWaiting()
	waitFor(t, tasks.ackReady, "the slow scenario never acknowledged its task")
	waitFor(t, stdout.wrote, "the slow scenario never reached its wait")
	cancel()
	select {
	case code := <-done:
		require.Equal(t, exitOK, code, "stderr: %s", stderr.String())
	case <-wait.Done():
		t.Fatal("the slow scenario did not stop when its context was cancelled")
	}
	require.Empty(t, tasks.submits, "the slow scenario must never submit")
	require.Contains(t, stdout.String(), "slow: waiting:")
}

// signalWriter reports its first write, so a test can wait for a step without sleeping.
type signalWriter struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	wrote chan struct{}
}

func (w *signalWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.buf.Write(p)
	select {
	case w.wrote <- struct{}{}:
	default:
	}
	return n, err
}

func (w *signalWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// waitFor waits for one readiness signal or fails the test when its own deadline passes first.
func waitFor(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	select {
	case <-ch:
	case <-ctx.Done():
		t.Fatal(message)
	}
}

// TestTheTokenIsReadFromAFileAndNeverInTheOutput uses a canary as the token: it must reach the server as a hash and
// must not appear in anything the agent prints or returns.
func TestTheTokenIsReadFromAFileAndNeverInTheOutput(t *testing.T) {
	tokens, _, hs := newWorld(t)
	code, stdout, stderr := runScenario(t, hs.URL+"/mcp", writeToken(t), "valid")
	require.Equal(t, exitOK, code, "stderr: %s", stderr)
	require.NotContains(t, stdout, testToken)
	require.NotContains(t, stderr, testToken)
	require.NotEmpty(t, tokens.seen, "the server must have seen the token's hash")
	for _, seen := range tokens.seen {
		require.Equal(t, hashOf(testToken), seen, "the server must see the hash, never the token")
	}
}

// TestAMissingTokenFileFailsBeforeAnyRequest keeps the token read ahead of the first call.
func TestAMissingTokenFileFailsBeforeAnyRequest(t *testing.T) {
	tokens, _, hs := newWorld(t)
	missing := filepath.Join(t.TempDir(), "absent")
	code, stdout, stderr := runScenario(t, hs.URL+"/mcp", missing, "valid")
	require.Equal(t, exitFailure, code)
	require.Contains(t, stderr, "read token file")
	require.Contains(t, stderr, missing)
	require.Empty(t, stdout)
	require.Empty(t, tokens.seen, "no request may reach the server without a token")
}

// TestAnUnreachableServerFails is the "could not talk to the server" exit.
func TestAnUnreachableServerFails(t *testing.T) {
	code, stdout, stderr := runScenario(t, "http://127.0.0.1:1/mcp", writeToken(t), "silent")
	require.Equal(t, exitFailure, code)
	require.Contains(t, stderr, "silent")
	require.NotContains(t, stderr, testToken)
	require.Empty(t, stdout)
}

// TestARefusedSubmissionIsOneLineAndExitZero is what a reader of a scripted run relies on.
func TestARefusedSubmissionIsOneLineAndExitZero(t *testing.T) {
	_, tasks, hs := newWorld(t)
	code, stdout, stderr := runScenario(t, hs.URL+"/mcp", writeToken(t), "outside-dir")
	require.Equal(t, exitOK, code, "stderr: %s", stderr)
	require.Equal(t, 1, strings.Count(stdout, "\n"), "a refused submission prints one line, got %q", stdout)
	require.Contains(t, stdout, "outside-dir: refused:")
	require.Empty(t, tasks.submits)
}

// TestTheFlagWinsOverTheEnvironment pins the order the scenario name is chosen in.
func TestTheFlagWinsOverTheEnvironment(t *testing.T) {
	_, tasks, hs := newWorld(t)
	var stdout, stderr bytes.Buffer
	getenv := func(name string) string {
		require.Equal(t, "HEIMDALL_TESTAGENT_SCENARIO", name)
		return "valid"
	}
	args := []string{"-url", hs.URL + "/mcp", "-token-file", writeToken(t), "-scenario", "silent"}
	code := run(t.Context(), args, getenv, &stdout, &stderr)
	require.Equal(t, exitOK, code, "stderr: %s", stderr.String())
	require.Zero(t, tasks.acks, "the flag's silent scenario must win over the environment")
	require.Contains(t, stdout.String(), "silent:")
}

// TestTheEnvironmentNamesTheScenarioWhenTheFlagIsEmpty covers the second way a scenario is chosen.
func TestTheEnvironmentNamesTheScenarioWhenTheFlagIsEmpty(t *testing.T) {
	_, tasks, hs := newWorld(t)
	var stdout, stderr bytes.Buffer
	var asked []string
	getenv := func(name string) string {
		asked = append(asked, name)
		return "valid"
	}
	args := []string{"-url", hs.URL + "/mcp", "-token-file", writeToken(t)}
	code := run(t.Context(), args, getenv, &stdout, &stderr)
	require.Equal(t, exitOK, code, "stderr: %s", stderr.String())
	require.Equal(t, []string{"HEIMDALL_TESTAGENT_SCENARIO"}, asked)
	require.Len(t, tasks.submits, 1)
}

// TestAnUnknownScenarioIsAUsageError lists the real names, so a typo is fixed in one read.
func TestAnUnknownScenarioIsAUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), []string{"-scenario", "nope"}, noEnvironment, &stdout, &stderr)
	require.Equal(t, exitUsage, code)
	require.Contains(t, stderr.String(), `"nope"`)
	require.Contains(t, stderr.String(), "missing-ac")
	require.Equal(t, 0, stdout.Len(), "a usage error prints nothing on stdout")
}

// TestAUnexpectedArgumentIsAUsageError keeps the command line closed.
func TestAUnexpectedArgumentIsAUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), []string{"-scenario", "valid", "extra"}, noEnvironment, &stdout, &stderr)
	require.Equal(t, exitUsage, code)
	require.Contains(t, stderr.String(), "unexpected argument")
	require.Equal(t, 0, stdout.Len(), "a usage error prints nothing on stdout")
}
