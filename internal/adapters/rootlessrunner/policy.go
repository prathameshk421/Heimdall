// Package rootlessrunner implements the server-side Unix-socket client and the isolated runner execution policy.
// The runner process must execute under its dedicated Unix identity; only it invokes the rootless Podman CLI.
package rootlessrunner

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/profileyaml"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

var digestImage = regexp.MustCompile(`^[A-Za-z0-9._:/-]+@sha256:[a-f0-9]{64}$`)

// Policy maps a human-reviewed profile name to an immutable image reference.
type Policy struct {
	Images      map[string]string
	Profiles    map[string]RepoProfile
	ModuleCache string
}

// ContainerArgs returns fixed, restrictive Podman arguments for a single Go test job.
// The source archive is mounted read-only and extracted into ephemeral /work inside the container.
func (p Policy) ContainerArgs(profile, sourceArchive string, timeoutSeconds int) ([]string, error) {
	return p.containerArgs(profile, sourceArchive, "", p.moduleCache(), "", timeoutSeconds)
}

// ContainerArgsWithOverlays adds a read-only, runner-staged overlay directory to the isolated job.
// dir is the repository-relative directory the acceptance command's {dir} names; empty means the
// profile's own tests directory.
func (p Policy) ContainerArgsWithOverlays(
	profile, sourceArchive, overlayDir, moduleCache, dir string, timeoutSeconds int,
) ([]string, error) {
	if overlayDir != "" {
		if err := absoluteCleanPath(overlayDir); err != nil {
			return nil, fmt.Errorf("overlay path: %w", err)
		}
	}
	if moduleCache == "" {
		moduleCache = p.moduleCache()
	}
	if err := absoluteCleanPath(moduleCache); err != nil {
		return nil, fmt.Errorf("module cache path: %w", err)
	}
	return p.containerArgs(profile, sourceArchive, overlayDir, moduleCache, dir, timeoutSeconds)
}

// WithProfile returns a one-profile Policy for a single trial run. The trial profile never joins the
// runner's promoted set, so a failed or unapproved artifact cannot be picked up by any other job.
func (p Policy) WithProfile(name string, spec RepoProfile) (Policy, error) {
	if !profileyaml.ProfileName.MatchString(name) {
		return Policy{}, errors.New("profile name must be 1 to 64 lowercase name characters")
	}
	profiles := map[string]RepoProfile{name: spec}
	return Policy{Images: p.Images, Profiles: profiles, ModuleCache: p.ModuleCache}, nil
}

// checkMarker ends the output of a static check; the exit status of the tool follows it.
const checkMarker = "heimdall-check-exit="

// ContainerArgsCheck returns the Podman arguments for one fixed static check on the built-in Go profile. The command
// is chosen by the runner from the check name, never from the request.
func (p Policy) ContainerArgsCheck(check, sourceArchive, moduleCache string, timeoutSeconds int) ([]string, error) {
	command, ok := checkCommand(check)
	if !ok {
		return nil, ErrUnknownCheck
	}
	if moduleCache == "" {
		moduleCache = p.moduleCache()
	}
	if err := absoluteCleanPath(moduleCache); err != nil {
		return nil, fmt.Errorf("module cache path: %w", err)
	}
	return p.containerArgsWith("go", sourceArchive, "", moduleCache, "", timeoutSeconds, command)
}

// checkCommand maps a check name to its fixed shell command. It prints the tool's exit status after checkMarker.
func checkCommand(check string) (string, bool) {
	if check == ports.CheckGoVet {
		return "go vet ./... 2>&1; echo " + checkMarker + "$?", true
	}
	return "", false
}

func (p Policy) containerArgs(profile, sourceArchive, overlayDir, moduleCache, dir string, timeoutSeconds int) ([]string, error) {
	return p.containerArgsWith(profile, sourceArchive, overlayDir, moduleCache, dir, timeoutSeconds, "")
}

func (p Policy) containerArgsWith(profile, sourceArchive, overlayDir, moduleCache, dir string, timeoutSeconds int, commandOverride string) ([]string, error) {
	image, ok := p.Images[profile]
	testCommand := "go test -json -count=1 ./..."
	if approved, exists := p.Profiles[profile]; exists {
		image, ok = approved.Image, true
		subst := dir
		if subst == "" {
			subst = approved.Paths.Tests
		}
		rel, err := cleanRelDir(subst)
		if err != nil {
			return nil, err
		}
		testCommand = strings.ReplaceAll(approved.Acceptance, "{dir}", "./"+rel)
	}
	if commandOverride != "" {
		testCommand = commandOverride
	}
	if !ok || !digestImage.MatchString(image) {
		return nil, ErrImageNotAllowed
	}
	if err := absoluteCleanPath(sourceArchive); err != nil {
		return nil, fmt.Errorf("source archive path: %w", err)
	}
	if timeoutSeconds < 1 || timeoutSeconds > 600 {
		return nil, errors.New("sandbox timeout must be between 1 and 600 seconds")
	}
	// A GitHub tarball unpacks into a single owner-repo-sha directory, while a flat archive leaves
	// the module at /work/src. The job enters the module root before it copies repository-relative
	// overlays or runs the acceptance command.
	command := "mkdir -p /work/src /work/.gocache /work/.gotmp" +
		" && tar -xzf /input/source.tgz -C /work/src --no-same-owner --no-same-permissions" +
		" && " + sourceRootScript("/work/src")
	args := []string{
		"run", "--rm", "--pull=never", "--network=none", "--read-only", "--user=65532:65532",
		"--group-add=keep-groups",
		"--cap-drop=ALL", "--security-opt=no-new-privileges",
		"--pids-limit=512", "--memory=2g", "--cpus=2", "--timeout=" + strconv.Itoa(timeoutSeconds),
		"--tmpfs", "/work:rw,nosuid,nodev,size=512m,mode=1777", "--tmpfs", "/tmp:rw,nosuid,nodev,size=64m,mode=1777",
		"--tmpfs", "/out:rw,noexec,nosuid,nodev,size=8m,mode=1777",
		"--mount", "type=bind,src=" + sourceArchive + ",dst=/input/source.tgz,ro=true",
		"--mount", "type=bind,src=" + moduleCache + ",dst=/go/pkg/mod,ro=true",
	}
	if overlayDir != "" {
		args = append(args, "--mount", "type=bind,src="+overlayDir+",dst=/input/overlays,ro=true")
		command += ` && cp -a /input/overlays/. "$root/"`
	}
	// The build cache and Go's temporary work directory go on the work tmpfs: a cold build writes
	// hundreds of megabytes, while /tmp is deliberately small.
	args = append(args, image, "/bin/sh", "-c",
		"export GOCACHE=/work/.gocache GOTMPDIR=/work/.gotmp GOMODCACHE=/go/pkg/mod GOPROXY=off && "+
			command+` && cd "$root" && `+testCommand)
	return args, nil
}

// sourceRootScript sets $root to the extracted module root. It descends while the current root has
// no go.mod and exactly one child directory, which is the GitHub tarball layout; a flat archive or a
// deeper module layout keeps $root where it is. The depth bound keeps a hostile nested archive from
// turning this into an unbounded walk.
func sourceRootScript(root string) string {
	return `{ root=` + shellQuote(root) + `; for _ in 1 2 3 4; do if [ -f "$root/go.mod" ]; then break; fi; ` +
		`set -- "$root"/*/; if [ "$#" -ne 1 ] || [ ! -d "$1" ]; then break; fi; root=${1%/}; done; }`
}

// shellQuote renders a value as one single-quoted shell word.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// cleanRelDir validates the repository-relative directory an acceptance command names.
func cleanRelDir(value string) (string, error) {
	value = path.Clean(value)
	if value == "." || path.IsAbs(value) || strings.HasPrefix(value, "../") || strings.ContainsAny(value, "\x00,\\") {
		return "", errors.New("job directory escape is refused")
	}
	return value, nil
}

func (p Policy) moduleCache() string {
	if p.ModuleCache != "" {
		return p.ModuleCache
	}
	return "/var/lib/heimdall-runner/module-cache"
}

func absoluteCleanPath(value string) error {
	if value == "" || !filepath.IsAbs(value) || filepath.Clean(value) != value || strings.ContainsAny(value, "\x00,\\") {
		return errors.New("must be an absolute, clean filesystem path")
	}
	return nil
}
