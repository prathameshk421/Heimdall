package main

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/uidsandbox"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const (
	testHead = id.SHA("1111111111111111111111111111111111111111")
	testBase = id.SHA("2222222222222222222222222222222222222222")
)

// untar lists a tar stream as name to content, and a symlink as "-> target".
func untar(t *testing.T, raw []byte) map[string]string {
	t.Helper()
	out := map[string]string{}
	tr := tar.NewReader(bytes.NewReader(raw))
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		require.NoError(t, err)
		switch h.Typeflag {
		case tar.TypeSymlink:
			out[h.Name] = "-> " + h.Linkname
		case tar.TypeReg:
			b, err := io.ReadAll(tr)
			require.NoError(t, err)
			out[h.Name] = string(b)
		}
	}
}

func checkoutDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git", "config"), []byte("[credential]"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "pay"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pay", "pay.go"), []byte("package pay\n"), 0o644))
	require.NoError(t, os.Symlink("/etc/passwd", filepath.Join(dir, "pay", "link")))
	return dir
}

// The staged tree has no .git, keeps a link as a link (never its target's content), and puts each overlay in place:
// one that replaces a file, one that is new.
func TestStageTreeWritesOverlaysAndNeverFollowsALink(t *testing.T) {
	dir := checkoutDir(t)
	raw, err := stageTree(dir, []ports.File{
		{Path: "pay/pay.go", Content: []byte("package pay // reverted\n")},
		{Path: "pay/pay_repro_test.go", Content: []byte("package pay\n")},
		{Path: "pay/link", Content: []byte("plain")},
	})
	require.NoError(t, err)
	got := untar(t, raw)
	require.Equal(t, map[string]string{
		"pay/pay.go":            "package pay // reverted\n",
		"pay/pay_repro_test.go": "package pay\n",
		"pay/link":              "plain",
	}, got)

	raw, err = stageTree(dir, nil)
	require.NoError(t, err)
	require.Equal(t, "-> /etc/passwd", untar(t, raw)["pay/link"])
}

func TestStageTreeRefusesAnOverlayOutsideTheTree(t *testing.T) {
	_, err := stageTree(checkoutDir(t), []ports.File{{Path: "../escape_test.go", Content: []byte("x")}})
	require.Error(t, err)
}

type fakeJobs struct {
	staged  int
	fetches int
	execs   []uidsandbox.Job
	report  string
}

func (f *fakeJobs) Stage(_ context.Context, tree io.Reader, _ int) error {
	f.staged++
	_, err := io.Copy(io.Discard, tree)
	return err
}

func (f *fakeJobs) Fetch(_ context.Context, _ uidsandbox.Job) (uidsandbox.Result, error) {
	f.fetches++
	return uidsandbox.Result{}, nil
}

func (f *fakeJobs) Exec(_ context.Context, j uidsandbox.Job) (uidsandbox.Result, error) {
	f.execs = append(f.execs, j)
	return uidsandbox.Result{Exit: 1, Stdout: []byte(f.report)}, nil
}

// Each job stages a fresh tree, the modules are fetched once, and a commit other than the head or the base is refused.
func TestSandboxBridgeRunsTheGoTestsOfTheJobDirectory(t *testing.T) {
	jobs := &fakeJobs{}
	b := &sandboxBridge{sbx: jobs, checkout: checkoutDir(t), head: testHead, base: testBase}
	job := ports.SandboxJob{Kind: ports.SandboxRepro, SHA: testHead, Dir: "pay", Timeout: time.Minute}
	for range 2 {
		_, err := b.RunOverlay(context.Background(), job, []ports.File{{Path: "pay/x_test.go", Content: []byte("package pay\n")}})
		require.NoError(t, err)
	}
	require.Equal(t, 2, jobs.staged)
	require.Equal(t, 1, jobs.fetches)
	require.Equal(t, "pay", jobs.execs[0].Dir)
	require.Equal(t, uidsandbox.ResultGoTestJSON, jobs.execs[0].Result)

	job.SHA = id.SHA("3333333333333333333333333333333333333333")
	_, err := b.Run(context.Background(), job)
	require.Error(t, err)
}
