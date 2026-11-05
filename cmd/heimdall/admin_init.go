package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
)

// runAdmin handles the initial operator configuration. It creates only the TOML file: integrations,
// secret installation, and deployment remain separately guided steps.
func runAdmin(args []string, in io.Reader, out, stderr io.Writer) int {
	if len(args) >= 2 && args[0] == "secret" && args[1] == "set" {
		return runAdminSecret(args[2:], out, stderr, config.Getenv, readHiddenSecret)
	}
	if len(args) == 0 || args[0] != "init" {
		_ = writeAdmin(stderr, "usage: heimdall admin init [--config PATH]\n")
		return 2
	}
	return runAdminInit(args, in, out, stderr)
}

func runAdminInit(args []string, in io.Reader, out, stderr io.Writer) int {
	fs := flag.NewFlagSet("heimdall admin init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	pathFlag := fs.String("config", "", "path to heimdall.toml (default: HEIMDALL_CONFIG or /etc/heimdall/heimdall.toml)")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		_ = writeAdmin(stderr, "usage: heimdall admin init [--config PATH]\n")
		return 2
	}
	path := *pathFlag
	if path == "" {
		path = config.Getenv("HEIMDALL_CONFIG")
	}
	if path == "" {
		path = "/etc/heimdall/heimdall.toml"
	}
	status := adminConfigStatus(path)
	if status == 0 {
		return writeAdmin(stderr, "heimdall admin init: existing config %s is invalid\n", path)
	}
	if status == 1 {
		return writeAdmin(out, "Configuration is already present and valid at %s; setup resumed.\n", path)
	}
	if status == 3 {
		return writeAdmin(stderr, "heimdall admin init: cannot inspect config path %s\n", path)
	}
	answers, err := collectAdminAnswers(bufio.NewReader(in), out)
	if err != nil {
		return writeAdmin(stderr, "heimdall admin init: %v\n", err)
	}
	if err := createAdminConfig(path, answers); err != nil {
		return writeAdmin(stderr, "heimdall admin init: %v\n", err)
	}
	if writeAdmin(out, "Created %s (mode 0600). Install named secret files under %s; secret values are never accepted here.\n", path, answers.SecretsDir) != 0 {
		return 1
	}
	return writeAdmin(out, "Required secret files: %s, %s, %s, %s, %s, %s, %s, %s.\n", answers.DatabaseURLSecret, answers.JiraEmailSecret, answers.JiraTokenSecret, answers.SwitchAPIKeySecret, answers.SwitchRegistrationSecret, answers.GitHubAppIDSecret, answers.GitHubInstallationSecret, answers.GitHubPrivateKeySecret)
}

func setupSecretNames(cfg config.Config) []string {
	names := []string{
		cfg.Database.URLSecret, cfg.Jira.EmailSecret, cfg.Jira.APITokenSecret,
		cfg.Switch.APIKeySecret, cfg.Switch.RegistrationTokenSecret,
		cfg.GitHub.AppIDSecret, cfg.GitHub.InstallationIDSecret, cfg.GitHub.PrivateKeySecret,
		"postgres_password", "openrouter_api_key",
	}
	for _, name := range append([]string{cfg.Agents.TestAuthor}, append(cfg.Agents.Reviewers, cfg.Agents.Resolver)...) {
		if name != "" {
			names = append(names, "agent_token_"+name)
		}
	}
	filtered := names[:0]
	for _, name := range names {
		if name != "" {
			filtered = append(filtered, name)
		}
	}
	return slices.Compact(slices.Sorted(slices.Values(filtered)))
}

func writeAdmin(w io.Writer, format string, args ...any) int {
	if _, err := fmt.Fprintf(w, format, args...); err != nil {
		return 1
	}
	return 0
}

// adminConfigStatus returns 0 for an invalid existing file, 1 for a valid file, 2 when absent, and 3 on I/O errors.
func adminConfigStatus(path string) int {
	_, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return 2
	}
	if err != nil {
		return 3
	}
	if _, err := config.Load(path); err != nil {
		return 0
	}
	return 1
}

func createAdminConfig(path string, answers adminAnswers) error {
	data, err := encodeAdminConfig(answers)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	f, err := os.OpenFile(filepath.Clean(path), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create config: %w", err)
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("write config: %w", err)
	}
	if _, err := config.Load(path); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("generated config did not validate: %w", err)
	}
	return nil
}

type adminAnswers struct {
	Listen, SecretsDir, DatabaseURLSecret                                                                   string
	JiraURL, JiraProject, JiraEmailSecret, JiraTokenSecret                                                  string
	SwitchURL, SwitchAgentID, SwitchAgentName, SwitchBridgeID, SwitchAPIKeySecret, SwitchRegistrationSecret string
	GitHubAppIDSecret, GitHubInstallationSecret, GitHubPrivateKeySecret                                     string
	TestAuthor, Reviewer, Resolver, Repo, BaseBranch                                                        string
}

func collectAdminAnswers(reader *bufio.Reader, out io.Writer) (adminAnswers, error) {
	var a adminAnswers
	fields := []struct {
		label, fallback string
		dest            *string
	}{
		{"Listen address", "0.0.0.0:8080", &a.Listen},
		{"Secrets directory", "/etc/heimdall/secrets", &a.SecretsDir},
		{"Database URL secret filename", "database_url", &a.DatabaseURLSecret},
		{"Jira base URL", "https://jira.example.com", &a.JiraURL},
		{"Jira project key", "KAN", &a.JiraProject},
		{"Jira account email secret filename", "jira_email", &a.JiraEmailSecret},
		{"Jira API token secret filename", "jira_api_token", &a.JiraTokenSecret},
		{"Switch base URL", "http://127.0.0.1:8000", &a.SwitchURL},
		{"Switch coordinator agent id", "agent-coordinator", &a.SwitchAgentID},
		{"Switch coordinator agent name", "heimdall-coordinator", &a.SwitchAgentName},
		{"Switch bridge id", "bridge-chat", &a.SwitchBridgeID},
		{"Switch API key secret filename", "switch_api_key", &a.SwitchAPIKeySecret},
		{"Switch registration token secret filename", "switch_registration_token", &a.SwitchRegistrationSecret},
		{"GitHub App id secret filename", "github_app_id", &a.GitHubAppIDSecret},
		{"GitHub installation id secret filename", "github_app_installation_id", &a.GitHubInstallationSecret},
		{"GitHub private key secret filename", "github_app_private_key", &a.GitHubPrivateKeySecret},
		{"Test author agent name", "test-author", &a.TestAuthor},
		{"Reviewer agent name", "reviewer-1", &a.Reviewer},
		{"Resolver agent name", "resolver", &a.Resolver},
		{"GitHub repository owner/name", "owner/repository", &a.Repo},
		{"GitHub base branch", "main", &a.BaseBranch},
	}
	for _, field := range fields {
		if _, err := fmt.Fprintf(out, "%s [%s]: ", field.label, field.fallback); err != nil {
			return a, err
		}
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return a, err
		}
		value := strings.TrimSpace(line)
		if value == "" {
			value = field.fallback
		}
		*field.dest = value
		if errors.Is(err, io.EOF) && field != fields[len(fields)-1] {
			return a, errors.New("input ended before setup answers were complete")
		}
	}
	return a, nil
}

func encodeAdminConfig(a adminAnswers) ([]byte, error) {
	type project struct {
		TrackerProject string `toml:"tracker_project"`
		Repo           string `toml:"repo"`
		BaseBranch     string `toml:"base_branch"`
	}
	data := struct {
		Listen     string `toml:"listen"`
		SecretsDir string `toml:"secrets_dir"`
		Database   struct {
			URLSecret string `toml:"url_secret"`
		} `toml:"database"`
		Jira struct {
			BaseURL        string   `toml:"base_url"`
			ProjectKeys    []string `toml:"project_keys"`
			EmailSecret    string   `toml:"email_secret"`
			APITokenSecret string   `toml:"api_token_secret"`
		} `toml:"jira"`
		Switch struct {
			BaseURL                 string `toml:"base_url"`
			AgentID                 string `toml:"agent_id"`
			AgentName               string `toml:"agent_name"`
			BridgeID                string `toml:"bridge_id"`
			APIKeySecret            string `toml:"api_key_secret"`
			RegistrationTokenSecret string `toml:"registration_token_secret"`
		} `toml:"switch"`
		GitHub struct {
			AppIDSecret          string `toml:"app_id_secret"`
			InstallationIDSecret string `toml:"installation_id_secret"`
			PrivateKeySecret     string `toml:"private_key_secret"`
		} `toml:"github"`
		VCS struct {
			Provider string `toml:"provider"`
		} `toml:"vcs"`
		Tracker struct {
			Provider string `toml:"provider"`
		} `toml:"tracker"`
		Chat struct {
			Bridge string `toml:"bridge"`
		} `toml:"chat"`
		Agents struct {
			TestAuthor string   `toml:"test_author"`
			Reviewers  []string `toml:"reviewers"`
			Resolver   string   `toml:"resolver"`
		} `toml:"agents"`
		Projects []project `toml:"projects"`
	}{Listen: a.Listen, SecretsDir: a.SecretsDir}
	data.Database.URLSecret = a.DatabaseURLSecret
	data.Jira.BaseURL, data.Jira.ProjectKeys, data.Jira.EmailSecret, data.Jira.APITokenSecret = a.JiraURL, []string{a.JiraProject}, a.JiraEmailSecret, a.JiraTokenSecret
	data.Switch.BaseURL, data.Switch.AgentID, data.Switch.AgentName, data.Switch.BridgeID = a.SwitchURL, a.SwitchAgentID, a.SwitchAgentName, a.SwitchBridgeID
	data.Switch.APIKeySecret, data.Switch.RegistrationTokenSecret = a.SwitchAPIKeySecret, a.SwitchRegistrationSecret
	data.GitHub.AppIDSecret, data.GitHub.InstallationIDSecret, data.GitHub.PrivateKeySecret = a.GitHubAppIDSecret, a.GitHubInstallationSecret, a.GitHubPrivateKeySecret
	data.VCS.Provider, data.Tracker.Provider, data.Chat.Bridge = "github", "jira", "switch"
	data.Agents.TestAuthor, data.Agents.Reviewers, data.Agents.Resolver = a.TestAuthor, []string{a.Reviewer}, a.Resolver
	data.Projects = []project{{TrackerProject: a.JiraProject, Repo: a.Repo, BaseBranch: a.BaseBranch}}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
