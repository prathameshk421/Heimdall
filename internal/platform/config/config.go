package config

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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

// projectKeyPattern is the Jira project key shape. A key ends up in a JQL query and a cursor source, so a
// value outside these characters is refused at boot.
var projectKeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// agentNamePattern is Switch's naming rule: lowercase alphanumeric, dots, dashes, underscores, max 64 chars.
var agentNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// Project maps one tracker project to the repository it is built in (D147).
type Project struct {
	TrackerProject string
	Repo           string
	BaseBranch     string
}

// Feed is a chat room that hears the story of every change besides the room of the change itself, for example the
// Telegram group, a Mattermost channel or a Slack channel. Audience is "tests", "review" or "both".
type Feed struct {
	RoomID   string
	Audience string
}

// Rooms names the two standing channels every change uses: one for the tests, one for the reviews. Empty means a room
// per change.
type Rooms struct {
	Tests   string
	Reviews string
}

// Config is the validated infrastructure configuration. Load is the only way to get a non-zero one.
type Config struct {
	SecretsDir     string
	Listen         string
	Projects       []Project
	Feeds          []Feed
	Rooms          Rooms
	HumanTokens    map[string]string
	PackIdentities map[string]PackIdentitySecrets
	Database       Database
	Jira           Jira
	TrackerPath    string
	Switch         Switch
	GitHub         GitHub
	Agents         Agents
	AgentRuntime   AgentRuntimeConfig
	byTracker      map[string]Project
	selections     map[string]string
}

// PackIdentitySecrets names the server-side verification and age decryption files for one policy person.
type PackIdentitySecrets struct {
	SigningPublicKey string
	AgeIdentity      string
}

// AgentRuntimeConfig holds the Switch controller placement and per-agent MCP endpoint.
type AgentRuntimeConfig struct {
	Machine     string
	MCPEndpoint string
	Model       string
	// AgentsDir is the controller's per-agent directory root. Each role agent's directory is AgentsDir/<name>; empty
	// leaves the directory to Switch.
	AgentsDir string
}

const defaultAgentModel = "openrouter/deepseek/deepseek-v4-flash"

// Agents is [agents]. TestAuthor, Reviewers and Resolver name the agents participating in the workflow.
type Agents struct {
	TestAuthor string
	Reviewers  []string
	Resolver   string
}

// Database is [database]. URLSecret names the file under SecretsDir that holds the Postgres URL. The URL
// itself never appears in heimdall.toml, so it cannot be committed with the file.
type Database struct {
	URLSecret string
}

// Jira is [jira]. ProjectKeys lists the tracker projects this install polls. The email and API token are read
// from the named secret files, never stored here.
type Jira struct {
	BaseURL        string
	ProjectKeys    []string
	EmailSecret    string
	APITokenSecret string
}

// Switch is [switch]. AgentID, AgentName and BridgeID identify the coordinator agent and its connection. The
// API key and registration token are read from the named secret files.
type Switch struct {
	BaseURL                 string
	AgentID                 string
	AgentName               string
	BridgeID                string
	APIKeySecret            string
	RegistrationTokenSecret string
}

// GitHub is [github]. The App id, installation id and private key are read from the named secret files.
type GitHub struct {
	AppIDSecret          string
	InstallationIDSecret string
	PrivateKeySecret     string
}

// fileConfig mirrors heimdall.toml. Pointers tell an absent key (use the default) from an empty one (refuse).
type fileConfig struct {
	SecretsDir     *string                     `toml:"secrets_dir"`
	Listen         *string                     `toml:"listen"`
	Projects       []fileProject               `toml:"projects"`
	Feeds          []fileFeed                  `toml:"feeds"`
	Rooms          *fileRooms                  `toml:"rooms"`
	HumanTokens    map[string]string           `toml:"human_tokens"`
	PackIdentities map[string]filePackIdentity `toml:"pack_identities"`
	Database       *fileDatabase               `toml:"database"`
	Jira           *fileJira                   `toml:"jira"`
	Switch         *fileSwitch                 `toml:"switch"`
	GitHub         *fileGitHub                 `toml:"github"`
	Agents         *fileAgents                 `toml:"agents"`
	VCS            *fileProvider               `toml:"vcs"`
	Tracker        *fileTracker                `toml:"tracker"`
	Chat           *fileChat                   `toml:"chat"`
	AgentRuntime   *fileAgentRuntime           `toml:"agent_runtime"`
	Memory         *fileProvider               `toml:"memory"`
}

type filePackIdentity struct {
	SigningPublicKey *string `toml:"signing_public_key_secret"`
	AgeIdentity      *string `toml:"age_identity_secret"`
}

// fileAgents is [agents]. Each key is validated by build.
type fileAgents struct {
	TestAuthor *string   `toml:"test_author"`
	Reviewers  *[]string `toml:"reviewers"`
	Resolver   *string   `toml:"resolver"`
}

// fileDatabase is [database]. url_secret is a secret file name, not the URL.
type fileDatabase struct {
	URLSecret *string `toml:"url_secret"`
}

// fileJira is [jira]. Each key is validated by build.
type fileJira struct {
	BaseURL        *string   `toml:"base_url"`
	ProjectKeys    *[]string `toml:"project_keys"`
	EmailSecret    *string   `toml:"email_secret"`
	APITokenSecret *string   `toml:"api_token_secret"`
}

// fileSwitch is [switch]. Each key is validated by build.
type fileSwitch struct {
	BaseURL                 *string `toml:"base_url"`
	AgentID                 *string `toml:"agent_id"`
	AgentName               *string `toml:"agent_name"`
	BridgeID                *string `toml:"bridge_id"`
	APIKeySecret            *string `toml:"api_key_secret"`
	RegistrationTokenSecret *string `toml:"registration_token_secret"`
}

// fileGitHub is [github]. Each secret key names a file under SecretsDir.
type fileGitHub struct {
	AppIDSecret          *string `toml:"app_id_secret"`
	InstallationIDSecret *string `toml:"installation_id_secret"`
	PrivateKeySecret     *string `toml:"private_key_secret"`
}

// fileProvider is a [section] whose only key is provider: the adapter name the registry must know.
type fileProvider struct {
	Provider *string `toml:"provider"`
}

type fileTracker struct {
	Provider *string `toml:"provider"`
	Path     *string `toml:"path"`
}

type fileAgentRuntime struct {
	Provider    *string `toml:"provider"`
	Machine     *string `toml:"machine"`
	MCPEndpoint *string `toml:"mcp_endpoint"`
	Model       *string `toml:"model"`
	AgentsDir   *string `toml:"agents_dir"`
}

// fileChat is [chat]. Its key is bridge, the Switch connection that carries chat (D79).
type fileChat struct {
	Bridge *string `toml:"bridge"`
}

type fileRooms struct {
	Tests   string `toml:"tests"`
	Reviews string `toml:"reviews"`
}

type fileFeed struct {
	RoomID   string `toml:"room_id"`
	Audience string `toml:"audience"`
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
	errs = append(errs, addLiveSections(&cfg, raw)...)
	errs = append(errs, addKnowledgeSecrets(&cfg, raw)...)
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
	if raw.Rooms != nil {
		cfg.Rooms = Rooms{Tests: raw.Rooms.Tests, Reviews: raw.Rooms.Reviews}
		if (cfg.Rooms.Tests == "") != (cfg.Rooms.Reviews == "") {
			errs = append(errs, errors.New("rooms: tests and reviews must both be set, or neither"))
		}
	}
	feeds, feedErrs := checkFeeds(raw.Feeds)
	cfg.Feeds = feeds
	errs = append(errs, feedErrs...)
	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}
	return cfg, nil
}

// checkFeeds validates the feed rooms: each has a room and a known audience, and no room is listed twice.
func checkFeeds(raw []fileFeed) ([]Feed, []error) {
	var errs []error
	var feeds []Feed
	seen := map[string]bool{}
	for i, f := range raw {
		where := fmt.Sprintf("feeds[%d]", i)
		switch {
		case f.RoomID == "":
			errs = append(errs, fmt.Errorf("%s: room_id is required", where))
			continue
		case f.Audience != "tests" && f.Audience != "review" && f.Audience != "both":
			errs = append(errs, fmt.Errorf("%s: audience %q must be tests, review or both", where, f.Audience))
			continue
		case seen[f.RoomID]:
			errs = append(errs, fmt.Errorf("%s: room %q is listed twice", where, f.RoomID))
			continue
		}
		seen[f.RoomID] = true
		feeds = append(feeds, Feed(f))
	}
	return feeds, errs
}

func addKnowledgeSecrets(cfg *Config, raw fileConfig) []error {
	return append(addHumanTokens(cfg, raw.HumanTokens), addPackIdentities(cfg, raw.PackIdentities)...)
}

func addHumanTokens(cfg *Config, tokens map[string]string) []error {
	var errs []error
	if len(tokens) > 0 {
		cfg.HumanTokens = make(map[string]string, len(tokens))
		for person, secret := range tokens {
			if person == "" || len(person) > 64 || strings.ContainsAny(person, " /\\\t\r\n") {
				errs = append(errs, fmt.Errorf("human_tokens.%s: person key must be a non-empty policy person name of at most 64 characters", person))
				continue
			}
			if err := checkSecretName(secret); err != nil {
				errs = append(errs, fmt.Errorf("human_tokens.%s: secret name: %w", person, err))
				continue
			}
			cfg.HumanTokens[person] = secret
		}
	}
	return errs
}

func addPackIdentities(cfg *Config, identities map[string]filePackIdentity) []error {
	var errs []error
	if len(identities) > 0 {
		cfg.PackIdentities = make(map[string]PackIdentitySecrets, len(identities))
		for person, rawIdentity := range identities {
			if person == "" || len(person) > 64 || strings.ContainsAny(person, " /\\\t\r\n") {
				errs = append(errs, fmt.Errorf("pack_identities.%s: person key must be a non-empty policy person name of at most 64 characters", person))
				continue
			}
			if err := checkSecretKey("pack_identities."+person+".signing_public_key_secret", rawIdentity.SigningPublicKey); err != nil {
				errs = append(errs, err)
			}
			if err := checkSecretKey("pack_identities."+person+".age_identity_secret", rawIdentity.AgeIdentity); err != nil {
				errs = append(errs, err)
			}
			if rawIdentity.SigningPublicKey != nil && rawIdentity.AgeIdentity != nil {
				cfg.PackIdentities[person] = PackIdentitySecrets{SigningPublicKey: *rawIdentity.SigningPublicKey, AgeIdentity: *rawIdentity.AgeIdentity}
			}
		}
	}
	return errs
}

// addSelections records each provider selection that is present. An absent section or key means unset;
// an empty value is refused, because an empty name can never match a registered adapter.
func addSelections(cfg *Config, raw fileConfig) []error {
	selected := []struct {
		kind, field string
		value       *string
	}{
		{"vcs", "vcs.provider", providerOf(raw.VCS)},
		{"tracker", "tracker.provider", trackerProviderOf(raw.Tracker)},
		{"chat", "chat.bridge", bridgeOf(raw.Chat)},
		{"agent_runtime", "agent_runtime.provider", agentRuntimeProviderOf(raw.AgentRuntime)},
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
	if raw.Tracker != nil {
		provider := trackerProviderOf(raw.Tracker)
		path := raw.Tracker.Path
		if provider != nil && *provider == "file" {
			switch {
			case path == nil || *path == "":
				errs = append(errs, errors.New("tracker.path must be set when tracker.provider is file"))
			case !filepath.IsAbs(*path):
				errs = append(errs, errors.New("tracker.path must be an absolute path"))
			default:
				cfg.TrackerPath = filepath.Clean(*path)
			}
		} else if path != nil {
			errs = append(errs, errors.New("tracker.path is only valid when tracker.provider is file"))
		}
	}
	return errs
}

// addLiveSections validates and records the sections a live run needs: [database], [jira], [switch] and
// [github]. An absent section is left to the binary's own missing-key check; a present one must be complete,
// because a half-filled section is a mistake, not an unset feature. Every error names its key.
func addLiveSections(cfg *Config, raw fileConfig) []error {
	var errs []error
	if raw.Database != nil {
		if err := checkSecretKey("database.url_secret", raw.Database.URLSecret); err != nil {
			errs = append(errs, err)
		} else {
			cfg.Database.URLSecret = *raw.Database.URLSecret
		}
	}
	if raw.Jira != nil {
		errs = append(errs, checkJira(&cfg.Jira, raw.Jira)...)
	}
	if raw.Switch != nil {
		errs = append(errs, checkSwitch(&cfg.Switch, raw.Switch)...)
	}
	if raw.GitHub != nil {
		var gerrs []error
		if err := checkSecretKey("github.app_id_secret", raw.GitHub.AppIDSecret); err != nil {
			gerrs = append(gerrs, err)
		}
		if err := checkSecretKey("github.installation_id_secret", raw.GitHub.InstallationIDSecret); err != nil {
			gerrs = append(gerrs, err)
		}
		if err := checkSecretKey("github.private_key_secret", raw.GitHub.PrivateKeySecret); err != nil {
			gerrs = append(gerrs, err)
		}
		errs = append(errs, gerrs...)
		if len(gerrs) == 0 {
			cfg.GitHub = GitHub{
				AppIDSecret:          *raw.GitHub.AppIDSecret,
				InstallationIDSecret: *raw.GitHub.InstallationIDSecret,
				PrivateKeySecret:     *raw.GitHub.PrivateKeySecret,
			}
		}
	}
	if raw.Agents != nil {
		errs = append(errs, checkAgents(&cfg.Agents, raw.Agents)...)
	}
	if raw.AgentRuntime != nil {
		errs = append(errs, checkAgentRuntime(&cfg.AgentRuntime, raw.AgentRuntime)...)
	}
	return errs
}

func checkAgentRuntime(out *AgentRuntimeConfig, raw *fileAgentRuntime) []error {
	var errs []error
	errs = append(errs, validateRuntimeProvider(raw.Provider)...)
	errs = append(errs, validateRuntimeMachine(raw.Machine)...)
	errs = append(errs, validateMCPEndpoint(raw.MCPEndpoint)...)
	model, modelErr := runtimeModel(raw.Model)
	if modelErr != nil {
		errs = append(errs, modelErr)
	}
	errs = append(errs, validateAgentsDir(raw.AgentsDir)...)
	if len(errs) == 0 {
		out.Machine = *raw.Machine
		out.MCPEndpoint = *raw.MCPEndpoint
		out.Model = model
		if raw.AgentsDir != nil {
			out.AgentsDir = *raw.AgentsDir
		}
	}
	return errs
}

func validateAgentsDir(dir *string) []error {
	if dir == nil {
		return nil
	}
	if !strings.HasPrefix(*dir, "/") || len(*dir) > 1024 || strings.ContainsAny(*dir, "\x00\r\n") {
		return []error{errors.New("agent_runtime.agents_dir must be an absolute path of at most 1024 bytes")}
	}
	return nil
}

func validateRuntimeProvider(provider *string) []error {
	switch {
	case provider == nil || *provider == "":
		return []error{errors.New("agent_runtime.provider is required")}
	case *provider != "switchroom":
		return []error{errors.New("agent_runtime.provider must be switchroom")}
	default:
		return nil
	}
}

func validateRuntimeMachine(machine *string) []error {
	if machine == nil || strings.TrimSpace(*machine) == "" || len(*machine) > 128 {
		return []error{errors.New("agent_runtime.machine is required and must be 1 to 128 bytes")}
	}
	return nil
}

func validateMCPEndpoint(endpoint *string) []error {
	if endpoint == nil || *endpoint == "" {
		return []error{errors.New("agent_runtime.mcp_endpoint is required")}
	}
	if err := checkBaseURL("agent_runtime.mcp_endpoint", endpoint); err != nil {
		return []error{err}
	}
	u, err := url.Parse(*endpoint)
	if err != nil || u.Path != "/mcp" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return []error{errors.New("agent_runtime.mcp_endpoint must be an /mcp URL without credentials, query or fragment")}
	}
	return nil
}

func runtimeModel(model *string) (string, error) {
	if model == nil {
		return defaultAgentModel, nil
	}
	if strings.TrimSpace(*model) == "" || len(*model) > 256 {
		return "", errors.New("agent_runtime.model must be 1 to 256 bytes")
	}
	return *model, nil
}

// checkAgentName requires a valid Switch agent name.
func checkAgentName(field, name string) error {
	if name == "" {
		return fmt.Errorf("%s is required", field)
	}
	if !agentNamePattern.MatchString(name) {
		return fmt.Errorf("%s: %q must match %s", field, name, agentNamePattern)
	}
	return nil
}

// checkReviewers validates the reviewers list.
func checkReviewers(raw *[]string) ([]string, []error) {
	if raw == nil || len(*raw) == 0 {
		return nil, []error{errors.New("agents.reviewers is required and must list at least one reviewer")}
	}
	var errs []error
	seen := map[string]bool{}
	for _, r := range *raw {
		if err := checkAgentName("agents.reviewers", r); err != nil {
			errs = append(errs, err)
		} else if seen[r] {
			errs = append(errs, fmt.Errorf("agents.reviewers: %q is listed twice", r))
		}
		seen[r] = true
	}
	if len(errs) > 0 {
		return nil, errs
	}
	return slices.Clone(*raw), nil
}

// checkAgents validates [agents] and records it when every key is good.
func checkAgents(out *Agents, raw *fileAgents) []error {
	var errs []error
	if raw.TestAuthor == nil || *raw.TestAuthor == "" {
		errs = append(errs, errors.New("agents.test_author is required"))
	} else if err := checkAgentName("agents.test_author", *raw.TestAuthor); err != nil {
		errs = append(errs, err)
	}
	revs, rerrs := checkReviewers(raw.Reviewers)
	errs = append(errs, rerrs...)
	if raw.Resolver == nil || *raw.Resolver == "" {
		errs = append(errs, errors.New("agents.resolver is required"))
	} else if err := checkAgentName("agents.resolver", *raw.Resolver); err != nil {
		errs = append(errs, err)
	}
	if len(errs) == 0 {
		*out = Agents{
			TestAuthor: *raw.TestAuthor,
			Reviewers:  revs,
			Resolver:   *raw.Resolver,
		}
	}
	return errs
}

// checkJira validates [jira] and records it when every key is good.
func checkJira(out *Jira, raw *fileJira) []error {
	var errs []error
	if err := checkBaseURL("jira.base_url", raw.BaseURL); err != nil {
		errs = append(errs, err)
	}
	keys, err := checkProjectKeys("jira.project_keys", raw.ProjectKeys)
	errs = append(errs, err...)
	if err := checkSecretKey("jira.email_secret", raw.EmailSecret); err != nil {
		errs = append(errs, err)
	}
	if err := checkSecretKey("jira.api_token_secret", raw.APITokenSecret); err != nil {
		errs = append(errs, err)
	}
	if len(errs) == 0 {
		*out = Jira{
			BaseURL:        *raw.BaseURL,
			ProjectKeys:    keys,
			EmailSecret:    *raw.EmailSecret,
			APITokenSecret: *raw.APITokenSecret,
		}
	}
	return errs
}

// checkSwitch validates [switch] and records it when every key is good.
func checkSwitch(out *Switch, raw *fileSwitch) []error {
	var errs []error
	if err := checkBaseURL("switch.base_url", raw.BaseURL); err != nil {
		errs = append(errs, err)
	}
	if err := checkRequiredText("switch.agent_id", raw.AgentID); err != nil {
		errs = append(errs, err)
	}
	if err := checkRequiredText("switch.agent_name", raw.AgentName); err != nil {
		errs = append(errs, err)
	}
	if err := checkRequiredText("switch.bridge_id", raw.BridgeID); err != nil {
		errs = append(errs, err)
	}
	if err := checkSecretKey("switch.api_key_secret", raw.APIKeySecret); err != nil {
		errs = append(errs, err)
	}
	if err := checkSecretKey("switch.registration_token_secret", raw.RegistrationTokenSecret); err != nil {
		errs = append(errs, err)
	}
	if len(errs) == 0 {
		*out = Switch{
			BaseURL:                 *raw.BaseURL,
			AgentID:                 *raw.AgentID,
			AgentName:               *raw.AgentName,
			BridgeID:                *raw.BridgeID,
			APIKeySecret:            *raw.APIKeySecret,
			RegistrationTokenSecret: *raw.RegistrationTokenSecret,
		}
	}
	return errs
}

// checkBaseURL requires an absolute https URL, or http only for a loopback host. A plain-http remote site is
// refused because it would carry tokens and bodies in clear text.
func checkBaseURL(field string, raw *string) error {
	if raw == nil || *raw == "" {
		return fmt.Errorf("%s is required", field)
	}
	u, err := url.Parse(*raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%s %q must be an absolute http(s) URL", field, *raw)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return nil
	case "http":
		if isLoopbackHost(u.Hostname()) {
			return nil
		}
		return fmt.Errorf("%s %q: http is allowed only for a loopback host; use https", field, *raw)
	default:
		return fmt.Errorf("%s %q must use https (or http on a loopback host)", field, *raw)
	}
}

// isLoopbackHost reports whether host names this machine. "localhost" is accepted because it is the name a
// developer uses for a local fake; the IP forms are checked rather than trusted.
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// checkProjectKeys requires a non-empty list of well-formed, distinct Jira project keys.
func checkProjectKeys(field string, raw *[]string) ([]string, []error) {
	if raw == nil || len(*raw) == 0 {
		return nil, []error{fmt.Errorf("%s is required and must list at least one project", field)}
	}
	keys := *raw
	seen := map[string]bool{}
	var errs []error
	for _, key := range keys {
		switch {
		case !projectKeyPattern.MatchString(key):
			errs = append(errs, fmt.Errorf("%s: %q must match %s", field, key, projectKeyPattern))
		case seen[key]:
			errs = append(errs, fmt.Errorf("%s: %q is listed twice", field, key))
		}
		seen[key] = true
	}
	if len(errs) > 0 {
		return nil, errs
	}
	return keys, nil
}

// checkRequiredText refuses an absent or empty text key and names it.
func checkRequiredText(field string, raw *string) error {
	if raw == nil || *raw == "" {
		return fmt.Errorf("%s is required", field)
	}
	return nil
}

// checkSecretKey validates a key whose value is a secret file name. The name is checkable; the secret value
// never enters the config, so it can never be printed here.
func checkSecretKey(field string, raw *string) error {
	if raw == nil || *raw == "" {
		return fmt.Errorf("%s is required", field)
	}
	if strings.Contains(*raw, "://") {
		// Do not echo the value: a URL written here by mistake may hold a password.
		return fmt.Errorf("%s must be a secret file name, not a URL", field)
	}
	if err := checkSecretName(*raw); err != nil {
		return fmt.Errorf("%s %q: %w", field, *raw, err)
	}
	return nil
}

func providerOf(p *fileProvider) *string {
	if p == nil {
		return nil
	}
	return p.Provider
}

func trackerProviderOf(p *fileTracker) *string {
	if p == nil {
		return nil
	}
	return p.Provider
}

func agentRuntimeProviderOf(p *fileAgentRuntime) *string {
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
