package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/github"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/registry"
	switchchat "github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/switch"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/tracker/jira"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Secret names the adapters read. They are the files under secrets_dir (D89), named as in the design notes.
const (
	githubAppIDFile        = "github_app_id"
	githubInstallationFile = "github_app_installation_id"
	githubKeyFile          = "github_app_private_key"
	jiraEmailFile          = "jira_email"
	jiraAPIFile            = "jira_api_token"
	switchAPIKeyFile       = "switch_api_key"
	switchRegistrationFile = "switch_registration_token"
)

// vcsPort is what the VCS adapter must do for the slice: write branches, read repos, PRs, code, reviews and checks.
type vcsPort interface {
	ports.BranchWriter
	ports.RepoReader
	ports.PRReader
	ports.CodeReader
	ports.ReviewReader
	ports.CheckReader
	ports.CheckWriter
}

// adapters are the adapters the slice uses, selected by name through the registry.
type adapters struct {
	tickets  ports.TicketReader
	vcs      vcsPort
	chat     ports.Chat
	projects projectRepos
	project  string
	repos    []change.Repo
}

// projectRepos maps a tracker project key to its repository (trigger.Projects).
type projectRepos map[string]change.Repo

// RepoFor returns the repository of a tracker project.
func (p projectRepos) RepoFor(project string) (change.Repo, bool) {
	r, ok := p[project]
	return r, ok
}

// sliceKinds are the kinds the slice needs. Each one must have a selection in heimdall.toml.
func sliceKinds() []registry.Kind {
	return []registry.Kind{registry.KindVCS, registry.KindTracker, registry.KindChat}
}

// adapterFactory builds the registered adapters. The registry calls its factories with the config only, so the
// endpoints, the clock and the HTTP client are captured here.
type adapterFactory struct {
	d       deps
	clk     ports.Clock
	wait    func(context.Context, time.Duration) error
	random  jira.Random
	project string
}

// selectAdapters checks every slice selection against the registry and builds the adapters it names. It refuses a
// missing or unknown selection before any adapter is built, and it names the heimdall.toml key of each problem.
func selectAdapters(cfg config.Config, d deps, clk ports.Clock) (adapters, error) {
	projects, project, repos, err := projectsOf(cfg)
	if err != nil {
		return adapters{}, err
	}
	f := adapterFactory{d: d, clk: clk, wait: clock.Wait, random: jitter{}, project: project}
	reg, err := registry.Default(f.entries())
	if err != nil {
		return adapters{}, fmt.Errorf("register adapters: %w", err)
	}
	var problems []error
	for _, kind := range sliceKinds() {
		if _, ok := cfg.Selection(string(kind)); !ok {
			problems = append(problems, fmt.Errorf("%s: no provider is selected", registry.SelectionField(kind)))
		}
	}
	if err := registry.ValidateSelections(cfg, reg); err != nil {
		problems = append(problems, err)
	}
	if len(problems) > 0 {
		return adapters{}, fmt.Errorf("heimdall.toml: %w", errors.Join(problems...))
	}
	out := adapters{projects: projects, project: project, repos: repos}
	if out.tickets, err = selected[ports.TicketReader](cfg, reg, registry.KindTracker); err != nil {
		return adapters{}, err
	}
	if out.vcs, err = selected[vcsPort](cfg, reg, registry.KindVCS); err != nil {
		return adapters{}, err
	}
	if out.chat, err = selected[ports.Chat](cfg, reg, registry.KindChat); err != nil {
		return adapters{}, err
	}
	return out, nil
}

// selected builds the adapter the config selects for kind and asserts it to the port the slice needs.
func selected[T any](cfg config.Config, reg *registry.Registry, kind registry.Kind) (T, error) {
	var zero T
	name, _ := cfg.Selection(string(kind))
	e, err := reg.Select(kind, name)
	if err != nil {
		return zero, fmt.Errorf("%s: %w", registry.SelectionField(kind), err)
	}
	v, err := e.Factory(cfg)
	if err != nil {
		return zero, fmt.Errorf("%s %q: %w", kind, name, err)
	}
	port, ok := v.(T)
	if !ok {
		return zero, fmt.Errorf("%s %q does not implement the port the slice needs", kind, name)
	}
	return port, nil
}

// projectsOf maps each tracker project to its repository and lists the watched repositories. The slice polls one Jira
// project, because the Jira client reads one project and the cursor has one source; a config with more projects is refused.
func projectsOf(cfg config.Config) (projectRepos, string, []change.Repo, error) {
	if len(cfg.Projects) != 1 {
		return nil, "", nil, fmt.Errorf("the slice polls one tracker project, but heimdall.toml maps %d", len(cfg.Projects))
	}
	out := projectRepos{}
	var repos []change.Repo
	for _, p := range cfg.Projects {
		repo, err := change.ParseRepo(p.Repo)
		if err != nil {
			return nil, "", nil, fmt.Errorf("projects %s: %w", p.TrackerProject, err)
		}
		out[p.TrackerProject] = repo
		repos = append(repos, repo)
	}
	return out, cfg.Projects[0].TrackerProject, repos, nil
}

// entries lists the adapters the server knows, by kind and name.
func (f adapterFactory) entries() []registry.Entry {
	return []registry.Entry{
		{Name: "github", Kind: registry.KindVCS,
			RequiredConfig: []string{githubAppIDFile, githubInstallationFile, githubKeyFile},
			Factory:        f.github, Check: notChecked},
		{Name: "jira", Kind: registry.KindTracker,
			RequiredConfig: []string{jiraEmailFile, jiraAPIFile},
			Factory:        f.jira, Check: notChecked},
		{Name: "switch", Kind: registry.KindChat,
			RequiredConfig: []string{switchAPIKeyFile, switchRegistrationFile},
			Factory:        f.switchChat, Check: notChecked},
	}
}

// notChecked is the Check of every entry. The server does not run the connect checks; heimdall doctor does.
func notChecked(context.Context, config.Config) registry.CheckResult {
	return registry.CheckResult{Status: registry.StatusNeedsConfig, Detail: "the server does not run connect checks"}
}

func (f adapterFactory) github(cfg config.Config) (any, error) {
	appID, err := secretInt(cfg, cfg.GitHub.AppIDSecret)
	if err != nil {
		return nil, err
	}
	installation, err := secretInt(cfg, cfg.GitHub.InstallationIDSecret)
	if err != nil {
		return nil, err
	}
	key, err := cfg.SecretMultiline(cfg.GitHub.PrivateKeySecret)
	if err != nil {
		return nil, fmt.Errorf("github app key: %w", err)
	}
	a, err := github.New(github.Config{
		BaseURL:    f.d.endpoints.githubAPI,
		App:        github.App{AppID: appID, InstallationID: installation, PrivateKeyPEM: key},
		HTTPClient: f.d.httpClient,
		Clock:      f.clk,
		Wait:       f.wait,
	})
	if err != nil {
		return nil, fmt.Errorf("github adapter: %w", err)
	}
	return a, nil
}

func (f adapterFactory) jira(cfg config.Config) (any, error) {
	c, err := jira.New(jira.Config{
		BaseURL:     f.d.endpoints.jiraBaseURL,
		Project:     f.project,
		EmailSecret: cfg.Jira.EmailSecret,
		TokenSecret: cfg.Jira.APITokenSecret,
	}, cfg, f.clk, f.wait, f.random, f.d.httpClient)
	if err != nil {
		return nil, fmt.Errorf("jira adapter: %w", err)
	}
	return c, nil
}

func (f adapterFactory) switchChat(cfg config.Config) (any, error) {
	c, err := switchchat.New(switchchat.Config{
		BaseURL:            f.d.endpoints.switchBaseURL,
		AgentID:            f.d.endpoints.switchAgentID,
		AgentName:          f.d.endpoints.switchAgentName,
		BridgeID:           f.d.endpoints.switchBridgeID,
		Secrets:            cfg,
		APIKeySecret:       cfg.Switch.APIKeySecret,
		RegistrationSecret: cfg.Switch.RegistrationTokenSecret,
		HTTPClient:         f.d.httpClient,
		Clock:              f.clk,
		Wait:               f.wait,
	})
	if err != nil {
		return nil, fmt.Errorf("switch adapter: %w", err)
	}
	return c, nil
}

// secretInt reads a secret that holds an integer. The error names the secret and never its value.
func secretInt(cfg config.Config, name string) (int64, error) {
	raw, err := cfg.Secret(name)
	if err != nil {
		return 0, err
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("secret %q: not an integer", name)
	}
	return n, nil
}
