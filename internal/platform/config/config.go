package config

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/pelletier/go-toml/v2"
)

const (
	defaultDir    = "/etc/heimdall/secrets" // D89: used when the file has no secrets_dir key
	defaultListen = ":8080"
	// maxSecretBytes bounds a secret read. A token is a line; a large file is a mistake, not a secret.
	maxSecretBytes = 64 << 10
)

// repoPattern is the GitHub owner/name shape. It is deliberately strict: a repo string is used to build
// URLs and branch names, so anything outside these characters is refused at boot.
var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// Project maps one tracker project to the repository it is built in (D147).
type Project struct {
	TrackerProject string
	Repo           string
	BaseBranch     string
}

// Config is the validated infrastructure configuration. Load is the only way to get a non-zero one.
type Config struct {
	SecretsDir string
	Listen     string
	Projects   []Project
	byTracker  map[string]Project
}

// fileConfig mirrors heimdall.toml. Pointers tell an absent key (use the default) from an empty one (refuse).
type fileConfig struct {
	SecretsDir *string       `toml:"secrets_dir"`
	Listen     *string       `toml:"listen"`
	Projects   []fileProject `toml:"projects"`
}

type fileProject struct {
	TrackerProject string `toml:"tracker_project"`
	Repo           string `toml:"repo"`
	BaseBranch     string `toml:"base_branch"`
}

// Load reads and validates the TOML file at path. Unknown keys are errors. Every other problem is
// collected and returned joined, so one run reports them all.
// The file is read through os.Root in its own directory, so a config path that escapes through a symlink is
// refused the same way a secret is.
func Load(path string) (Config, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return Config{}, fmt.Errorf("load config %s: %w", path, err)
	}
	defer func() { _ = root.Close() }() // read-only handle: a close error changes nothing here
	data, err := root.ReadFile(filepath.Base(path))
	if err != nil {
		return Config{}, fmt.Errorf("load config %s: %w", path, err)
	}
	var raw fileConfig
	if err := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&raw); err != nil {
		return Config{}, fmt.Errorf("load config %s: %w", path, describeDecodeError(err))
	}
	cfg, err := build(raw)
	if err != nil {
		return Config{}, fmt.Errorf("load config %s: %w", path, err)
	}
	return cfg, nil
}

// describeDecodeError names every unknown key. go-toml's own message says only that fields are missing.
func describeDecodeError(err error) error {
	var missing *toml.StrictMissingError
	if !errors.As(err, &missing) {
		return err
	}
	errs := make([]error, 0, len(missing.Errors))
	for _, de := range missing.Errors {
		errs = append(errs, fmt.Errorf("unknown key %q", strings.Join(de.Key(), ".")))
	}
	return errors.Join(errs...)
}

func build(raw fileConfig) (Config, error) {
	cfg := Config{SecretsDir: defaultDir, Listen: defaultListen, byTracker: map[string]Project{}}
	var errs []error
	if raw.SecretsDir != nil {
		if *raw.SecretsDir == "" {
			errs = append(errs, errors.New("secrets_dir must not be empty"))
		} else {
			cfg.SecretsDir = *raw.SecretsDir
		}
	}
	if raw.Listen != nil {
		if _, _, err := net.SplitHostPort(*raw.Listen); err != nil {
			errs = append(errs, fmt.Errorf("listen %q must be host:port: %w", *raw.Listen, err))
		} else {
			cfg.Listen = *raw.Listen
		}
	}
	firstIndex := map[string]int{}
	for i, p := range raw.Projects {
		where := fmt.Sprintf("projects[%d]", i)
		errs = append(errs, checkProject(where, p)...)
		if p.TrackerProject == "" {
			continue
		}
		if first, dup := firstIndex[p.TrackerProject]; dup {
			errs = append(errs, fmt.Errorf("%s: tracker_project %q already mapped at projects[%d]", where, p.TrackerProject, first))
			continue
		}
		firstIndex[p.TrackerProject] = i
		proj := Project(p)
		cfg.Projects = append(cfg.Projects, proj)
		cfg.byTracker[p.TrackerProject] = proj
	}
	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}
	return cfg, nil
}

func checkProject(where string, p fileProject) []error {
	var errs []error
	if p.TrackerProject == "" {
		errs = append(errs, fmt.Errorf("%s: tracker_project is required", where))
	}
	switch {
	case p.Repo == "":
		errs = append(errs, fmt.Errorf("%s: repo is required", where))
	case !repoPattern.MatchString(p.Repo):
		errs = append(errs, fmt.Errorf("%s: repo %q must be owner/name", where, p.Repo))
	}
	if p.BaseBranch == "" {
		errs = append(errs, fmt.Errorf("%s: base_branch is required", where))
	}
	return errs
}

// ProjectFor returns the repo mapped to a tracker project. ok is false for an unmapped project.
func (c Config) ProjectFor(trackerProject string) (Project, bool) {
	p, ok := c.byTracker[trackerProject]
	return p, ok
}

// Secret reads the secret called name from SecretsDir and returns its value without the trailing newline.
// The name must be one path element. The file must be a regular file whose real location stays inside
// SecretsDir (os.Root enforces that). Errors name the secret but never carry its value.
func (c Config) Secret(name string) (string, error) {
	if err := checkSecretName(name); err != nil {
		return "", fmt.Errorf("secret %q: %w", name, err)
	}
	root, err := os.OpenRoot(c.SecretsDir)
	if err != nil {
		return "", fmt.Errorf("secret %q: open secrets_dir: %w", name, err)
	}
	defer func() { _ = root.Close() }() // read-only handle: a close error changes nothing here
	f, err := root.Open(name)
	if err != nil {
		return "", fmt.Errorf("secret %q: %w", name, err)
	}
	defer func() { _ = f.Close() }() // read-only handle: a close error changes nothing here
	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("secret %q: stat: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("secret %q: not a regular file", name)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxSecretBytes+1))
	if err != nil {
		return "", fmt.Errorf("secret %q: read: %w", name, err)
	}
	if len(data) > maxSecretBytes {
		return "", fmt.Errorf("secret %q: larger than %d bytes", name, maxSecretBytes)
	}
	value := strings.TrimRight(string(data), "\r\n")
	if value == "" {
		return "", fmt.Errorf("secret %q: empty", name)
	}
	if strings.ContainsAny(value, "\r\n") {
		return "", fmt.Errorf("secret %q: must be a single line", name)
	}
	return value, nil
}

func checkSecretName(name string) error {
	switch {
	case name == "":
		return errors.New("name is empty")
	case name == "." || strings.Contains(name, ".."):
		return errors.New("name must not be a single dot or contain dot-dot")
	case strings.ContainsAny(name, `/\`):
		return errors.New("name must not contain a path separator")
	}
	return nil
}

// OnceSet remembers keys. First reports true only the first time a key is offered, so a caller can log
// an unmapped project once. The zero value is ready to use and it is safe for concurrent use.
type OnceSet struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

// First reports whether key was offered for the first time.
func (s *OnceSet) First(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen == nil {
		s.seen = map[string]struct{}{}
	}
	if _, ok := s.seen[key]; ok {
		return false
	}
	s.seen[key] = struct{}{}
	return true
}

// FilePath returns the config file path: the --config flag if given, else HEIMDALL_CONFIG. There is no
// default path; a server that does not know which file to read refuses to start.
func FilePath(args []string, getenv func(string) string) (string, error) {
	fs := flag.NewFlagSet("heimdall-server", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("config", "", "path to heimdall.toml")
	if err := fs.Parse(args); err != nil {
		return "", fmt.Errorf("parse arguments: %w", err)
	}
	if fs.NArg() > 0 {
		return "", fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if *path != "" {
		return *path, nil
	}
	if env := getenv("HEIMDALL_CONFIG"); env != "" {
		return env, nil
	}
	return "", errors.New("no config file: pass --config or set HEIMDALL_CONFIG")
}

// Getenv reads one environment variable. It is the binary's only environment access, passed to
// FilePath from cmd/* so that the rule against os.Getenv outside this package holds.
func Getenv(name string) string {
	return os.Getenv(name)
}
