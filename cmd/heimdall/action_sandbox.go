package main

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/testevents"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/uidsandbox"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/action"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// The bounds of the tree the bridge stages, and of the dependency fetch. The tree is the head checkout of the proof
// job; a repository larger than this is refused, and the proof run reports the sandbox as the missing fact.
const (
	maxStageFiles = 200_000
	maxStageBytes = 256 << 20
	fetchTimeout  = 3 * time.Minute
)

// sandboxJobs is the part of uidsandbox.Sandbox the bridge uses. The tests replace it with a fake.
type sandboxJobs interface {
	Stage(ctx context.Context, tree io.Reader, stripComponents int) error
	Fetch(ctx context.Context, j uidsandbox.Job) (uidsandbox.Result, error)
	Exec(ctx context.Context, j uidsandbox.Job) (uidsandbox.Result, error)
}

// sandboxBridge is action.OverlaySandbox on the uidsandbox adapter. Every job stages the head checkout with the
// overlays written in, so a job never sees the files of the job before it. The first job fetches the Go modules in
// the fetch phase; every test runs in the execution phase, with loopback only.
//
// The proof run names two commits: the head, and the merge base for the tree with the suspected lines put back to
// their base text. That tree is the head tree with the reverted file as an overlay (proofSteps), so both commits stage
// the head checkout. Any other commit is refused: the job holds no tree for it.
type sandboxBridge struct {
	sbx      sandboxJobs
	checkout string
	head     id.SHA
	base     id.SHA

	mu      sync.Mutex
	fetched bool
}

var _ action.OverlaySandbox = (*sandboxBridge)(nil)

// Run runs a job with no overlay.
func (b *sandboxBridge) Run(ctx context.Context, j ports.SandboxJob) (ports.SandboxResult, error) {
	return b.RunOverlay(ctx, j, nil)
}

// RunOverlay stages the head tree with the overlays and runs the Go tests of the job's directory.
func (b *sandboxBridge) RunOverlay(ctx context.Context, j ports.SandboxJob, overlays []ports.File) (ports.SandboxResult, error) {
	if j.SHA != b.head && j.SHA != b.base {
		return ports.SandboxResult{}, fmt.Errorf("sandbox: commit %s is neither the head nor the merge base of this run", j.SHA)
	}
	if j.Timeout <= 0 {
		return ports.SandboxResult{}, errors.New("sandbox: the job has no timeout")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	tree, err := stageTree(b.checkout, overlays)
	if err != nil {
		return ports.SandboxResult{}, fmt.Errorf("sandbox: build the tree: %w", err)
	}
	if err := b.sbx.Stage(ctx, bytes.NewReader(tree), 0); err != nil {
		return ports.SandboxResult{}, fmt.Errorf("sandbox: stage the tree: %w", err)
	}
	if err := b.fetchOnce(ctx, j.Dir); err != nil {
		return ports.SandboxResult{}, err
	}
	res, err := b.sbx.Exec(ctx, uidsandbox.Job{
		Argv: []string{"go", "test", "-json", "-count=1", "."}, Dir: j.Dir,
		Env:     []string{"GOFLAGS=-mod=mod", "GOTOOLCHAIN=local", "GOPROXY=off"},
		Timeout: j.Timeout, Result: uidsandbox.ResultGoTestJSON,
	})
	if err != nil {
		return ports.SandboxResult{}, fmt.Errorf("sandbox: run the tests of %s: %w", j.Dir, err)
	}
	out := testevents.ReportToSandboxResult(res.Report, logRefOf(res))
	out.Exit, out.Output = res.Exit, outputTail(res)
	return out, nil
}

// fetchOnce downloads the modules once per run, in the fetch phase, where egress goes only through the proxy. The
// module cache lives in the sandbox home, which a new stage keeps.
func (b *sandboxBridge) fetchOnce(ctx context.Context, dir string) error {
	if b.fetched {
		return nil
	}
	res, err := b.sbx.Fetch(ctx, uidsandbox.Job{Argv: []string{"go", "mod", "download"}, Dir: dir,
		Env: []string{"GOTOOLCHAIN=local"}, Timeout: fetchTimeout})
	if err != nil {
		return fmt.Errorf("sandbox: fetch the modules: %w", err)
	}
	if res.Exit != 0 {
		return fmt.Errorf("sandbox: go mod download exited %d: %s", res.Exit, clipText(strings.TrimSpace(string(res.Stderr)), 400))
	}
	b.fetched = true
	return nil
}

// logRefOf names the job's output by its hash. The output itself stays in the job; the reference only lets the port's
// result validate.
func logRefOf(res uidsandbox.Result) workflow.BlobRef {
	sum := sha256.New()
	sum.Write(res.Stdout)
	sum.Write(res.Stderr)
	return workflow.BlobRef("blob:sha256:" + hex.EncodeToString(sum.Sum(nil)))
}

// outputTail is the end of the job's output, bounded as the port requires. The end holds the failure.
func outputTail(res uidsandbox.Result) string {
	text := string(res.Stderr)
	if len(res.Stdout) > 0 {
		text = string(res.Stdout) + "\n" + text
	}
	if len(text) > ports.MaxCheckOutput {
		text = text[len(text)-ports.MaxCheckOutput:]
	}
	return strings.ToValidUTF8(text, "")
}

// stageTree writes the checkout as a tar stream, without .git, with each overlay in place of the file at its path.
// It reads the checkout through os.Root, so a symlink in the PR cannot make it read outside the checkout: a symlink is
// written as a symlink, and the sandbox user is the one that follows it.
func stageTree(checkout string, overlays []ports.File) ([]byte, error) {
	over := make(map[string][]byte, len(overlays))
	for _, f := range overlays {
		if err := f.Validate(); err != nil {
			return nil, fmt.Errorf("overlay %q: %w", clipText(f.Path, 80), err)
		}
		over[path.Clean(f.Path)] = f.Content
	}
	root, err := os.OpenRoot(checkout)
	if err != nil {
		return nil, fmt.Errorf("open the checkout: %w", err)
	}
	defer func() { _ = root.Close() }()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	st := &stager{root: root, tw: tw, over: over}
	if err := fs.WalkDir(root.FS(), ".", st.visit); err != nil {
		return nil, err
	}
	if err := st.writeOverlays(); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("close the tree: %w", err)
	}
	return buf.Bytes(), nil
}

// stager is one walk of the checkout into a tar stream.
type stager struct {
	root  *os.Root
	tw    *tar.Writer
	over  map[string][]byte
	files int
	bytes int64
}

func (s *stager) visit(p string, d fs.DirEntry, err error) error {
	if err != nil {
		return fmt.Errorf("read the checkout at %s: %w", p, err)
	}
	if p == "." {
		return nil
	}
	if d.Name() == ".git" {
		if d.IsDir() {
			return fs.SkipDir
		}
		return nil
	}
	if err := s.count(0); err != nil {
		return err
	}
	switch {
	case d.IsDir():
		return s.header(&tar.Header{Typeflag: tar.TypeDir, Name: p + "/", Mode: 0o755})
	case d.Type()&fs.ModeSymlink != 0:
		if content, ok := s.over[p]; ok {
			// An overlay replaces a link with a plain file, so the test never writes through a link of the PR.
			delete(s.over, p)
			return s.file(p, content, 0o644)
		}
		target, err := s.root.Readlink(p)
		if err != nil {
			return fmt.Errorf("read the link %s: %w", p, err)
		}
		return s.header(&tar.Header{Typeflag: tar.TypeSymlink, Name: p, Linkname: target, Mode: 0o777})
	case d.Type().IsRegular():
		if content, ok := s.over[p]; ok {
			delete(s.over, p)
			return s.file(p, content, 0o644)
		}
		return s.copyFile(p, d)
	}
	// A device, a socket or a pipe is not part of a source tree.
	return nil
}

func (s *stager) copyFile(p string, d fs.DirEntry) error {
	info, err := d.Info()
	if err != nil {
		return fmt.Errorf("stat %s: %w", p, err)
	}
	if err := s.count(info.Size()); err != nil {
		return err
	}
	content, err := s.root.ReadFile(p)
	if err != nil {
		return fmt.Errorf("read %s: %w", p, err)
	}
	return s.file(p, content, int64(info.Mode().Perm()|0o600))
}

// writeOverlays writes the overlays whose path the checkout does not hold, such as a new test file.
func (s *stager) writeOverlays() error {
	for _, p := range slices.Sorted(maps.Keys(s.over)) {
		content := s.over[p]
		if err := s.count(int64(len(content))); err != nil {
			return err
		}
		if err := s.file(p, content, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (s *stager) count(size int64) error {
	s.files++
	s.bytes += size
	if s.files > maxStageFiles || s.bytes > maxStageBytes {
		return fmt.Errorf("the checkout is over the stage bound (%d files, %d bytes)", maxStageFiles, maxStageBytes)
	}
	return nil
}

func (s *stager) header(h *tar.Header) error {
	if err := s.tw.WriteHeader(h); err != nil {
		return fmt.Errorf("write %s: %w", h.Name, err)
	}
	return nil
}

func (s *stager) file(p string, content []byte, mode int64) error {
	if err := s.header(&tar.Header{Typeflag: tar.TypeReg, Name: p, Mode: mode, Size: int64(len(content))}); err != nil {
		return err
	}
	if _, err := s.tw.Write(content); err != nil {
		return fmt.Errorf("write %s: %w", p, err)
	}
	return nil
}
