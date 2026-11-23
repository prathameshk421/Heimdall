package localrunner_test

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
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

// TestModuleOutcomes runs each fixture module through the real go tool: pass, assertion failure, compile error,
// panic and skip each report their own outcome.
func TestModuleOutcomes(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		dir                     string
		shaChar                 string
		passed, failed, errored int
		name                    string
		outcome                 ports.Outcome
	}{
		{"pass", "1", 1, 0, 0, "TestPass", ports.OutcomePassed},
		{"fail", "2", 0, 1, 0, "TestAssertionFailure", ports.OutcomeFailure},
		{"compile_error", "3", 0, 0, 1, "[build failed]", ports.OutcomeError},
		{"panic", "4", 0, 0, 1, "TestPanics", ports.OutcomeError},
		{"skip", "5", 0, 0, 0, "TestSkipped", ports.OutcomeSkipped},
	} {
		t.Run(tt.dir, func(t *testing.T) {
			t.Parallel()
			sha := id.SHA(strings.Repeat(tt.shaChar, 40))
			src := &memorySource{tarballs: map[string][]byte{string(sha): tarDir(t, "testdata/"+tt.dir)}}
			res, err := localrunner.New(src).Run(context.Background(), makeJob(sha, 30*time.Second))
			require.NoError(t, err)
			require.NoError(t, res.Validate())
			require.Equal(t, tt.passed, res.Passed)
			require.Equal(t, tt.failed, res.Failed)
			require.Equal(t, tt.errored, res.Errored)
			require.Len(t, res.Tests, 1)
			require.Equal(t, tt.name, res.Tests[0].Name)
			require.Equal(t, tt.outcome, res.Tests[0].Outcome)
		})
	}
}

func TestTimeoutModuleKillsProcessGroup(t *testing.T) {
	srcTar := tarDir(t, "testdata/timeout")
	sha := id.SHA(strings.Repeat("6", 40))
	src := &memorySource{tarballs: map[string][]byte{string(sha): srcTar}}

	var tempDirUsed string
	var tempDirMu sync.Mutex
	runner := localrunner.New(src, localrunner.WithTempDirHook(func(dir string) {
		tempDirMu.Lock()
		defer tempDirMu.Unlock()
		tempDirUsed = dir
	}))

	readyCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	job := localrunner.Job{
		SandboxJob: makeJob(sha, 2*time.Second),
	}

	type runResult struct {
		res ports.SandboxResult
		err error
	}
	resultCh := make(chan runResult, 1)
	start := time.Now()
	go func() {
		res, err := runner.RunJob(readyCtx, job)
		resultCh <- runResult{res: res, err: err}
	}()

	pidFile := ""
	var pid int
	readyTicker := time.NewTicker(10 * time.Millisecond)
	defer readyTicker.Stop()
	for pidFile == "" {
		tempDirMu.Lock()
		runDir := tempDirUsed
		tempDirMu.Unlock()
		if runDir != "" {
			candidate := filepath.Join(runDir, "timeout-child.pid")
			if _, statErr := os.Stat(candidate); statErr == nil {
				pidFile = candidate
				b, readErr := os.ReadFile(pidFile)
				require.NoError(t, readErr)
				pid, readErr = strconv.Atoi(strings.TrimSpace(string(b)))
				require.NoError(t, readErr)
				require.Positive(t, pid)
				break
			} else if !os.IsNotExist(statErr) {
				require.NoError(t, statErr)
			}
		}
		select {
		case result := <-resultCh:
			require.FailNow(t, "runner completed before timeout child signaled ready", "error: %v", result.err)
		case <-readyCtx.Done():
			require.FailNow(t, "timed out waiting for timeout child ready signal", readyCtx.Err())
		case <-readyTicker.C:
		}
	}

	var res ports.SandboxResult
	var err error
	select {
	case result := <-resultCh:
		res, err = result.res, result.err
	case <-readyCtx.Done():
		require.FailNow(t, "runner did not finish before deadline", readyCtx.Err())
	}
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
	tempDirMu.Lock()
	runDir := tempDirUsed
	tempDirMu.Unlock()
	_, statErr := os.Stat(runDir)
	require.True(t, os.IsNotExist(statErr), "temp dir must be removed")

	// A killed grandchild can remain briefly as a zombie until init reaps it. It is dead for this
	// contract because it cannot execute; treating a zombie PID as a live child made this test racy.
	_ = pidFile
	require.Eventually(t, func() bool { return !processIsRunning(pid) }, 2*time.Second, 10*time.Millisecond,
		"spawned child process must not still be running")
}

func processIsRunning(pid int) bool {
	if runtime.GOOS == "linux" {
		stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
		if os.IsNotExist(err) {
			return false
		}
		if err != nil {
			return true
		}
		// comm is parenthesized and may itself contain spaces or parentheses; state follows the final ')'.
		stateFields := strings.Fields(string(stat[strings.LastIndex(string(stat), ")")+1:]))
		return len(stateFields) == 0 || (stateFields[0] != "Z" && stateFields[0] != "X")
	}
	err := syscall.Kill(pid, 0)
	return err == nil || !errors.Is(err, syscall.ESRCH)
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

// TestTarballRefused covers every hostile tarball shape: traversal, absolute path, symlink escape and oversize.
// Each is refused before any test runs.
func TestTarballRefused(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name     string
		hdr      *tar.Header
		body     string
		closeTar bool
		want     error
	}{
		{"path traversal", &tar.Header{Name: "../evil.txt", Mode: 0o644, Size: 4}, "evil", true, localrunner.ErrPathTraversal},
		{"absolute path", &tar.Header{Name: "/tmp/evil.txt", Mode: 0o644, Size: 4}, "evil", true, localrunner.ErrAbsolutePath},
		{"symlink escape", &tar.Header{Typeflag: tar.TypeSymlink, Name: "escapelink", Linkname: "../../../../etc/passwd"}, "", true, localrunner.ErrSymlinkEscape},
		// Header only: closing the writer would try to pad the declared 65 MiB, and the runner refuses on the header.
		{"oversized", &tar.Header{Name: "huge.bin", Mode: 0o644, Size: 65 << 20}, "", false, localrunner.ErrTarTooLarge},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			tw := tar.NewWriter(&buf)
			require.NoError(t, tw.WriteHeader(tt.hdr))
			if tt.body != "" {
				_, _ = tw.Write([]byte(tt.body))
			}
			if tt.closeTar {
				require.NoError(t, tw.Close())
			}

			sha := id.SHA(strings.Repeat("8", 40))
			src := &memorySource{tarballs: map[string][]byte{string(sha): buf.Bytes()}}
			_, err := localrunner.New(src).Run(context.Background(), makeJob(sha, 30*time.Second))
			require.ErrorIs(t, err, tt.want)
		})
	}
}

func TestArbitraryCommandRefused(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
