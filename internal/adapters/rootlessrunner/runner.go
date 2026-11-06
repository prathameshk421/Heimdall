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
	"time"

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
	Timeout  time.Duration
}

// Runner runs validated jobs using allowlisted images and holds the durable promoted profile set.
// Activate is the only mutation, so the promoted set a job consults and the files a restart reloads
// never disagree.
type Runner struct {
	Policy   Policy
	Executor Executor
	Root     string

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
	spec, err := ParseApprovedProfile(artifact, r.imageAllowlist())
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

func (r *Runner) imageAllowlist() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	allowed := make(map[string]struct{}, len(r.allowlist)+len(r.Policy.Images)+len(r.active))
	for _, image := range r.allowlist {
		allowed[image] = struct{}{}
	}
	for _, image := range r.Policy.Images {
		allowed[image] = struct{}{}
	}
	for name, image := range r.active {
		if name != "" && image.Image != "" {
			allowed[image.Image] = struct{}{}
		}
	}
	out := make([]string, 0, len(allowed))
	for image := range allowed {
		out = append(out, image)
	}
	return out
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

// AllowedImages returns the distinct image allowlist the runner validates proposals against.
func (r *Runner) AllowedImages() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	allowed := make(map[string]struct{}, len(r.allowlist)+len(r.Policy.Images)+len(r.active))
	for _, image := range r.allowlist {
		allowed[image] = struct{}{}
	}
	for _, image := range r.Policy.Images {
		allowed[image] = struct{}{}
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

func (r *Runner) runJob(ctx context.Context, in JobInput) ([]byte, error) {
	if r.Executor == nil {
		return nil, errors.New("rootless runner executor is required")
	}
	if in.Timeout <= 0 || in.Timeout > 10*time.Minute {
		return nil, errors.New("sandbox timeout must be between 1 second and 10 minutes")
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
	archivePath, err := storeArchive(dir, in.Archive)
	if err != nil {
		return nil, err
	}
	overlayDir, err := createOverlays(dir, in.Overlays)
	if err != nil {
		return nil, err
	}
	moduleCache := r.moduleCache()
	if err := os.MkdirAll(moduleCache, 0o750); err != nil {
		return nil, fmt.Errorf("prepare read-only module cache mount: %w", err)
	}
	args, err := policy.ContainerArgsWithOverlays(in.Profile, archivePath, overlayDir, moduleCache, in.Dir, int(in.Timeout.Seconds()))
	if err != nil {
		return nil, err
	}
	jobCtx, cancel := context.WithTimeout(ctx, in.Timeout+5*time.Second)
	defer cancel()
	output, runErr := r.Executor.Execute(jobCtx, args, dir)
	if runErr != nil && len(output) == 0 {
		return nil, runErr
	}
	return output, runErr
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

func storeArchive(dir string, archive io.Reader) (string, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", fmt.Errorf("open runner job directory: %w", err)
	}
	defer func() { _ = root.Close() }()
	f, err := root.OpenFile("source.tgz", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o444)
	if err != nil {
		return "", fmt.Errorf("create source archive: %w", err)
	}
	written, copyErr := io.Copy(f, io.LimitReader(archive, (64<<20)+1))
	closeErr := f.Close()
	if copyErr != nil {
		return "", fmt.Errorf("store source archive: %w", copyErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close source archive: %w", closeErr)
	}
	if written > 64<<20 {
		return "", errors.New("source archive exceeds 64 MiB")
	}
	return filepath.Join(dir, "source.tgz"), nil
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
