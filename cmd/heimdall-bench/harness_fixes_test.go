package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// The recall-harness fixes are tested at the seam the reviewer uses: the replay's own tools and the budget-stop report.

type countingBenchSandbox struct {
	calls int
	err   error
}

func (s *countingBenchSandbox) Run(context.Context, ports.SandboxJob) (ports.SandboxResult, error) {
	return ports.SandboxResult{}, nil
}

func (s *countingBenchSandbox) RunOverlay(context.Context, ports.SandboxJob, []ports.File) (ports.SandboxResult, error) {
	return ports.SandboxResult{}, nil
}

func (s *countingBenchSandbox) RunCommand(_ context.Context, _, _, _ string, _ []string, _ []ports.File, _ time.Duration) (commandResult, error) {
	s.calls++
	return commandResult{Exit: 0, Output: "ok"}, s.err
}

func TestReplayRunCommandIsCappedAtTwoCalls(t *testing.T) {
	sb := &countingBenchSandbox{}
	rp := &replay{sandbox: sb}
	var desc string
	for _, tool := range rp.toolSpecs() {
		if tool.spec.Name == "run_command" {
			desc = tool.spec.Description
		}
	}
	require.Contains(t, desc, "(max 2 calls per review)")

	args := json.RawMessage(`{"argv":["go","test","./..."]}`)
	for i := 0; i < maxCommandCalls; i++ {
		out, err := rp.runCommand(t.Context(), args)
		require.NoError(t, err, "call %d", i+1)
		require.Contains(t, out, "exit 0")
	}
	require.Equal(t, maxCommandCalls, sb.calls)

	_, err := rp.runCommand(t.Context(), args)
	require.ErrorContains(t, err, "used all 2 run_command calls")
	require.ErrorContains(t, err, "run_test")
	require.ErrorContains(t, err, "propose_finding")
	require.ErrorContains(t, err, "finish_review")
	require.Equal(t, maxCommandCalls, sb.calls, "a refused call never reaches the sandbox")

	_, err = rp.runCommand(t.Context(), json.RawMessage(`{}`))
	require.ErrorContains(t, err, "used all 2 run_command calls")
	require.Equal(t, maxCommandCalls, sb.calls)
}

func TestReplayRunCommandDoesNotSpendACallOnBadArguments(t *testing.T) {
	sb := &countingBenchSandbox{}
	rp := &replay{sandbox: sb}
	_, err := rp.runCommand(t.Context(), json.RawMessage(`{}`))
	require.ErrorContains(t, err, "argv")
	require.Zero(t, sb.calls)

	for i := 0; i < maxCommandCalls; i++ {
		_, err := rp.runCommand(t.Context(), json.RawMessage(`{"argv":["true"]}`))
		require.NoError(t, err)
	}
	require.Equal(t, maxCommandCalls, sb.calls)
}

func TestReplayHeadFileRefusesSymlinksIntoGit(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git", "config"), []byte("[core]\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ok.go"), []byte("package ok\n"), 0o600))
	require.NoError(t, os.Symlink(".git/config", filepath.Join(dir, "leak")))
	require.NoError(t, os.Symlink(".git", filepath.Join(dir, "gitlink")))
	require.NoError(t, os.Symlink("leak", filepath.Join(dir, "chain")))
	rp := &replay{workdir: dir}

	b, err := rp.headFile("ok.go")
	require.NoError(t, err)
	require.Contains(t, string(b), "package ok")

	for _, p := range []string{"leak", "gitlink/config", "chain"} {
		_, err := rp.headFile(p)
		require.ErrorContains(t, err, "git's directory", p)
		require.NotContains(t, err.Error(), "[core]", "git's file is never read")
	}
}

func TestBudgetStopMessageDistinguishesEveryCause(t *testing.T) {
	const perCase, inCap, outCap = 0.50, int64(800_000), int64(40_000)
	for _, tt := range []struct {
		cause ports.AgentBudgetCause
		want  string
	}{
		{ports.AgentBudgetUSD, "stopped by the per-case USD budget ($0.50)"},
		{ports.AgentBudgetInputTokens, "stopped by the per-case input token cap (800000)"},
		{ports.AgentBudgetOutputTokens, "stopped by the per-case output token cap (40000)"},
		{ports.AgentBudgetUsage, "stopped by an invalid usage report from the model"},
		{ports.AgentBudgetRequestSize, "stopped because the next request could not be sized against the caps"},
		{"", "stopped by the per-case USD budget ($0.50)"}, // an unlabelled stop still reports the USD cap
	} {
		require.Equal(t, tt.want, budgetStopMessage(tt.cause, perCase, inCap, outCap), tt.cause)
	}
}

func TestCaseTokenCapsAreSharedByTheLoopAndTheReport(t *testing.T) {
	inCap, outCap := caseTokenCaps(reviewOptions{})
	require.Equal(t, int64(inputTokensPerCase), inCap)
	require.Equal(t, int64(outputTokensPerCas), outCap)

	inCap, outCap = caseTokenCaps(reviewOptions{inputTokens: 1234, outputTokens: 56})
	require.Equal(t, int64(1234), inCap)
	require.Equal(t, int64(56), outCap)
}
