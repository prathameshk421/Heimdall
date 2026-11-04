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
	"syscall"

	"github.com/pelletier/go-toml/v2"
)

const (
	defaultDir    = "/etc/heimdall/secrets" // D89: used when the file has no secrets_dir key
	defaultListen = ":8080"
	// maxSecretBytes bounds a secret read. A token is a line; a large file is a mistake, not a secret.
	maxSecretBytes = 64 << 10
	// maxMultilineSecretBytes bounds a multi-line secret such as a PEM key. A 4096-bit RSA key is under 4 KiB.
	maxMultilineSecretBytes = 16 << 10
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
	selections map[string]string
}

// fileConfig mirrors heimdall.toml. Pointers tell an absent key (use the default) from an empty one (refuse).
type fileConfig struct {
	SecretsDir   *string       `toml:"secrets_dir"`
	Listen       *string       `toml:"listen"`
	Projects     []fileProject `toml:"projects"`
	VCS          *fileProvider `toml:"vcs"`
	Tracker      *fileProvider `toml:"tracker"`
	Chat         *fileChat     `toml:"chat"`
	AgentRuntime *fileProvider `toml:"agent_runtime"`
	Memory       *fileProvider `toml:"memory"`
}

// fileProvider is a [section] whose only key is provider: the adapter name the registry must know.
type fileProvider struct {
	Provider *string `toml:"provider"`
}

// fileChat is [chat]. Its key is bridge, the Switch connection that carries chat (D79).
type fileChat struct {
	Bridge *string `toml:"bridge"`
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
	cfg := Config{SecretsDir: defaultDir, Listen: defaultListen, byTracker: map[string]Project{}, selections: map[string]string{}}
	var errs []error
	errs = append(errs, addSelections(&cfg, raw)...)
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

// addSelections records each provider selection that is present. An absent section or key means unset;
// an empty value is refused, because an empty name can never match a registered adapter.
func addSelections(cfg *Config, raw fileConfig) []error {
	selected := []struct {
		kind, field string
		value       *string
	}{
		{"vcs", "vcs.provider", providerOf(raw.VCS)},
		{"tracker", "tracker.provider", providerOf(raw.Tracker)},
		{"chat", "chat.bridge", bridgeOf(raw.Chat)},
		{"agent_runtime", "agent_runtime.provider", providerOf(raw.AgentRuntime)},
		{"memory", "memory.provider", providerOf(raw.Memory)},
	}
	var errs []error
	for _, s := range selected {
		switch {
		case s.value == nil:
			continue
		case *s.value == "":
			errs = append(errs, fmt.Errorf("%s must not be empty", s.field))
		default:
			cfg.selections[s.kind] = *s.value
		}
	}
	return errs
}

func providerOf(p *fileProvider) *string {
	if p == nil {
		return nil
	}
	return p.Provider
}

func bridgeOf(c *fileChat) *string {
	if c == nil {
		return nil
	}
	return c.Bridge
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

// Selection returns the adapter name selected for kind ("vcs", "tracker", "chat", "agent_runtime" or
// "memory"; chat's name comes from chat.bridge). ok is false when the section or key is absent.
func (c Config) Selection(kind string) (string, bool) {
	name, ok := c.selections[kind]
	return name, ok
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
	data, err := c.readSecretFile(name, maxSecretBytes, false)
	if err != nil {
		return "", err
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

// SecretMultiline reads a secret that spans lines, such as a PEM private key, and returns its bytes without the
// trailing newlines. It keeps every rule Secret has (one path element, a regular file, confined to SecretsDir,
// not empty) except the single-line rule. It adds two rules: the file is at most 16 KiB, and its mode has no
// group or other permission bits (mode & 0o077 must be 0). Errors name the secret but never carry its content.
func (c Config) SecretMultiline(name string) ([]byte, error) {
	data, err := c.readSecretFile(name, maxMultilineSecretBytes, true)
	if err != nil {
		return nil, err
	}
	value := bytes.TrimRight(data, "\r\n")
	if len(value) == 0 {
		return nil, fmt.Errorf("secret %q: empty", name)
	}
	return value, nil
}

// readSecretFile opens name under SecretsDir through os.Root and returns at most limit bytes. It refuses a name that
// is not one path element, a file that is not regular, and, when privateMode is set, a file with any group or other
// permission bit. Errors name the secret and never carry its content.
func (c Config) readSecretFile(name string, limit int64, privateMode bool) ([]byte, error) {
	if err := checkSecretName(name); err != nil {
		return nil, fmt.Errorf("secret %q: %w", name, err)
	}
	root, err := os.OpenRoot(c.SecretsDir)
	if err != nil {
		return nil, fmt.Errorf("secret %q: open secrets_dir: %w", name, err)
	}
	defer func() { _ = root.Close() }() // read-only handle: a close error changes nothing here
	// O_NONBLOCK stops an open of a named pipe from waiting for a writer. Stat then refuses it as not regular.
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("secret %q: %w", name, err)
	}
	defer func() { _ = f.Close() }() // read-only handle: a close error changes nothing here
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("secret %q: stat: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("secret %q: not a regular file", name)
	}
	if privateMode && info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("secret %q: mode %04o gives group or others access; use 0600 or stricter",
			name, uint32(info.Mode().Perm()))
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, fmt.Errorf("secret %q: read: %w", name, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("secret %q: larger than %d bytes", name, limit)
	}
	return data, nil
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
