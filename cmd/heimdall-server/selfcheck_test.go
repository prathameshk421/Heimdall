package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/outbox"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

func captureLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, nil)), &buf
}

type fakeRunnerHealth struct {
	kinds []ports.SandboxKind
	err   error
}

func (f fakeRunnerHealth) Health(context.Context) ([]ports.SandboxKind, error) { return f.kinds, f.err }

// A runner that answers but runs no check jobs cannot serve run_check: the tool is withheld with that reason.
func TestTheRunnerProbeRequiresTheCheckAndTestKinds(t *testing.T) {
	require.NoError(t, runnerProbe(fakeRunnerHealth{kinds: []ports.SandboxKind{ports.SandboxCheck, ports.SandboxTest}})(context.Background()))
	require.Error(t, runnerProbe(fakeRunnerHealth{err: errors.New("dial")})(context.Background()))

	logger, _ := captureLogger()
	got := runSelfCheck(context.Background(), logger, time.Second, []dependency{{name: "runner", tools: []string{"run_check"},
		reason: "runner unreachable", probe: runnerProbe(fakeRunnerHealth{kinds: []ports.SandboxKind{ports.SandboxRedFirst}})}})
	require.Equal(t, "runner does not run check and test jobs", got.withheld["run_check"])
}

type fakeCodeReads struct {
	headErr error
	readErr error
	reads   []change.Repo
}

func (f *fakeCodeReads) DefaultHead(context.Context, change.Repo) (id.SHA, error) {
	return id.SHA(strings.Repeat("a", 40)), f.headErr
}

func (f *fakeCodeReads) ReadFile(_ context.Context, repo change.Repo, _ id.SHA, _ string) ([]byte, error) {
	f.reads = append(f.reads, repo)
	return nil, f.readErr
}

// The code host answered when it serves the file or says it is not there; only a failed call fails the probe.
func TestTheCodeHostProbeReadsEachMappedRepository(t *testing.T) {
	repos := []change.Repo{"acme/pay", "acme/web"}
	ok := &fakeCodeReads{readErr: ports.ErrNotFound}
	require.NoError(t, codeHostProbe(ok, repos)(context.Background()))
	require.Equal(t, repos, ok.reads)
	require.NoError(t, codeHostProbe(&fakeCodeReads{headErr: ports.ErrNotFound}, repos)(context.Background()), "an empty repository")
	require.Error(t, codeHostProbe(&fakeCodeReads{readErr: errors.New("502 bad gateway")}, repos)(context.Background()))
	require.Error(t, codeHostProbe(&fakeCodeReads{headErr: errors.New("401")}, repos)(context.Background()))
}

type nopExecutor struct{}

func (nopExecutor) Execute(context.Context, change.Effect) error { return nil }

// A row job kind without an executor and a kind tool nobody registered are both named, so the server refuses to start
// with every problem at once.
func TestTheWiringCheckNamesAMissingExecutorAndAMissingTool(t *testing.T) {
	var tools []string
	tools = append(tools, workflow.SharedTools()...)
	for _, k := range workflow.Kinds() {
		tools = append(tools, k.Tools...)
	}
	schemas := []string{"TestSubmission@1", "Review@2", "Verdict@1", "Profile@1"}
	execs := map[change.EffectKind]outbox.Executor{}
	for _, k := range change.RowJobKinds() {
		execs[k] = nopExecutor{}
	}
	require.Empty(t, wiringProblems(tools, schemas, execs))

	delete(execs, change.EffectVerifyEvidence)
	var text []string
	for _, p := range wiringProblems(slicesWithout(tools, "run_check"), schemas, execs) {
		text = append(text, p.Error())
	}
	joined := strings.Join(text, "\n")
	require.Contains(t, joined, "job kind verify_evidence has no executor")
	require.Contains(t, joined, `kind review: tool "run_check" is not registered`)
	require.Contains(t, joined, `kind verdict: tool "run_check" is not registered`)
}

func slicesWithout(in []string, drop string) []string {
	var out []string
	for _, s := range in {
		if s != drop {
			out = append(out, s)
		}
	}
	return out
}
