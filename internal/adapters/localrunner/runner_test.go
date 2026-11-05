package localrunner_test

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/localrunner"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

type memorySource struct {
	tarballs map[string][]byte
}

func (m *memorySource) Tarball(_ context.Context, _ change.Repo, sha id.SHA) (io.ReadCloser, error) {
	b, ok := m.tarballs[string(sha)]
	if !ok {
		return nil, ports.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

type memoryBlobs struct {
	blobs map[string][]byte
}

func (m *memoryBlobs) Put(_ context.Context, _ ports.BlobClass, r io.Reader) (workflow.BlobRef, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	ref := workflow.BlobRef("blob:sha256:" + strings.Repeat("9", 64))
	m.blobs[string(ref)] = b
	return ref, nil
}

func (m *memoryBlobs) Get(_ context.Context, ref workflow.BlobRef) (io.ReadCloser, error) {
	b, ok := m.blobs[string(ref)]
	if !ok {
		return nil, ports.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func tarDir(t *testing.T, srcDir string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	err := filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if strings.HasSuffix(hdr.Name, ".go.txt") {
			hdr.Name = strings.TrimSuffix(hdr.Name, ".txt")
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = tw.Write(data)
		return err
	})
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	return buf.Bytes()
}

func makeJob(sha id.SHA, timeout time.Duration) ports.SandboxJob {
	return ports.SandboxJob{
		Kind:    ports.SandboxRedFirst,
		Repo:    "acme/demo",
		SHA:     sha,
		Dir:     ".",
		Timeout: timeout,
	}
}

func TestPassingModule(t *testing.T) {
	srcTar := tarDir(t, "testdata/pass")
	sha := id.SHA(strings.Repeat("1", 40))
	src := &memorySource{tarballs: map[string][]byte{string(sha): srcTar}}
	runner := localrunner.New(src)

	ctx := context.Background()
	res, err := runner.Run(ctx, makeJob(sha, 30*time.Second))
	require.NoError(t, err)
	require.NoError(t, res.Validate())
	require.Equal(t, 1, res.Passed)
	require.Equal(t, 0, res.Failed)
	require.Equal(t, 0, res.Errored)
	require.Len(t, res.Tests, 1)
	require.Equal(t, "TestPass", res.Tests[0].Name)
	require.Equal(t, ports.OutcomePassed, res.Tests[0].Outcome)
}

func TestAssertionFailureModule(t *testing.T) {
	srcTar := tarDir(t, "testdata/fail")
	sha := id.SHA(strings.Repeat("2", 40))
	src := &memorySource{tarballs: map[string][]byte{string(sha): srcTar}}
	runner := localrunner.New(src)

	ctx := context.Background()
	res, err := runner.Run(ctx, makeJob(sha, 30*time.Second))
	require.NoError(t, err)
	require.NoError(t, res.Validate())
	require.Equal(t, 0, res.Passed)
	require.Equal(t, 1, res.Failed)
	require.Equal(t, 0, res.Errored)
	require.Len(t, res.Tests, 1)
	require.Equal(t, "TestAssertionFailure", res.Tests[0].Name)
	require.Equal(t, ports.OutcomeFailure, res.Tests[0].Outcome)
}

func TestCompileErrorModule(t *testing.T) {
	srcTar := tarDir(t, "testdata/compile_error")
	sha := id.SHA(strings.Repeat("3", 40))
	src := &memorySource{tarballs: map[string][]byte{string(sha): srcTar}}
	runner := localrunner.New(src)

	ctx := context.Background()
	res, err := runner.Run(ctx, makeJob(sha, 30*time.Second))
	require.NoError(t, err)
	require.NoError(t, res.Validate())
	require.Equal(t, 0, res.Passed)
	require.Equal(t, 0, res.Failed)
	require.Equal(t, 1, res.Errored)
	require.Len(t, res.Tests, 1)
	require.Equal(t, "[build failed]", res.Tests[0].Name)
	require.Equal(t, ports.OutcomeError, res.Tests[0].Outcome)
}

func TestPanicModule(t *testing.T) {
	srcTar := tarDir(t, "testdata/panic")
	sha := id.SHA(strings.Repeat("4", 40))
	src := &memorySource{tarballs: map[string][]byte{string(sha): srcTar}}
	runner := localrunner.New(src)

	ctx := context.Background()
	res, err := runner.Run(ctx, makeJob(sha, 30*time.Second))
	require.NoError(t, err)
	require.NoError(t, res.Validate())
	require.Equal(t, 0, res.Passed)
	require.Equal(t, 0, res.Failed)
	require.Equal(t, 1, res.Errored)
	require.Len(t, res.Tests, 1)
	require.Equal(t, "TestPanics", res.Tests[0].Name)
	require.Equal(t, ports.OutcomeError, res.Tests[0].Outcome)
}

func TestSkippedModule(t *testing.T) {
	srcTar := tarDir(t, "testdata/skip")
	sha := id.SHA(strings.Repeat("5", 40))
	src := &memorySource{tarballs: map[string][]byte{string(sha): srcTar}}
	runner := localrunner.New(src)

	ctx := context.Background()
	res, err := runner.Run(ctx, makeJob(sha, 30*time.Second))
	require.NoError(t, err)
	require.NoError(t, res.Validate())
	require.Equal(t, 0, res.Passed)
	require.Equal(t, 0, res.Failed)
	require.Equal(t, 0, res.Errored)
	require.Len(t, res.Tests, 1)
	require.Equal(t, "TestSkipped", res.Tests[0].Name)
	require.Equal(t, ports.OutcomeSkipped, res.Tests[0].Outcome)
}

func TestTimeoutModuleKillsProcessGroup(t *testing.T) {
	srcTar := tarDir(t, "testdata/timeout")
	sha := id.SHA(strings.Repeat("6", 40))
	src := &memorySource{tarballs: map[string][]byte{string(sha): srcTar}}

	var tempDirUsed string
	runner := localrunner.New(src, localrunner.WithTempDirHook(func(dir string) {
		tempDirUsed = dir
	}))

	pidFile := "/tmp/heimdall_timeout_child.pid"
	_ = os.Remove(pidFile)

	job := localrunner.Job{
		SandboxJob: makeJob(sha, 5*time.Second),
	}

	start := time.Now()
	res, err := runner.RunJob(context.Background(), job)
	elapsed := time.Since(start)

	// Must finish quickly around the timeout (under 10s), not wait 30s
	require.Less(t, elapsed, 10*time.Second)
	require.NoError(t, err)
	require.NoError(t, res.Validate())
	require.Equal(t, 1, res.Errored)
	require.Len(t, res.Tests, 1)
	require.Equal(t, "TestSleep", res.Tests[0].Name)
	require.Equal(t, ports.OutcomeError, res.Tests[0].Outcome)

	require.NotEmpty(t, tempDirUsed)
	_, statErr := os.Stat(tempDirUsed)
	require.True(t, os.IsNotExist(statErr), "temp dir must be removed")

	// Verify the spawned child process sleep 30 was terminated with the process group
	b, readErr := os.ReadFile(pidFile)
	require.NoError(t, readErr, "child pid file must have been created by child process")
	_ = os.Remove(pidFile)
	pid, parseErr := strconv.Atoi(strings.TrimSpace(string(b)))
	require.NoError(t, parseErr)
	require.Positive(t, pid)
	// ESRCH means process does not exist
	killErr := syscall.Kill(pid, 0)
	require.Equal(t, syscall.ESRCH, killErr, "spawned child process must be dead")
}

func TestParentEnvNotLeaked(t *testing.T) {
	srcTar := tarDir(t, "testdata/readenv")
	sha := id.SHA(strings.Repeat("7", 40))
	src := &memorySource{tarballs: map[string][]byte{string(sha): srcTar}}
	runner := localrunner.New(src)

	t.Setenv("SECRET_TOKEN", "super-secret-token-12345")
	t.Setenv("PARENT_SECRET", "cannot-leak-this")

	ctx := context.Background()
	res, err := runner.Run(ctx, makeJob(sha, 30*time.Second))
	require.NoError(t, err)
	require.NoError(t, res.Validate())
	require.Equal(t, 1, res.Passed)
	require.Equal(t, 0, res.Failed)
	require.Equal(t, 0, res.Errored)
}

func TestTarballPathTraversalRefused(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{
		Name: "../evil.txt",
		Mode: 0o644,
		Size: int64(len("evil")),
	}
	require.NoError(t, tw.WriteHeader(hdr))
	_, _ = tw.Write([]byte("evil"))
	require.NoError(t, tw.Close())

	sha := id.SHA(strings.Repeat("8", 40))
	src := &memorySource{tarballs: map[string][]byte{string(sha): buf.Bytes()}}
	runner := localrunner.New(src)

	_, err := runner.Run(context.Background(), makeJob(sha, 30*time.Second))
	require.Error(t, err)
	require.ErrorIs(t, err, localrunner.ErrPathTraversal)
}

func TestTarballAbsolutePathRefused(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{
		Name: "/tmp/evil.txt",
		Mode: 0o644,
		Size: int64(len("evil")),
	}
	require.NoError(t, tw.WriteHeader(hdr))
	_, _ = tw.Write([]byte("evil"))
	require.NoError(t, tw.Close())

	sha := id.SHA(strings.Repeat("8", 40))
	src := &memorySource{tarballs: map[string][]byte{string(sha): buf.Bytes()}}
	runner := localrunner.New(src)

	_, err := runner.Run(context.Background(), makeJob(sha, 30*time.Second))
	require.Error(t, err)
	require.ErrorIs(t, err, localrunner.ErrAbsolutePath)
}

func TestTarballSymlinkEscapeRefused(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{
		Typeflag: tar.TypeSymlink,
		Name:     "escapelink",
		Linkname: "../../../../etc/passwd",
	}
	require.NoError(t, tw.WriteHeader(hdr))
	require.NoError(t, tw.Close())

	sha := id.SHA(strings.Repeat("8", 40))
	src := &memorySource{tarballs: map[string][]byte{string(sha): buf.Bytes()}}
	runner := localrunner.New(src)

	_, err := runner.Run(context.Background(), makeJob(sha, 30*time.Second))
	require.Error(t, err)
	require.ErrorIs(t, err, localrunner.ErrSymlinkEscape)
}

func TestTarballOversizedRefused(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{
		Name: "huge.bin",
		Mode: 0o644,
		Size: 65 << 20, // 65 MiB
	}
	// Write header only
	require.NoError(t, tw.WriteHeader(hdr))

	sha := id.SHA(strings.Repeat("8", 40))
	src := &memorySource{tarballs: map[string][]byte{string(sha): buf.Bytes()}}
	runner := localrunner.New(src)

	_, err := runner.Run(context.Background(), makeJob(sha, 30*time.Second))
	require.Error(t, err)
	require.ErrorIs(t, err, localrunner.ErrTarTooLarge)
}

func TestArbitraryCommandRefused(t *testing.T) {
	srcTar := tarDir(t, "testdata/pass")
	sha := id.SHA(strings.Repeat("1", 40))
	src := &memorySource{tarballs: map[string][]byte{string(sha): srcTar}}
	runner := localrunner.New(src)

	job := localrunner.Job{
		SandboxJob: makeJob(sha, 30*time.Second),
		Command:    []string{"sh", "-c", "echo hello"},
	}

	_, err := runner.RunJob(context.Background(), job)
	require.Error(t, err)
	require.ErrorIs(t, err, localrunner.ErrInvalidCommand)
}

func TestTempDirAlwaysRemoved(t *testing.T) {
	srcTar := tarDir(t, "testdata/fail")
	sha := id.SHA(strings.Repeat("2", 40))
	src := &memorySource{tarballs: map[string][]byte{string(sha): srcTar}}

	var createdDir string
	runner := localrunner.New(src, localrunner.WithTempDirHook(func(dir string) {
		createdDir = dir
	}))

	job := localrunner.Job{
		SandboxJob: makeJob(sha, 30*time.Second),
	}
	_, _ = runner.RunJob(context.Background(), job)

	require.NotEmpty(t, createdDir)
	_, err := os.Stat(createdDir)
	require.True(t, os.IsNotExist(err), "temp dir %s must be removed", createdDir)
}

func TestOverlayFilesApplied(t *testing.T) {
	// Base module has a failing test
	srcTar := tarDir(t, "testdata/fail")
	sha := id.SHA(strings.Repeat("2", 40))
	src := &memorySource{tarballs: map[string][]byte{string(sha): srcTar}}
	runner := localrunner.New(src)

	// Overlay replaces fail_test.go with a passing test
	overlayPass := []byte("package fail\n\nimport \"testing\"\n\nfunc TestAssertionFailure(t *testing.T) {}\n")
	job := localrunner.Job{
		SandboxJob: makeJob(sha, 30*time.Second),
		Overlay: []ports.File{
			{Path: "fail_test.go", Content: overlayPass},
		},
	}

	res, err := runner.RunJob(context.Background(), job)
	require.NoError(t, err)
	require.Equal(t, 1, res.Passed)
	require.Equal(t, 0, res.Failed)
}

func TestBlobStoreIntegration(t *testing.T) {
	srcTar := tarDir(t, "testdata/pass")
	sha := id.SHA(strings.Repeat("1", 40))
	src := &memorySource{tarballs: map[string][]byte{string(sha): srcTar}}
	blobs := &memoryBlobs{blobs: make(map[string][]byte)}

	runner := localrunner.New(src, localrunner.WithBlobs(blobs))
	res, err := runner.Run(context.Background(), makeJob(sha, 30*time.Second))
	require.NoError(t, err)
	require.NotEmpty(t, res.LogRef)
	require.NotEmpty(t, blobs.blobs[string(res.LogRef)])
}
