// Package profileyaml parses and validates the reviewed repository profile artifact
// (`.heimdall/profile.yaml`, TECH_SPEC 12.2b) — the boundary between a proposed YAML text and a
// sandbox contract the runtime is allowed to execute.
//
// Purpose: one authority for every shape-check the artifact must pass before any component trusts
// it. The server's propose step uses the same Builder validation power as the runner's own trial and
// promotion paths do, so a proposal can never be more permissive on one side than on the other.
//
// Invariants: the artifact is one bounded (64 KiB) single YAML document with no unknown keys; the
// image is digest-pinned and on the caller's allowlist; acceptance carries exactly one {dir}; fetch
// hosts are exact DNS hostnames; tests_dir is a clean repository-relative path; profile names are
// one to 64 lowercase name characters. The echo fields setup, test, seam, ac_tag and languages are
// parsed and stored, but this build executes only the acceptance command: the registry refuses
// profile names the file cannot echo and leaves setup/test inert, so an artifact that appears to
// name extra commands never runs anything.
//
// The checks, tests.kinds, tests.runs and services sections follow the same rules. Their shape (a
// kind's run carries exactly one {dir}, a ready URL is loopback, globs stay inside the repository,
// every list is bounded) lives in ports.ProfileSpec.ValidateExtensions, called from here; this package
// adds the rule only it can apply: every image they name is on the allowlist the caller holds, either
// as a digest reference or as a bare name the runner resolves at job time. Unknown keys are refused at
// every level.
//
// Failure modes: every violation fails closed with an error naming the category, and no error says
// or ships any secret.
//
// Traps: the {dir} substitution is textual, so other braces and shell metacharacters in the
// acceptance command are executed by /bin/sh inside the still-isolated container. That is the
// reviewed artifact's privilege and it is why promotion needs a human approval and a real trial, not
// a text rule. Parsing here never runs a command.
package profileyaml

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"net/url"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// ErrImageNotAllowed reports a profile absent from the pinned image policy.
var ErrImageNotAllowed = errors.New("sandbox image is not allowlisted")

var digestImage = regexp.MustCompile(`^[A-Za-z0-9._:/-]+@sha256:[a-f0-9]{64}$`)

// RepoProfile is the reviewed, repository-owned sandbox contract. A profile is usable only after it
// has been loaded from the runner's promoted set or accepted as a trial proposal.
type RepoProfile struct {
	Languages  []string `yaml:"languages" json:"languages"`
	Image      string   `yaml:"image" json:"image"`
	Setup      string   `yaml:"setup" json:"setup,omitempty"`
	Test       string   `yaml:"test" json:"test,omitempty"`
	Acceptance string   `yaml:"acceptance" json:"acceptance"`
	Seam       string   `yaml:"seam" json:"seam"`
	ACTag      string   `yaml:"ac_tag" json:"ac_tag"`
	FetchHosts []string `yaml:"fetch_hosts,omitempty" json:"fetch_hosts,omitempty"`
	Paths      struct {
		Tests string `yaml:"tests" json:"tests"`
	} `yaml:"paths" json:"paths"`
	// Checks are the tool checks of REVIEW_ARCH 4.1; Tests holds the test kinds of TEST_AGENT_ARCH 5.1 and the
	// number of times a gate run repeats; Services are the throwaway containers a job may reach.
	Checks   []Check   `yaml:"checks,omitempty" json:"checks,omitempty"`
	Tests    Tests     `yaml:"tests,omitempty" json:"tests,omitempty"`
	Services []Service `yaml:"services,omitempty" json:"services,omitempty"`
}

// Check is one row of the profile's checks list.
type Check struct {
	ID       string   `yaml:"id" json:"id"`
	Lens     string   `yaml:"lens" json:"lens"`
	When     []string `yaml:"when" json:"when"`
	Image    string   `yaml:"image" json:"image"`
	Run      string   `yaml:"run" json:"run"`
	Baseline bool     `yaml:"baseline" json:"baseline"`
	History  bool     `yaml:"history,omitempty" json:"history,omitempty"`
}

// Tests is the profile's tests section.
type Tests struct {
	Runs  int             `yaml:"runs,omitempty" json:"runs,omitempty"`
	Kinds map[string]Kind `yaml:"kinds,omitempty" json:"kinds,omitempty"`
}

// Kind is one test kind: how to run tests of that kind, and what app it needs running first.
type Kind struct {
	Image string `yaml:"image,omitempty" json:"image,omitempty"`
	Run   string `yaml:"run" json:"run"`
	Serve string `yaml:"serve,omitempty" json:"serve,omitempty"`
	Ready string `yaml:"ready,omitempty" json:"ready,omitempty"`
}

// Service is one throwaway container of the profile.
type Service struct {
	Name  string            `yaml:"name" json:"name"`
	Image string            `yaml:"image" json:"image"`
	Env   map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	// Port is the TCP port the runner waits on before the job; Data is the directory the service writes, a fresh tmpfs.
	Port int    `yaml:"port" json:"port"`
	Data string `yaml:"data,omitempty" json:"data,omitempty"`
}

// Spec converts the artifact into the port-level contract, dropping fields this build does not
// execute. The conversion is total: every validated artifact maps to a valid ProfileSpec.
func Spec(p RepoProfile) ports.ProfileSpec {
	spec := ports.ProfileSpec{
		Languages:  p.Languages,
		Image:      p.Image,
		Acceptance: p.Acceptance,
		Seam:       p.Seam,
		ACTag:      p.ACTag,
		FetchHosts: p.FetchHosts,
		TestsDir:   p.Paths.Tests,
		Runs:       p.Tests.Runs,
	}
	for _, c := range p.Checks {
		spec.Checks = append(spec.Checks, ports.ProfileCheck{ID: c.ID, Lens: c.Lens, When: c.When, Image: c.Image, Run: c.Run, Baseline: c.Baseline, History: c.History})
	}
	for _, name := range slices.Sorted(maps.Keys(p.Tests.Kinds)) {
		k := p.Tests.Kinds[name]
		spec.Kinds = append(spec.Kinds, ports.ProfileKind{Name: name, Image: k.Image, Run: k.Run, Serve: k.Serve, Ready: k.Ready})
	}
	for _, svc := range p.Services {
		spec.Services = append(spec.Services, ports.ProfileService{Name: svc.Name, Image: svc.Image, Env: svc.Env, Port: svc.Port, Data: svc.Data})
	}
	return spec
}

// ProfileName matches the normalized profile names the runner persists.
var ProfileName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// ProfileRepo matches the regular repositories a profile belongs to.
var ProfileRepo = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// NormalizeRepo maps a repository onto the profile name the runner persists: acme/pay becomes acme-pay.
// The caller still validates the result with ProfileName before using it as a key.
func NormalizeRepo(repo string) string {
	return strings.ReplaceAll(repo, "/", "-")
}

var _ ports.ProfileParser = (*Parser)(nil)

// Parser implements ports.ProfileParser against the shared artifact rules.
type Parser struct{}

// Parse validates the YAML artifact against the given image allowlist and returns the port-level
// contract. It is the only way the server turns a proposal into a spec.
func (Parser) Parse(data []byte, images []string) (ports.ProfileSpec, error) {
	profile, err := ParseApprovedProfile(data, images)
	if err != nil {
		return ports.ProfileSpec{}, err
	}
	return Spec(profile), nil
}

// LoadApprovedProfiles loads only regular YAML files from an operator- or promotion-managed
// directory; this directory is the durable boundary for promoted repository profiles.
func LoadApprovedProfiles(dir string, allowlistedImages []string) (map[string]RepoProfile, error) {
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read approved profile directory: %w", err)
	}
	profiles := make(map[string]RepoProfile, len(entries))
	for _, entry := range entries {
		if !isProfileFile(entry) {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".yaml")
		profile, err := loadApprovedProfile(dir, entry.Name(), name, allowlistedImages)
		if err != nil {
			return nil, err
		}
		profiles[name] = profile
	}
	return profiles, nil
}

func isProfileFile(entry os.DirEntry) bool {
	return !entry.IsDir() && path.Ext(entry.Name()) == ".yaml"
}

func loadApprovedProfile(dir, filename, name string, allowlistedImages []string) (RepoProfile, error) {
	if !ProfileName.MatchString(name) {
		return RepoProfile{}, fmt.Errorf("invalid approved profile name %q", name)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return RepoProfile{}, fmt.Errorf("open approved profile directory: %w", err)
	}
	file, err := root.Open(filename)
	if err != nil {
		_ = root.Close()
		return RepoProfile{}, fmt.Errorf("open approved profile %q: %w", name, err)
	}
	data, readErr := io.ReadAll(io.LimitReader(file, (64<<10)+1))
	closeErr := file.Close()
	rootErr := root.Close()
	if err := errors.Join(readErr, closeErr, rootErr); err != nil {
		return RepoProfile{}, fmt.Errorf("read approved profile %q: %w", name, err)
	}
	profile, err := ParseApprovedProfile(data, allowlistedImages)
	if err != nil {
		return RepoProfile{}, fmt.Errorf("validate approved profile %q: %w", name, err)
	}
	return profile, nil
}

// ParseApprovedProfile validates the syntax and policy bounds before the server records a proposal
// or the runner accepts one for a trial or promotion.
func ParseApprovedProfile(data []byte, allowlistedImages []string) (RepoProfile, error) {
	var p RepoProfile
	if len(data) == 0 || len(data) > 64<<10 {
		return p, errors.New("profile must be between 1 byte and 64 KiB")
	}
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&p); err != nil {
		return RepoProfile{}, fmt.Errorf("decode repo profile: %w", err)
	}
	if err := profileSingleDocument(decoder); err != nil {
		return RepoProfile{}, err
	}
	if !slices.Contains(allowlistedImages, p.Image) || !digestImage.MatchString(p.Image) {
		return RepoProfile{}, ErrImageNotAllowed
	}
	if err := validateProfileFields(p); err != nil {
		return RepoProfile{}, err
	}
	if err := validateProfileHosts(p.FetchHosts); err != nil {
		return RepoProfile{}, err
	}
	if err := validateExtensions(p, allowlistedImages); err != nil {
		return RepoProfile{}, err
	}
	return p, nil
}

// validateExtensions applies the shape rules of the checks, test kinds, runs and services (owned by ports) and the
// one rule only this package can apply: every image they name is on the allowlist the caller holds.
func validateExtensions(p RepoProfile, allowlist []string) error {
	if err := Spec(p).ValidateExtensions(); err != nil {
		return fmt.Errorf("profile extensions: %w", err)
	}
	var images []string
	for _, c := range p.Checks {
		images = append(images, c.Image)
	}
	for _, k := range p.Tests.Kinds {
		if k.Image != "" {
			images = append(images, k.Image)
		}
	}
	for _, svc := range p.Services {
		images = append(images, svc.Image)
	}
	for _, image := range images {
		if !slices.Contains(allowlist, image) {
			return fmt.Errorf("%w: %q", ErrImageNotAllowed, image)
		}
	}
	return nil
}

func profileSingleDocument(decoder *yaml.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return errors.New("profile must contain one YAML document")
	} else if !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode trailing profile document: %w", err)
	}
	return nil
}

func validateProfileFields(p RepoProfile) error {
	if len(p.Languages) == 0 || len(p.Languages) > 4 || p.Acceptance == "" || p.Seam == "" || p.ACTag == "" {
		return errors.New("profile requires languages, acceptance, seam and ac_tag")
	}
	if !strings.Contains(p.Acceptance, "{dir}") || strings.ContainsAny(p.Acceptance, "\x00\n\r") {
		return errors.New("acceptance must be one command containing {dir}")
	}
	if p.Paths.Tests == "" || path.Clean(p.Paths.Tests) != p.Paths.Tests || strings.HasPrefix(p.Paths.Tests, "/") || strings.HasPrefix(p.Paths.Tests, "../") {
		return errors.New("paths.tests must be a clean repository-relative path")
	}
	return nil
}

func validateProfileHosts(hosts []string) error {
	for _, host := range hosts {
		u, err := url.Parse("https://" + host)
		if err != nil || u.Host != host || u.Path != "" || u.User != nil || strings.Contains(host, "*") {
			return errors.New("fetch_hosts entries must be exact DNS hostnames")
		}
	}
	return nil
}
