package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/uidsandbox"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/action"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
)

const (
	smokeLogKey   = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
	smokeModelKey = "sk-" + "smoke-model-key-value"
	triggerRun    = int64(5001)
	proofRun      = int64(5002)
	gateRun       = int64(5003)
)

func noWait(ctx context.Context, _ time.Duration) error { return ctx.Err() }

// smokeDeps wires the action steps to the fakes. A sandbox is never set up unless the test passes one.
func smokeDeps(env map[string]string, jobs sandboxJobs) actionDeps {
	return actionDeps{getenv: func(k string) string { return env[k] }, http: &http.Client{Timeout: 10 * time.Second},
		wait: noWait,
		sandbox: func(context.Context, uidsandbox.Config) (sandboxJobs, func(context.Context) error, error) {
			if jobs == nil {
				return nil, nil, &uidsandbox.CannotIsolateError{Reason: "test runner"}
			}
			return jobs, func(context.Context) error { return nil }, nil
		},
		newKey: func() ([]byte, error) { return bytes.Repeat([]byte{7}, 32), nil }}
}

func writeEvent(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	p := filepath.Join(t.TempDir(), "event.json")
	require.NoError(t, os.WriteFile(p, raw, 0o600))
	return p
}

func smokePREvent(fork bool) map[string]any {
	full := fakeRepo
	if fork {
		full = "mallory/pay"
	}
	return map[string]any{"number": 7, "pull_request": map[string]any{"number": 7, "user": map[string]any{"login": "ana"},
		"head": map[string]any{"sha": string(testHead), "repo": map[string]any{"full_name": full}}}}
}

func smokeRunEvent(run int64, path string) map[string]any {
	return map[string]any{"workflow_run": map[string]any{"id": run, "path": path, "event": "pull_request",
		"head_sha": string(testHead), "conclusion": "success", "pull_requests": []any{}}}
}

func gateEnv(gh *fakeGitHub, eventName, eventPath string) map[string]string {
	return map[string]string{"GITHUB_REPOSITORY": fakeRepo, "GITHUB_API_URL": gh.srv.URL, "GITHUB_TOKEN": fakeToken,
		"GITHUB_EVENT_NAME": eventName, "GITHUB_EVENT_PATH": eventPath, "GITHUB_RUN_ID": "5003",
		"HEIMDALL_LOG_KEY": smokeLogKey}
}

func runStep(t *testing.T, deps actionDeps, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := runAction(args, strings.NewReader(""), &out, &errOut, deps)
	return code, out.String(), errOut.String()
}

// triggerRecordFor runs the trigger step on a pull_request event and returns the record it wrote.
func triggerRecordFor(t *testing.T) []byte {
	t.Helper()
	out := filepath.Join(t.TempDir(), triggerFile)
	env := map[string]string{"GITHUB_EVENT_NAME": "pull_request", "GITHUB_EVENT_PATH": writeEvent(t, smokePREvent(false))}
	code, _, stderr := runStep(t, smokeDeps(env, nil), "trigger", "-out", out)
	require.Equal(t, exitActionOK, code, stderr)
	raw, err := os.ReadFile(out)
	require.NoError(t, err)
	return raw
}

func TestActionTriggerWritesTheRecordAndRefusesAMergeGroup(t *testing.T) {
	raw := triggerRecordFor(t)
	rec, err := parseTriggerRecord(raw)
	require.NoError(t, err)
	require.Equal(t, 7, rec.Number)
	require.Equal(t, testHead, rec.Head)

	env := map[string]string{"GITHUB_EVENT_NAME": "merge_group", "GITHUB_EVENT_PATH": writeEvent(t, map[string]any{})}
	code, _, stderr := runStep(t, smokeDeps(env, nil), "trigger", "-out", filepath.Join(t.TempDir(), "x.json"))
	require.Equal(t, exitActionFail, code)
	require.Contains(t, stderr, "merge_group")
}

// The gate on a finished trigger run reads the record, writes the state ref, posts heimdall/fast and one summary. No
// heimdall/proof is posted before a proof result exists. A second run on the same head posts no second check and no
// second comment.
func TestActionGateServesATriggerRunEndToEnd(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.addArtifact(triggerRun, triggerArtifact, map[string][]byte{triggerFile: triggerRecordFor(t)})
	env := gateEnv(gh, "workflow_run", writeEvent(t, smokeRunEvent(triggerRun, triggerWorkflowPath)))

	code, stdout, stderr := runStep(t, smokeDeps(env, nil), "gate")
	require.Equal(t, exitActionOK, code, stderr)
	require.Contains(t, stdout, "#7 at "+string(testHead))
	require.NotEmpty(t, gh.refs["refs/heimdall/state"], "the state ref holds the log")
	require.Len(t, gh.checks, 1, "heimdall/fast only: no proof result yet")
	require.Equal(t, "heimdall/fast", gh.checks[0]["name"])
	require.Len(t, gh.comments, 1)

	code, _, stderr = runStep(t, smokeDeps(env, nil), "gate")
	require.Equal(t, exitActionOK, code, stderr)
	require.Len(t, gh.checks, 1, "a repeat posts no second check run")
	require.Len(t, gh.comments, 1, "a repeat updates the one summary")
}

// Without the log key the gate stops before any GitHub call, so nothing can be posted.
func TestActionGateFailsClosedWithoutTheLogKey(t *testing.T) {
	gh := newFakeGitHub(t)
	env := gateEnv(gh, "workflow_run", writeEvent(t, smokeRunEvent(triggerRun, triggerWorkflowPath)))
	delete(env, "HEIMDALL_LOG_KEY")
	code, _, stderr := runStep(t, smokeDeps(env, nil), "gate")
	require.Equal(t, exitActionFail, code)
	require.Contains(t, stderr, "HEIMDALL_LOG_KEY")
	require.Zero(t, gh.count(""), "no GitHub call")
}

// A proof artifact that breaks the format is recorded as unknown: no result, never as a pass.
func TestActionGateRecordsABadProofArtifactAsUnknown(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.addArtifact(proofRun, triggerArtifact, map[string][]byte{triggerFile: triggerRecordFor(t)})
	gh.addArtifact(proofRun, proofArtifactName, map[string][]byte{proofFile: []byte(`{"version":1,"kind":"proven"}`)})
	env := gateEnv(gh, "workflow_run", writeEvent(t, smokeRunEvent(proofRun, proofWorkflowPath)))
	code, stdout, stderr := runStep(t, smokeDeps(env, nil), "gate")
	require.Equal(t, exitActionOK, code, stderr)
	require.Contains(t, stdout, "recorded as unknown (no_result)")
	require.Contains(t, stdout, "artifact refused")
}

// fakeModel is an OpenAI-compatible endpoint that ends the review at once. It counts its calls and checks the key.
func fakeModel(t *testing.T) (*httptest.Server, *atomic.Int32) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+smokeModelKey {
			writeFakeJSON(w, http.StatusUnauthorized, map[string]string{"error": "bad key"})
			return
		}
		writeFakeJSON(w, http.StatusOK, map[string]any{"id": "c1", "model": "test/flash",
			"choices": []any{map[string]any{"index": 0, "finish_reason": "tool_calls", "message": map[string]any{
				"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": "call_1", "type": "function",
					"function": map[string]any{"name": "finish_review", "arguments": `{"summary":"checked"}`}}}}}},
			"usage": map[string]any{"prompt_tokens": 1000, "completion_tokens": 20}})
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func proofEnv(t *testing.T, gh *fakeGitHub, model string, fork bool) map[string]string {
	env := map[string]string{"GITHUB_REPOSITORY": fakeRepo, "GITHUB_API_URL": gh.srv.URL, "GITHUB_TOKEN": fakeToken,
		"GITHUB_EVENT_NAME": "pull_request", "GITHUB_EVENT_PATH": writeEvent(t, smokePREvent(fork)), "GITHUB_RUN_ID": "5002",
		"GITHUB_WORKSPACE": checkoutDir(t), "RUNNER_TEMP": t.TempDir(), "HOME": t.TempDir()}
	if model != "" {
		env["HEIMDALL_MODEL_KEY"] = smokeModelKey
		env["HEIMDALL_MODEL_BASE_URL"] = model
		env["HEIMDALL_MODEL"] = "test/flash"
		env["HEIMDALL_MODEL_PRICE_INPUT"] = "0.30"
		env["HEIMDALL_MODEL_PRICE_OUTPUT"] = "1.20"
	}
	return env
}

func readArtifact(t *testing.T, p string) action.ProofArtifact {
	t.Helper()
	raw, err := os.ReadFile(p)
	require.NoError(t, err)
	var a action.ProofArtifact
	require.NoError(t, json.Unmarshal(raw, &a))
	require.LessOrEqual(t, len(raw), action.MaxArtifactBytes)
	return a
}

// The proof step reads the PR with the read-only token, runs the review through the real agent loop against the model
// endpoint, and writes an artifact the gate's parser accepts.
func TestActionProofRunsTheLoopAndWritesTheArtifact(t *testing.T) {
	gh := newFakeGitHub(t)
	model, calls := fakeModel(t)
	out := filepath.Join(t.TempDir(), proofFile)
	code, stdout, stderr := runStep(t, smokeDeps(proofEnv(t, gh, model.URL, false), &fakeJobs{}), "proof", "-out", out)
	require.Equal(t, exitActionOK, code, stderr)
	require.Positive(t, calls.Load(), "the model was called")
	a := readArtifact(t, out)
	require.Equal(t, testHead, a.Head)
	require.Equal(t, proofRun, a.WorkflowRun)
	require.Equal(t, proof.KindNotReproduced, a.Kind, stdout)
	require.Positive(t, a.Cost.PromptTokens, "the run's cost is in the artifact")
	require.NotContains(t, stdout+stderr, smokeModelKey)
}

// A fork gets no key by GitHub's rule: the artifact says so and no model is called. A same-repo PR without the key is
// a mistake: the artifact is unknown and the step fails.
func TestActionProofWithoutAKeyIsUnknown(t *testing.T) {
	gh := newFakeGitHub(t)
	out := filepath.Join(t.TempDir(), proofFile)
	code, _, stderr := runStep(t, smokeDeps(proofEnv(t, gh, "", true), nil), "proof", "-out", out)
	require.Equal(t, exitActionOK, code, stderr)
	a := readArtifact(t, out)
	require.Equal(t, proof.KindUnknown, a.Kind)
	require.Equal(t, proof.ReasonForkNoKey, a.Reason)

	code, _, stderr = runStep(t, smokeDeps(proofEnv(t, gh, "", false), nil), "proof", "-out", out)
	require.Equal(t, exitActionFail, code)
	require.Contains(t, stderr, "HEIMDALL_MODEL_KEY")
	require.Equal(t, proof.ReasonInputMissing, readArtifact(t, out).Reason)
}

// A runner that cannot isolate PR code makes the run unknown before any model call.
func TestActionProofOnARunnerThatCannotIsolateIsUnknown(t *testing.T) {
	gh := newFakeGitHub(t)
	model, calls := fakeModel(t)
	out := filepath.Join(t.TempDir(), proofFile)
	code, _, stderr := runStep(t, smokeDeps(proofEnv(t, gh, model.URL, false), nil), "proof", "-out", out)
	require.Equal(t, exitActionOK, code, stderr)
	require.Equal(t, proof.ReasonCannotIsolate, readArtifact(t, out).Reason)
	require.Zero(t, calls.Load())
}

// fakeGH records every gh call and its stdin.
type fakeGH struct {
	calls  [][]string
	stdins [][]byte
	reply  func(args []string) []byte
}

func (f *fakeGH) Run(_ context.Context, args []string, stdin []byte) ([]byte, error) {
	f.calls = append(f.calls, args)
	f.stdins = append(f.stdins, stdin)
	if f.reply != nil {
		return f.reply(args), nil
	}
	return nil, nil
}

// Without -apply init only prints the plan. With it, the log key goes to gh on stdin and never into argv or output.
func TestActionInitPrintsThePlanAndAppliesOnlyWithTheFlag(t *testing.T) {
	gh := &fakeGH{reply: func(args []string) []byte {
		if len(args) > 1 && args[1] == "repos/"+fakeRepo {
			return []byte("main\n")
		}
		return nil
	}}
	deps := smokeDeps(nil, nil)
	deps.gh = gh
	code, stdout, stderr := runStep(t, deps, "init", "-repo", fakeRepo)
	require.Equal(t, exitActionOK, code, stderr)
	require.Contains(t, stdout, "environment heimdall")
	require.Empty(t, gh.calls, "the plan runs no gh command")

	code, stdout, stderr = runStep(t, deps, "init", "-repo", fakeRepo, "-apply")
	require.Equal(t, exitActionOK, code, stderr)
	key := strings.Repeat("07", 32)
	var sent bool
	for i, args := range gh.calls {
		require.NotContains(t, strings.Join(args, " "), key)
		if string(gh.stdins[i]) == key {
			sent = true
		}
	}
	require.True(t, sent, "the log key was sent on stdin")
	require.NotContains(t, stdout+stderr, key)
}

// The whole chain: the trigger run, the proof step's own artifact, then the gate on the proof run. The gate accepts the
// artifact the proof step wrote, records it, and posts heimdall/proof next to heimdall/fast.
func TestActionProofArtifactIsAcceptedByTheGate(t *testing.T) {
	gh := newFakeGitHub(t)
	model, _ := fakeModel(t)
	out := filepath.Join(t.TempDir(), proofFile)
	code, _, stderr := runStep(t, smokeDeps(proofEnv(t, gh, model.URL, false), &fakeJobs{}), "proof", "-out", out)
	require.Equal(t, exitActionOK, code, stderr)
	artifact, err := os.ReadFile(out)
	require.NoError(t, err)

	gh.addArtifact(proofRun, triggerArtifact, map[string][]byte{triggerFile: triggerRecordFor(t)})
	gh.addArtifact(proofRun, proofArtifactName, map[string][]byte{proofFile: artifact})
	env := gateEnv(gh, "workflow_run", writeEvent(t, smokeRunEvent(proofRun, proofWorkflowPath)))
	code, stdout, stderr := runStep(t, smokeDeps(env, nil), "gate")
	require.Equal(t, exitActionOK, code, stderr)
	require.Contains(t, stdout, "recorded as "+string(proof.KindNotReproduced))
	require.NotContains(t, stdout, "artifact refused")
	names := []any{}
	for _, c := range gh.checks {
		names = append(names, c["name"])
	}
	require.ElementsMatch(t, []any{"heimdall/fast", "heimdall/proof"}, names)
}

// The sweep serves every open pull request.
func TestActionGateSweepServesTheOpenPullRequests(t *testing.T) {
	gh := newFakeGitHub(t)
	env := gateEnv(gh, "schedule", writeEvent(t, map[string]any{}))
	code, stdout, stderr := runStep(t, smokeDeps(env, nil), "gate")
	require.Equal(t, exitActionOK, code, stderr)
	require.Contains(t, stdout, "#7 at "+string(testHead))
	require.Len(t, gh.checks, 1)
}
