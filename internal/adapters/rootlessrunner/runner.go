// Package rootlessrunner implements the server-side Unix-socket client and the isolated runner execution policy.
// The runner process must execute under its dedicated Unix identity; only it invokes the rootless Podman CLI.
package rootlessrunner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const maxOutput = 5 << 20

// Executor starts the container runtime without inheriting the runner environment.
type Executor interface {
	Execute(context.Context, []string, string) ([]byte, error)
}

// Podman invokes the rootless Podman CLI available to the dedicated runner identity.
type Podman struct {
	Home       string
	RuntimeDir string
}

// Execute runs one bounded container job.
func (p Podman) Execute(ctx context.Context, args []string, dir string) ([]byte, error) {
	// #nosec G204 -- args come only from Policy.containerArgs; profile commands and image refs are fixed/validated.
	cmd := exec.CommandContext(ctx, "podman", args...)
	cmd.Dir = dir
	// A cancelled job (superseded or timed out) gets SIGTERM, which podman forwards to the container and cleans up
	// after; only a job that ignores it for ten seconds is killed.
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 10 * time.Second
	home := p.Home
	if home == "" {
		home = "/var/lib/heimdall-runner"
	}
	runtimeDir := p.RuntimeDir
	if runtimeDir == "" {
		runtimeDir = "/run/heimdall-podman"
	}
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + home, "XDG_RUNTIME_DIR=" + runtimeDir}
	var output boundedBuffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Run(); err != nil {
		return output.Bytes(), fmt.Errorf("podman job: %w", err)
	}
	return output.Bytes(), nil
}

// JobInput is one validated execution request inside the runner process.
type JobInput struct {
	Profile  string       // the named profile the job runs under
	Spec     *RepoProfile // when set, a one-job trial proposal the policy keeps out of the promoted set
	Archive  io.Reader
	Overlays []ports.File
	Dir      string // repository-relative directory the acceptance command's {dir} names
	// CheckID and TestKind select a row of the promoted profile; at most one is set. A job with neither runs the
	// profile's acceptance command (red-first, repro and trial jobs).
	CheckID  string
	TestKind string
	// Repo is the repository the job runs for; a check that reads the history mirror finds it by this name.
	Repo change.Repo
	// BaseArchive is the tree of the base commit, present at /base for a check or test job that compares trees.
	BaseArchive io.Reader
	Timeout     time.Duration
}

// Runner runs validated jobs using allowlisted images and holds the durable promoted profile set.
// Activate is the only mutation, so the promoted set a job consults and the files a restart reloads
// never disagree.
type Runner struct {
	Policy   Policy
	Executor Executor
	Root     string
	// Git runs the history mirror's git commands; nil means the real git binary.
	Git GitRunner
	// ServiceReady bounds the wait for a job's services to accept on their ports; zero means 30 seconds.
	ServiceReady time.Duration

	// profilesDir is the durable directory of promoted profiles; empty means no promotions are
	// persisted for this runner instance (unit tests).
	profilesDir string
	// allowlist is the image allowlist the runner validates every promotion against.
	allowlist []string

	mu       sync.RWMutex
	promoted map[string]RepoProfile
	// active is the successor of Policy.Profiles that Activate extends.
	active map[string]RepoProfile
}

// NewRunner loads the durable promoted profiles from profilesDir over the image allowlist and
// returns a runner ready to serve jobs or promotions.
func NewRunner(policy Policy, executor Executor, root, profilesDir string, allowlist []string) (Runner, error) {
	loaded, err := loadPromotedProfiles(profilesDir, allowlist)
	if err != nil {
		return Runner{}, err
	}
	machines := make(map[string]RepoProfile, len(loaded)+len(policy.Profiles))
	for name, spec := range policy.Profiles {
		if !validProfileName(name) {
			return Runner{}, fmt.Errorf("runner profile name %q must be a profile name", name)
		}
		if err := validateRunnerProfile(spec, allowlist); err != nil {
			return Runner{}, fmt.Errorf("runner profile %q: %w", name, err)
		}
		machines[name] = spec
	}
	for name, spec := range loaded {
		machines[name] = spec
	}
	return Runner{
		Policy: policy, Executor: executor, Root: root,
		profilesDir: profilesDir, allowlist: allowlist,
		promoted: loaded, active: machines,
	}, nil
}

func loadPromotedProfiles(dir string, allowlist []string) (map[string]RepoProfile, error) {
	if dir == "" {
		return nil, nil
	}
	profiles, err := LoadApprovedProfiles(dir, allowlist)
	if err != nil {
		return nil, err
	}
	return profiles, nil
}

func validateRunnerProfile(spec RepoProfile, allowlist []string) error {
	if !slices.Contains(allowlist, spec.Image) || !digestImage.MatchString(spec.Image) {
		return fmt.Errorf("%w: %q", ErrImageNotAllowed, spec.Image)
	}
	if spec.Acceptance == "" || spec.Seam == "" || spec.ACTag == "" {
		return errors.New("runner profile requires acceptance, seam and ac_tag")
	}
	return nil
}

// Activate validates one promoted profile artifact end to end, persists it durably for restarts,
// and extends the in-memory profile set after the write succeeded. Re-activating the same artifact
// is idempotent.
func (r *Runner) Activate(name string, artifact []byte) (RepoProfile, error) {
	if !validProfileName(name) {
		return RepoProfile{}, errors.New("profile name must be 1 to 64 lowercase name characters")
	}
	spec, err := ParseApprovedProfile(artifact, r.AllowedImages())
	if err != nil {
		return RepoProfile{}, fmt.Errorf("validate promoted profile %q: %w", name, err)
	}
	if err := r.persistProfile(name, artifact); err != nil {
		return RepoProfile{}, err
	}
	r.mu.Lock()
	r.promoted[name] = spec
	r.active[name] = spec
	r.mu.Unlock()
	return spec, nil
}

// persistProfile writes the artifact into the durable promoted directory atomically. Idempotent:
// an equal existing file stays as it is.
func (r *Runner) persistProfile(name string, artifact []byte) error {
	if r.profilesDir == "" {
		return nil
	}
	if err := os.MkdirAll(r.profilesDir, 0o700); err != nil {
		return fmt.Errorf("prepare promoted profile directory: %w", err)
	}
	final := filepath.Join(r.profilesDir, name+".yaml")
	existing, err := os.ReadFile(final) // #nosec G304 -- name is validated against ProfileName, dir is runner-owned
	if err == nil {
		if bytes.Equal(existing, artifact) {
			return nil
		}
		return fmt.Errorf("promoted profile %q differs from the stored copy", name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read stored promoted profile %q: %w", name, err)
	}
	tmp := filepath.Join(r.profilesDir, name+".yaml tmp")
	if err := os.WriteFile(tmp, artifact, 0o400); err != nil { // #nosec G304 -- see above
		return fmt.Errorf("write promoted profile %q: %w", name, err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("publish promoted profile %q: %w", name, err)
	}
	return nil
}

// Profile snapshot for the run path. The policy copy isolates the runner from map races.
func (r *Runner) effectivePolicy(in JobInput) (Policy, error) {
	if in.Spec != nil {
		spec := *in.Spec
		return r.Policy.WithProfile(in.Profile, spec)
	}
	if _, known := r.Policy.Profiles[in.Profile]; known {
		return r.Policy, nil
	}
	if _, builtIn := r.Policy.Images[in.Profile]; builtIn {
		return r.Policy, nil
	}
	if err := validateProfileName(in.Profile); err != nil {
		return Policy{}, err
	}
	r.mu.RLock()
	spec, promoted := r.active[in.Profile]
	r.mu.RUnlock()
	if !promoted {
		return Policy{}, ErrImageNotAllowed
	}
	p := r.Policy
	p.Profiles = map[string]RepoProfile{in.Profile: spec}
	return p, nil
}

func validateProfileName(name string) error {
	if !validProfileName(name) {
		return errors.New("profile name must be 1 to 64 lowercase name characters")
	}
	return nil
}

// Names returns every profile the runner can currently run, sorted.
func (r *Runner) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.active))
	for name := range r.active {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// AllowedImages returns the distinct image allowlist the runner validates proposals against: every digest-pinned
// reference it may run and every image name a profile may use for a check, a test kind or a service. A name resolves
// to its reference only here, at job time, so rotating an image's digest edits no repository profile.
func (r *Runner) AllowedImages() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	allowed := make(map[string]struct{}, len(r.allowlist)+2*len(r.Policy.Images)+len(r.active))
	for _, image := range r.allowlist {
		allowed[image] = struct{}{}
	}
	for name, image := range r.Policy.Images {
		allowed[name], allowed[image] = struct{}{}, struct{}{}
	}
	for _, spec := range r.active {
		if spec.Image != "" {
			allowed[spec.Image] = struct{}{}
		}
	}
	out := make([]string, 0, len(allowed))
	for image := range allowed {
		out = append(out, image)
	}
	slices.Sort(out)
	return out
}

// Run executes one validated isolated job and returns the raw test event output.
func (r *Runner) Run(ctx context.Context, in JobInput) ([]byte, error) {
	return r.runJob(ctx, in)
}

// RunOverlay stages validated test files with the source archive before one isolated run.
func (r *Runner) RunOverlay(ctx context.Context, profile string, archive io.Reader, overlays []ports.File, timeout time.Duration) ([]byte, error) {
	return r.runJob(ctx, JobInput{Profile: profile, Archive: archive, Overlays: overlays, Timeout: timeout})
}

// checkRunnable refuses a job the runner cannot or may not start.
func (r *Runner) checkRunnable(in JobInput) error {
	if r.Executor == nil {
		return errors.New("rootless runner executor is required")
	}
	if in.Timeout <= 0 || in.Timeout > 10*time.Minute {
		return errors.New("sandbox timeout must be between 1 second and 10 minutes")
	}
	return nil
}

func (r *Runner) runJob(ctx context.Context, in JobInput) ([]byte, error) {
	if err := r.checkRunnable(in); err != nil {
		return nil, err
	}
	policy, err := r.effectivePolicy(in)
	if err != nil {
		return nil, err
	}
	dir, err := r.createJobDir()
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	archivePath, baseArchive, overlayDir, err := stageInputs(dir, in)
	if err != nil {
		return nil, err
	}
	moduleCache := r.moduleCache()
	if err := os.MkdirAll(moduleCache, 0o750); err != nil {
		return nil, fmt.Errorf("prepare read-only module cache mount: %w", err)
	}
	args, err := containerArgsFor(policy, in, archivePath, baseArchive, overlayDir, moduleCache)
	if err != nil {
		return nil, err
	}
	services, err := policy.resolveServices(in.Profile, in)
	if err != nil {
		return nil, err
	}
	jobCtx, cancel := context.WithTimeout(ctx, in.Timeout+5*time.Second)
	defer cancel()
	args, stop, err := r.startServices(jobCtx, services, filepath.Base(dir), args, dir, in.Timeout)
	if err != nil {
		return nil, err
	}
	defer stop()
	output, runErr := r.Executor.Execute(jobCtx, args, dir)
	if runErr != nil && len(output) == 0 {
		return nil, runErr
	}
	return output, runErr
}

// stageInputs writes the head archive, the optional base archive and the overlays into the job directory.
func stageInputs(dir string, in JobInput) (archive, base, overlays string, err error) {
	if archive, err = storeArchive(dir, "source.tgz", in.Archive, maxSourceArchive); err != nil {
		return "", "", "", err
	}
	if in.BaseArchive != nil {
		if base, err = storeArchive(dir, "base.tgz", in.BaseArchive, maxBaseArchive); err != nil {
			return "", "", "", err
		}
	}
	if overlays, err = createOverlays(dir, in.Overlays); err != nil {
		return "", "", "", err
	}
	return archive, base, overlays, nil
}

func (r *Runner) moduleCache() string {
	if r.Policy.ModuleCache != "" {
		return r.Policy.ModuleCache
	}
	if r.Root != "" {
		return filepath.Join(r.Root, "module-cache")
	}
	return r.Policy.moduleCache()
}

func (r *Runner) createJobDir() (string, error) {
	root := r.Root
	if root == "" {
		root = "/var/lib/heimdall-runner/jobs"
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", fmt.Errorf("create runner state directory: %w", err)
	}
	dir, err := os.MkdirTemp(root, "job-")
	if err != nil {
		return "", fmt.Errorf("create runner job directory: %w", err)
	}
	return dir, nil
}

// Archive bounds: the head tree and the base tree each have their own cap.
const (
	maxSourceArchive = 64 << 20
	maxBaseArchive   = 32 << 20
)

func storeArchive(dir, name string, archive io.Reader, limit int64) (string, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", fmt.Errorf("open runner job directory: %w", err)
	}
	defer func() { _ = root.Close() }()
	f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o444)
	if err != nil {
		return "", fmt.Errorf("create %s: %w", name, err)
	}
	written, copyErr := io.Copy(f, io.LimitReader(archive, limit+1))
	closeErr := f.Close()
	if copyErr != nil {
		return "", fmt.Errorf("store %s: %w", name, copyErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close %s: %w", name, closeErr)
	}
	if written > limit {
		return "", fmt.Errorf("%s exceeds %d MiB", name, limit>>20)
	}
	return filepath.Join(dir, name), nil
}

func createOverlays(dir string, overlays []ports.File) (string, error) {
	if len(overlays) == 0 {
		return "", nil
	}
	overlayDir := filepath.Join(dir, "overlays")
	if err := os.Mkdir(overlayDir, 0o750); err != nil {
		return "", fmt.Errorf("create overlay directory: %w", err)
	}
	if err := stageOverlays(overlayDir, overlays); err != nil {
		return "", err
	}
	return overlayDir, nil
}

func stageOverlays(root string, overlays []ports.File) error {
	for _, overlay := range overlays {
		if err := stageOverlay(root, overlay); err != nil {
			return err
		}
	}
	return nil
}

func stageOverlay(root string, overlay ports.File) error {
	if err := overlay.Validate(); err != nil {
		return fmt.Errorf("invalid test overlay: %w", err)
	}
	name, err := cleanOverlayName(overlay.Path)
	if err != nil {
		return err
	}
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		return fmt.Errorf("open test overlay directory: %w", err)
	}
	defer func() { _ = rootFS.Close() }()
	if err := rootFS.MkdirAll(filepath.Dir(name), 0o750); err != nil {
		return fmt.Errorf("create overlay parent: %w", err)
	}
	file, err := rootFS.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o444)
	if err != nil {
		return fmt.Errorf("create overlay file: %w", err)
	}
	_, writeErr := file.Write(overlay.Content)
	closeErr := file.Close()
	if writeErr != nil {
		return fmt.Errorf("write overlay file: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close overlay file: %w", closeErr)
	}
	return nil
}

func cleanOverlayName(value string) (string, error) {
	name := filepath.FromSlash(value)
	if path.Clean(value) != value || strings.HasPrefix(value, "../") || filepath.IsAbs(name) || strings.Contains(value, "\\") {
		return "", errors.New("test overlay path escapes overlay directory")
	}
	return name, nil
}

type boundedBuffer struct{ bytes.Buffer }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	written := len(p)
	remaining := maxOutput - b.Len()
	if remaining <= 0 {
		return written, nil
	}
	if len(p) > remaining {
		p = p[:remaining]
	}
	n, err := b.Buffer.Write(p)
	if err != nil {
		return n, err
	}
	return written, nil
}

// containerArgsFor returns the Podman arguments of one job: a check or test-kind job from the promoted profile, or the
// profile's acceptance command otherwise.
func containerArgsFor(policy Policy, in JobInput, archivePath, baseArchive, overlayDir, moduleCache string) ([]string, error) {
	if in.CheckID != "" || in.TestKind != "" {
		spec, err := policy.resolveJob(in.Profile, in.CheckID, in.TestKind, in.Dir)
		if err != nil {
			return nil, err
		}
		spec.Archive, spec.BaseArchive, spec.OverlayDir = archivePath, baseArchive, overlayDir
		spec.ModuleCache, spec.Timeout = moduleCache, int(in.Timeout.Seconds())
		if spec.History {
			if spec.HistoryDir, err = policy.historyMount(in.Repo); err != nil {
				return nil, err
			}
		}
		return policy.profileJobArgs(spec)
	}
	return policy.ContainerArgsWithOverlays(in.Profile, archivePath, overlayDir, moduleCache, in.Dir, int(in.Timeout.Seconds()))
}
