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
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/bench"
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

var (
	_ action.OverlaySandbox = (*sandboxBridge)(nil)
	_ action.CommandSandbox = (*sandboxBridge)(nil)
)

// repoStack identifies the build/test toolchain of a staged repository or test file.
type repoStack string

const (
	stackGo      repoStack = "go"
	stackNode    repoStack = "node"
	stackPython  repoStack = "python"
	stackRust    repoStack = "rust"
	stackUnknown repoStack = "unknown"
)

// Run runs a job with no overlay.
func (b *sandboxBridge) Run(ctx context.Context, j ports.SandboxJob) (ports.SandboxResult, error) {
	return b.RunOverlay(ctx, j, nil)
}

// RunOverlay stages the head tree with the overlays and runs the tests of the detected stack in PhaseExec.
func (b *sandboxBridge) RunOverlay(ctx context.Context, j ports.SandboxJob, overlays []ports.File) (ports.SandboxResult, error) {
	if err := b.checkSHA(j.SHA); err != nil {
		return ports.SandboxResult{}, err
	}
	if j.Timeout <= 0 {
		return ports.SandboxResult{}, errors.New("sandbox: the job has no timeout")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.stage(ctx, overlays); err != nil {
		return ports.SandboxResult{}, err
	}
	testPath := overlayTestPath(overlays)
	stack := detectStack(b.checkout, testPath)
	if err := b.fetchOnce(ctx, stack, j.Dir); err != nil {
		return ports.SandboxResult{}, err
	}
	return b.execTest(ctx, stack, j, testPath)
}

// RunCommand stages the head tree with optional overlays and runs an arbitrary exploratory command inside PhaseExec
// (no network, read-only rootfs, CPU/RAM/tasks/time limits).
func (b *sandboxBridge) RunCommand(ctx context.Context, sha id.SHA, dir string, argv []string, overlays []ports.File, timeout time.Duration) (action.CommandResult, error) {
	if err := b.checkSHA(sha); err != nil {
		return action.CommandResult{}, err
	}
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return action.CommandResult{}, errors.New("sandbox: command argv is empty")
	}
	if timeout <= 0 {
		return action.CommandResult{}, errors.New("sandbox: command timeout must be positive")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.stage(ctx, overlays); err != nil {
		return action.CommandResult{}, err
	}
	stack := detectStack(b.checkout, "")
	// Best-effort dependency warm for known stacks so builds/tests work offline inside PhaseExec.
	if stack != stackUnknown {
		_ = b.fetchOnce(ctx, stack, dir)
	}
	res, err := b.sbx.Exec(ctx, uidsandbox.Job{
		Argv: argv, Dir: dir, Timeout: timeout, Result: uidsandbox.ResultNone,
	})
	if err != nil {
		return action.CommandResult{}, fmt.Errorf("sandbox: run %s in %s: %w", argv[0], dir, err)
	}
	return action.CommandResult{Exit: res.Exit, TimedOut: res.TimedOut, Output: outputTail(res)}, nil
}

func (b *sandboxBridge) checkSHA(sha id.SHA) error {
	if sha != b.head && sha != b.base {
		return fmt.Errorf("sandbox: commit %s is neither the head nor the merge base of this run", sha)
	}
	return nil
}

func (b *sandboxBridge) stage(ctx context.Context, overlays []ports.File) error {
	pr, pw := io.Pipe()
	writeErr := make(chan error, 1)
	go func() {
		err := writeStageTree(pw, b.checkout, overlays)
		_ = pw.CloseWithError(err)
		writeErr <- err
	}()
	stageErr := b.sbx.Stage(ctx, pr, 0)
	_ = pr.Close()
	if werr := <-writeErr; werr != nil {
		return fmt.Errorf("sandbox: build the tree: %w", werr)
	}
	if stageErr != nil {
		return fmt.Errorf("sandbox: stage the tree: %w", stageErr)
	}
	return nil
}

// overlayTestPath returns the last overlay path (which proof_candidates appends as the repro test file).
func overlayTestPath(overlays []ports.File) string {
	if len(overlays) == 0 {
		return ""
	}
	return overlays[len(overlays)-1].Path
}

// detectStack infers the toolchain from the repro test file extension first, then from repository manifests.
func detectStack(checkout, testPath string) repoStack {
	if s := stackFromPath(testPath); s != stackUnknown {
		return s
	}
	switch {
	case checkoutHas(checkout, "go.mod"):
		return stackGo
	case checkoutHas(checkout, "package.json"):
		return stackNode
	case checkoutHas(checkout, "Cargo.toml"):
		return stackRust
	case checkoutHas(checkout, "pyproject.toml") || checkoutHas(checkout, "requirements.txt") || checkoutHas(checkout, "setup.py"):
		return stackPython
	}
	return stackGo
}

func stackFromPath(p string) repoStack {
	if p == "" {
		return stackUnknown
	}
	base := path.Base(p)
	switch {
	case strings.HasSuffix(base, "_test.go"):
		return stackGo
	case strings.HasSuffix(base, ".py"):
		return stackPython
	case strings.HasSuffix(base, ".rs"):
		return stackRust
	case bench.VitestTestFile(p):
		return stackNode
	}
	return stackUnknown
}

func checkoutHas(checkout, name string) bool {
	root, err := os.OpenRoot(checkout)
	if err != nil {
		return false
	}
	defer func() { _ = root.Close() }()
	_, err = root.Stat(name)
	return err == nil
}

func (b *sandboxBridge) execTest(ctx context.Context, stack repoStack, j ports.SandboxJob, testPath string) (ports.SandboxResult, error) {
	switch stack {
	case stackNode:
		return b.execNodeTest(ctx, j, testPath)
	case stackPython:
		return b.execPythonTest(ctx, j, testPath)
	case stackRust:
		return b.execRustTest(ctx, j, testPath)
	case stackGo, stackUnknown:
		return b.execGoTest(ctx, j)
	}
	return b.execGoTest(ctx, j)
}

func (b *sandboxBridge) execGoTest(ctx context.Context, j ports.SandboxJob) (ports.SandboxResult, error) {
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

func (b *sandboxBridge) execNodeTest(ctx context.Context, j ports.SandboxJob, testPath string) (ports.SandboxResult, error) {
	const junitPath = ".heimdall-junit.xml"
	argv, resultFmt, resPath := nodeTestCommand(b.checkout, testPath, junitPath)
	res, err := b.sbx.Exec(ctx, uidsandbox.Job{
		Argv: argv, Dir: ".", Timeout: j.Timeout, Result: resultFmt, ResultPath: resPath,
	})
	if err != nil {
		return ports.SandboxResult{}, fmt.Errorf("sandbox: run node tests for %s: %w", testPath, err)
	}
	out := testevents.ReportToSandboxResult(res.Report, logRefOf(res))
	out.Exit, out.Output = res.Exit, outputTail(res)
	return out, nil
}

func nodeTestCommand(checkout, testPath, junitPath string) ([]string, uidsandbox.ResultFormat, string) {
	pkg := readCheckoutFile(checkout, "package.json")
	if strings.Contains(pkg, `"vitest"`) || checkoutHas(checkout, "vitest.config.ts") || checkoutHas(checkout, "vitest.config.js") {
		argv := []string{"npx", "--no-install", "vitest", "run", "--reporter=junit", "--outputFile=" + junitPath}
		if testPath != "" {
			argv = append(argv, testPath)
		}
		return argv, uidsandbox.ResultJUnit, junitPath
	}
	if checkoutHas(checkout, "bun.lock") || checkoutHas(checkout, "bun.lockb") {
		argv := []string{"bun", "test"}
		if testPath != "" {
			argv = append(argv, testPath)
		}
		return argv, uidsandbox.ResultNone, ""
	}
	argv := []string{"node", "--test"}
	if testPath != "" {
		argv = append(argv, testPath)
	}
	return argv, uidsandbox.ResultNone, ""
}

func (b *sandboxBridge) execPythonTest(ctx context.Context, j ports.SandboxJob, testPath string) (ports.SandboxResult, error) {
	const junitPath = ".heimdall-junit.xml"
	argv := []string{"pytest", "-q", "--junitxml=" + junitPath}
	if testPath != "" {
		argv = append(argv, testPath)
	}
	res, err := b.sbx.Exec(ctx, uidsandbox.Job{
		Argv: argv, Dir: ".", Timeout: j.Timeout, Result: uidsandbox.ResultJUnit, ResultPath: junitPath,
	})
	if err != nil {
		return ports.SandboxResult{}, fmt.Errorf("sandbox: run pytest for %s: %w", testPath, err)
	}
	out := testevents.ReportToSandboxResult(res.Report, logRefOf(res))
	out.Exit, out.Output = res.Exit, outputTail(res)
	return out, nil
}

func (b *sandboxBridge) execRustTest(ctx context.Context, j ports.SandboxJob, testPath string) (ports.SandboxResult, error) {
	argv := []string{"cargo", "test", "--offline"}
	if strings.HasPrefix(testPath, "tests/") && strings.HasSuffix(testPath, ".rs") {
		stem := strings.TrimSuffix(path.Base(testPath), ".rs")
		argv = append(argv, "--test", stem)
	}
	res, err := b.sbx.Exec(ctx, uidsandbox.Job{
		Argv: argv, Dir: ".", Timeout: j.Timeout, Result: uidsandbox.ResultNone,
	})
	if err != nil {
		return ports.SandboxResult{}, fmt.Errorf("sandbox: run cargo test: %w", err)
	}
	out := ports.SandboxResult{LogRef: logRefOf(res), Exit: res.Exit, Output: outputTail(res)}
	parsed, ok := parseLibtestResult(res.Stdout, res.Stderr)
	if !ok || parsed.BuildFailed {
		out.Errored = 1
		return out, nil
	}
	for _, name := range slices.Sorted(maps.Keys(parsed.Tests)) {
		switch parsed.Tests[name] {
		case bench.ResultPass:
			out.Passed++
			out.Tests = append(out.Tests, ports.TestResult{Name: name, Outcome: ports.OutcomePassed})
		case bench.ResultFail:
			out.Failed++
			out.Tests = append(out.Tests, ports.TestResult{Name: name, Outcome: ports.OutcomeFailure})
		case bench.ResultSkip:
			out.Tests = append(out.Tests, ports.TestResult{Name: name, Outcome: ports.OutcomeSkipped})
		}
	}
	return out, nil
}

func parseLibtestResult(stdout, stderr []byte) (bench.Run, bool) {
	parsed, err := bench.ParseLibtest(stdout, stderr)
	if err != nil {
		return bench.Run{}, false
	}
	return parsed, true
}

func readCheckoutFile(checkout, name string) string {
	root, err := os.OpenRoot(checkout)
	if err != nil {
		return ""
	}
	defer func() { _ = root.Close() }()
	b, err := root.ReadFile(name)
	if err != nil {
		return ""
	}
	return string(b)
}

// fetchOnce downloads dependencies once per run, in the fetch phase, where egress goes only through the proxy.
func (b *sandboxBridge) fetchOnce(ctx context.Context, stack repoStack, dir string) error {
	if b.fetched {
		return nil
	}
	job, ok := fetchJobFor(b.checkout, stack, dir)
	if !ok {
		b.fetched = true
		return nil
	}
	res, err := b.sbx.Fetch(ctx, job)
	if err != nil {
		return fmt.Errorf("sandbox: fetch dependencies: %w", err)
	}
	if res.Exit != 0 {
		return fmt.Errorf("sandbox: %s exited %d: %s", strings.Join(job.Argv, " "), res.Exit, clipText(strings.TrimSpace(string(res.Stderr)), 400))
	}
	b.fetched = true
	return nil
}

func fetchJobFor(checkout string, stack repoStack, dir string) (uidsandbox.Job, bool) {
	switch stack {
	case stackGo:
		return uidsandbox.Job{Argv: []string{"go", "mod", "download"}, Dir: dir,
			Env: []string{"GOTOOLCHAIN=local"}, Timeout: fetchTimeout}, true
	case stackNode:
		return nodeFetchJob(checkout)
	case stackPython:
		if checkoutHas(checkout, "requirements.txt") {
			return uidsandbox.Job{Argv: []string{"pip", "install", "--user", "-r", "requirements.txt"}, Dir: ".", Timeout: fetchTimeout}, true
		}
	case stackRust:
		return uidsandbox.Job{Argv: []string{"cargo", "fetch"}, Dir: ".", Timeout: fetchTimeout}, true
	case stackUnknown:
		return uidsandbox.Job{}, false
	}
	return uidsandbox.Job{}, false
}

func nodeFetchJob(checkout string) (uidsandbox.Job, bool) {
	switch {
	case checkoutHas(checkout, "pnpm-lock.yaml"):
		return uidsandbox.Job{Argv: []string{"pnpm", "install", "--frozen-lockfile"}, Dir: ".", Timeout: fetchTimeout}, true
	case checkoutHas(checkout, "bun.lock") || checkoutHas(checkout, "bun.lockb"):
		return uidsandbox.Job{Argv: []string{"bun", "install", "--frozen-lockfile"}, Dir: ".", Timeout: fetchTimeout}, true
	case checkoutHas(checkout, "yarn.lock"):
		return uidsandbox.Job{Argv: []string{"yarn", "install", "--frozen-lockfile"}, Dir: ".", Timeout: fetchTimeout}, true
	case checkoutHas(checkout, "package-lock.json"):
		return uidsandbox.Job{Argv: []string{"npm", "ci"}, Dir: ".", Timeout: fetchTimeout}, true
	case checkoutHas(checkout, "package.json"):
		return uidsandbox.Job{Argv: []string{"npm", "install"}, Dir: ".", Timeout: fetchTimeout}, true
	}
	return uidsandbox.Job{}, false
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
	var buf bytes.Buffer
	if err := writeStageTree(&buf, checkout, overlays); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeStageTree streams the tar archive of checkout + overlays directly to w so large repositories do not buffer up
// to 256 MiB in memory (SECURITY_AND_PERF_AUDIT.md PERF-02).
func writeStageTree(w io.Writer, checkout string, overlays []ports.File) error {
	over := make(map[string][]byte, len(overlays))
	for _, f := range overlays {
		if err := f.Validate(); err != nil {
			return fmt.Errorf("overlay %q: %w", clipText(f.Path, 80), err)
		}
		over[path.Clean(f.Path)] = f.Content
	}
	root, err := os.OpenRoot(checkout)
	if err != nil {
		return fmt.Errorf("open the checkout: %w", err)
	}
	defer func() { _ = root.Close() }()
	tw := tar.NewWriter(w)
	st := &stager{root: root, tw: tw, over: over}
	if err := fs.WalkDir(root.FS(), ".", st.visit); err != nil {
		return err
	}
	if err := st.writeOverlays(); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("close the tree: %w", err)
	}
	return nil
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
