package localrunner

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/verify"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Sentinel errors returned by the runner.
var (
	ErrPathTraversal  = errors.New("path traversal detected in tarball")
	ErrAbsolutePath   = errors.New("absolute path rejected in tarball")
	ErrSymlinkEscape  = errors.New("symlink target escapes sandbox directory")
	ErrDeviceFile     = errors.New("unsupported device or special file in tarball")
	ErrTarTooLarge    = errors.New("tarball exceeds maximum size limit of 64 MiB")
	ErrTooManyFiles   = errors.New("tarball exceeds maximum file count limit of 20000")
	ErrInvalidCommand = errors.New("disallowed command: only go test -json -count=1 ./... is permitted")
	ErrTimeout        = errors.New("test execution timed out")
)

// Limits for the runner.
const (
	MaxTarBytes    = 64 << 20 // 64 MiB
	MaxTarFiles    = 20000
	MaxOutputBytes = 4 << 20 // 4 MiB
)

// Source fetches source code tarballs. It is satisfied by ports.CodeReader.
type Source interface {
	Tarball(ctx context.Context, repo change.Repo, sha id.SHA) (io.ReadCloser, error)
}

// Option configures a Runner.
type Option func(*Runner)

// WithBlobs configures the blob store.
func WithBlobs(b ports.BlobStore) Option {
	return func(r *Runner) {
		r.blobs = b
	}
}

// WithClock configures the clock.
func WithClock(c ports.Clock) Option {
	return func(r *Runner) {
		r.clock = c
	}
}

// WithPath configures the PATH environment variable used by runner.
func WithPath(p string) Option {
	return func(r *Runner) {
		r.path = p
	}
}

// WithGoCache sets the Go build cache directory used by every run. Without it the cache would live in the temporary
// HOME and be empty each time, so every run would compile the standard library again. The cache is content-addressed,
// so sharing it between runs is safe. The default is a directory under the user cache directory.
func WithGoCache(dir string) Option {
	return func(r *Runner) {
		r.cacheDir = dir
	}
}

// WithTempDirHook registers a hook called with the temporary directory path.
func WithTempDirHook(fn func(string)) Option {
	return func(r *Runner) {
		r.onTempDir = fn
	}
}

// Job extends ports.SandboxJob with local runner specifics.
type Job struct {
	ports.SandboxJob
	Overlay []ports.File
	Command []string
	GOFlags string
	GOPROXY string
}

// Runner implements ports.Sandbox.
type Runner struct {
	source    Source
	blobs     ports.BlobStore
	clock     ports.Clock
	path      string
	cacheDir  string
	onTempDir func(string)
}

var _ ports.Sandbox = (*Runner)(nil)

// New builds a new Runner.
func New(src Source, opts ...Option) *Runner {
	r := &Runner{
		source: src,
	}
	for _, opt := range opts {
		opt(r)
	}
	if r.cacheDir == "" {
		if base, err := os.UserCacheDir(); err == nil {
			r.cacheDir = filepath.Join(base, "heimdall-runner", "go-build")
		}
	}
	return r
}

// Run executes a job and returns the SandboxResult.
func (r *Runner) Run(ctx context.Context, j ports.SandboxJob) (ports.SandboxResult, error) {
	res, _, err := r.RunWithLog(ctx, j)
	return res, err
}

// RunWithLog executes a job and returns the SandboxResult and raw log bytes.
func (r *Runner) RunWithLog(ctx context.Context, j ports.SandboxJob) (ports.SandboxResult, []byte, error) {
	return r.RunJobWithLog(ctx, Job{SandboxJob: j})
}

// RunJob executes an extended Job.
func (r *Runner) RunJob(ctx context.Context, j Job) (ports.SandboxResult, error) {
	res, _, err := r.RunJobWithLog(ctx, j)
	return res, err
}

// RunJobWithLog executes an extended Job and returns raw log bytes.
func (r *Runner) RunJobWithLog(ctx context.Context, j Job) (ports.SandboxResult, []byte, error) {
	if err := r.validateJob(j); err != nil {
		return ports.SandboxResult{}, nil, err
	}

	dir, err := os.MkdirTemp("", "heimdall-runner-*")
	if err != nil {
		return ports.SandboxResult{}, nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(dir)
	}()

	if r.onTempDir != nil {
		r.onTempDir(dir)
	}

	if err := r.prepareSourceAndOverlays(ctx, j, dir); err != nil {
		return ports.SandboxResult{}, nil, err
	}

	return r.executeAndReport(ctx, j, dir)
}

func (r *Runner) validateJob(j Job) error {
	if err := j.Validate(); err != nil {
		return fmt.Errorf("invalid sandbox job: %w", err)
	}
	switch j.Kind {
	case ports.SandboxRedFirst, ports.SandboxRepro:
	case ports.SandboxStatic, ports.SandboxContract, ports.SandboxMutation:
		return fmt.Errorf("unsupported sandbox kind %q: %w", j.Kind, ErrInvalidCommand)
	default:
		return fmt.Errorf("unsupported sandbox kind %q: %w", j.Kind, ErrInvalidCommand)
	}
	return validateCommand(j.Command)
}

func validateCommand(cmd []string) error {
	if len(cmd) == 0 {
		return nil
	}
	expected := []string{"go", "test", "-json", "-count=1", "./..."}
	if len(cmd) != len(expected) {
		return ErrInvalidCommand
	}
	for i := range expected {
		if cmd[i] != expected[i] {
			return ErrInvalidCommand
		}
	}
	return nil
}

func (r *Runner) prepareSourceAndOverlays(ctx context.Context, j Job, dir string) error {
	rc, err := r.source.Tarball(ctx, j.Repo, j.SHA)
	if err != nil {
		return fmt.Errorf("fetch tarball: %w", err)
	}
	defer func() {
		_ = rc.Close()
	}()

	if err := extractTar(rc, dir); err != nil {
		return fmt.Errorf("unpack tarball: %w", err)
	}

	if err := applyOverlays(dir, j.Overlay); err != nil {
		return fmt.Errorf("apply overlays: %w", err)
	}
	return nil
}

func (r *Runner) executeAndReport(ctx context.Context, j Job, dir string) (ports.SandboxResult, []byte, error) {
	env := r.buildEnv(dir, j)
	rawLog, timedOut, _ := executeGoTest(ctx, dir, env, j.Timeout)

	rep, _, parseErr := ParseEvents(bytes.NewReader(rawLog))
	if parseErr != nil && len(rawLog) == 0 {
		return ports.SandboxResult{}, nil, fmt.Errorf("parse test events: %w", parseErr)
	}

	if timedOut && len(rep.Cases()) == 0 {
		rep.Suites = append(rep.Suites, verify.Suite{
			Name: "timeout",
			Cases: []verify.Case{
				{
					Suite:     "timeout",
					ClassName: "timeout",
					Name:      "[timeout]",
					Status:    verify.StatusError,
					Message:   "timeout",
					Text:      "timeout",
				},
			},
		})
	}

	logRef, err := r.resolveLogRef(ctx, rawLog)
	if err != nil {
		return ports.SandboxResult{}, nil, err
	}

	res := ReportToSandboxResult(rep, logRef)
	return res, rawLog, nil
}

func (r *Runner) resolveLogRef(ctx context.Context, rawLog []byte) (workflow.BlobRef, error) {
	if r.blobs != nil {
		ref, err := r.blobs.Put(ctx, ports.BlobLog, bytes.NewReader(rawLog))
		if err != nil {
			return "", fmt.Errorf("store log blob: %w", err)
		}
		return ref, nil
	}
	hash := sha256.Sum256(rawLog)
	return workflow.BlobRef(fmt.Sprintf("blob:sha256:%x", hash)), nil
}

func (r *Runner) buildEnv(destDir string, j Job) []string {
	pathStr := r.path
	if pathStr == "" {
		pathStr = config.Getenv("PATH")
	}
	if pathStr == "" {
		pathStr = "/usr/local/bin:/usr/bin:/bin:/usr/local/go/bin:/opt/homebrew/bin"
	}

	env := []string{
		"PATH=" + pathStr,
		"HOME=" + destDir,
		"CGO_ENABLED=0",
	}
	if r.cacheDir != "" {
		env = append(env, "GOCACHE="+r.cacheDir)
	}
	if j.GOFlags != "" {
		env = append(env, "GOFLAGS="+j.GOFlags)
	}
	if j.GOPROXY != "" {
		env = append(env, "GOPROXY="+j.GOPROXY)
	} else {
		env = append(env, "GOPROXY=off")
	}
	return env
}

func executeGoTest(ctx context.Context, destDir string, env []string, timeout time.Duration) ([]byte, bool, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "go", "test", "-json", "-count=1", "./...")
	cmd.Dir = destDir
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil && cmd.Process.Pid > 0 {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	cmd.WaitDelay = 2 * time.Second

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &limitWriter{w: &stdoutBuf, limit: MaxOutputBytes}
	cmd.Stderr = &limitWriter{w: &stderrBuf, limit: MaxOutputBytes}

	err := cmd.Run()
	timedOut := cmdCtx.Err() != nil

	combined := append(stdoutBuf.Bytes(), stderrBuf.Bytes()...)
	return combined, timedOut, err
}

type limitWriter struct {
	w     io.Writer
	limit int64
	n     int64
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if l.n >= l.limit {
		return len(p), nil
	}
	remaining := l.limit - l.n
	toWrite := p
	if int64(len(toWrite)) > remaining {
		toWrite = toWrite[:remaining]
	}
	n, err := l.w.Write(toWrite)
	l.n += int64(n)
	return len(p), err
}

func extractTar(r io.Reader, destDir string) error {
	tr := tar.NewReader(io.LimitReader(r, MaxTarBytes+1))
	var totalBytes int64
	var fileCount int

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}
		fileCount++
		if fileCount > MaxTarFiles {
			return ErrTooManyFiles
		}
		if err := validateAndExtractEntry(tr, hdr, destDir, &totalBytes); err != nil {
			return err
		}
	}
	return nil
}

func validateAndExtractEntry(tr *tar.Reader, hdr *tar.Header, destDir string, totalBytes *int64) error {
	cleanName := path.Clean(hdr.Name)
	if cleanName == "." {
		return nil
	}
	if strings.HasPrefix(cleanName, "../") || cleanName == ".." || strings.Contains(cleanName, "/../") {
		return ErrPathTraversal
	}
	if path.IsAbs(cleanName) || strings.HasPrefix(hdr.Name, "/") || filepath.IsAbs(hdr.Name) {
		return ErrAbsolutePath
	}
	cleanDest := filepath.Clean(destDir) + string(filepath.Separator)
	target := filepath.Clean(filepath.Join(destDir, filepath.FromSlash(cleanName)))
	if !strings.HasPrefix(target, cleanDest) {
		return ErrPathTraversal
	}

	return extractByType(tr, hdr, target, destDir, totalBytes)
}

func extractByType(tr *tar.Reader, hdr *tar.Header, target, destDir string, totalBytes *int64) error {
	switch hdr.Typeflag {
	case tar.TypeDir:
		return os.MkdirAll(target, 0o750)
	case tar.TypeReg:
		return extractRegularFile(tr, hdr, target, totalBytes)
	case tar.TypeSymlink:
		return extractSymlink(hdr, target, destDir)
	default:
		return fmt.Errorf("%w: entry %q type flag %c", ErrDeviceFile, hdr.Name, hdr.Typeflag)
	}
}

func extractRegularFile(tr *tar.Reader, hdr *tar.Header, target string, totalBytes *int64) error {
	if hdr.Size < 0 || *totalBytes+hdr.Size > MaxTarBytes {
		return ErrTarTooLarge
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Clean(target), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
	}()

	written, err := io.Copy(f, io.LimitReader(tr, MaxTarBytes-*totalBytes+1))
	if err != nil {
		return err
	}
	*totalBytes += written
	if *totalBytes > MaxTarBytes {
		return ErrTarTooLarge
	}
	return nil
}

func extractSymlink(hdr *tar.Header, target, destDir string) error {
	linkTarget := filepath.Clean(hdr.Linkname)
	if filepath.IsAbs(linkTarget) || strings.HasPrefix(linkTarget, "/") {
		return ErrSymlinkEscape
	}
	parentDir := filepath.Dir(target)
	cleanDest := filepath.Clean(destDir) + string(filepath.Separator)
	relToDest, err := filepath.Rel(destDir, parentDir)
	if err != nil || strings.HasPrefix(relToDest, "..") {
		return ErrSymlinkEscape
	}
	resolved := filepath.Join(destDir, filepath.Clean(relToDest), linkTarget)
	if !strings.HasPrefix(resolved, cleanDest) {
		return ErrSymlinkEscape
	}
	if err := os.MkdirAll(parentDir, 0o750); err != nil {
		return err
	}
	return os.Symlink(linkTarget, target)
}

func applyOverlays(destDir string, overlays []ports.File) error {
	for _, f := range overlays {
		if err := applyOneOverlay(destDir, f); err != nil {
			return err
		}
	}
	return nil
}

func applyOneOverlay(destDir string, f ports.File) error {
	target, err := validateOverlayPath(destDir, f.Path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return err
	}
	return os.WriteFile(target, f.Content, 0o600)
}

func validateOverlayPath(destDir, rawPath string) (string, error) {
	clean := path.Clean(rawPath)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", ErrPathTraversal
	}
	if path.IsAbs(clean) || strings.HasPrefix(rawPath, "/") || filepath.IsAbs(rawPath) {
		return "", ErrAbsolutePath
	}
	cleanDest := filepath.Clean(destDir) + string(filepath.Separator)
	target := filepath.Clean(filepath.Join(destDir, filepath.FromSlash(clean)))
	if !strings.HasPrefix(target, cleanDest) {
		return "", ErrPathTraversal
	}
	return target, nil
}
