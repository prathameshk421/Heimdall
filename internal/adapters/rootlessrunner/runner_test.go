package rootlessrunner_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/rootlessrunner"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/stretchr/testify/require"
)

type recordingExecutor struct {
	calls  int
	args   []string
	output []byte
}

func (e *recordingExecutor) Execute(_ context.Context, args []string, _ string) ([]byte, error) {
	e.calls++
	e.args = append([]string(nil), args...)
	return e.output, nil
}

func TestRunnerRejectsOverlayEscapeBeforeRuntime(t *testing.T) {
	executor := &recordingExecutor{}
	runner := rootlessrunner.Runner{
		Policy:   rootlessrunner.Policy{Images: map[string]string{"go": "registry.example/go@sha256:" + strings.Repeat("a", 64)}},
		Executor: executor, Root: t.TempDir(),
	}
	_, err := runner.RunOverlay(context.Background(), "go", bytes.NewReader([]byte("archive")), []ports.File{{Path: "../../host-file", Content: []byte("bad")}}, time.Minute)
	require.Error(t, err)
	require.Zero(t, executor.calls, "an overlay escape must be rejected before starting Podman")
}

func TestRunnerBoundsArchiveBeforeRuntime(t *testing.T) {
	executor := &recordingExecutor{}
	runner := rootlessrunner.Runner{Policy: rootlessrunner.Policy{Images: map[string]string{"go": "registry.example/go@sha256:" + strings.Repeat("a", 64)}}, Executor: executor, Root: t.TempDir()}
	_, err := runner.Run(context.Background(), rootlessrunner.JobInput{Profile: "go", Archive: bytes.NewReader(bytes.Repeat([]byte("x"), (64<<20)+1)), Timeout: time.Minute})
	require.ErrorContains(t, err, "64 MiB")
	require.Zero(t, executor.calls)
}

func TestRunnerPassesOnlyFixedBoundedInvocation(t *testing.T) {
	executor := &recordingExecutor{output: []byte("{\"Action\":\"pass\",\"Package\":\"fixture\",\"Test\":\"TestSafe\"}\n")}
	runner := rootlessrunner.Runner{Policy: rootlessrunner.Policy{Images: map[string]string{"go": "registry.example/go@sha256:" + strings.Repeat("a", 64)}}, Executor: executor, Root: t.TempDir()}
	_, err := runner.Run(context.Background(), rootlessrunner.JobInput{Profile: "go", Archive: bytes.NewReader([]byte("archive")), Timeout: time.Minute})
	require.NoError(t, err)
	require.Equal(t, 1, executor.calls)
	require.Contains(t, executor.args, "--network=none")
	require.Contains(t, executor.args, "--memory=2g")
	require.Contains(t, executor.args, "--cpus=2")
	require.NotContains(t, strings.Join(executor.args, " "), "/var/run/docker.sock")
	require.NotContains(t, strings.Join(executor.args, " "), "169.254.169.254")
}

func TestRunnerReportsMissingExecutor(t *testing.T) {
	runner := rootlessrunner.Runner{}
	_, err := runner.Run(context.Background(), rootlessrunner.JobInput{Profile: "go", Archive: bytes.NewReader(nil), Timeout: time.Minute})
	require.Error(t, err)
	require.NotErrorIs(t, err, rootlessrunner.ErrImageNotAllowed)
}
