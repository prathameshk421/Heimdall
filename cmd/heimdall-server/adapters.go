package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/github"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/registry"
	switchchat "github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/switch"
	filetracker "github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/tracker/file"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/tracker/jira"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/agentprovision"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport/agentsetup"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport/mcp"
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
	ports.PRCommentReader
	ports.CodeReader
	ports.ReviewReader
	ports.CheckReader
	ports.CheckWriter
	ports.PRComments
}

// adapters are the adapters the slice uses, selected by name through the registry.
type adapters struct {
	tickets           ports.TicketReader
	vcs               vcsPort
	chat              ports.Chat
	agents            config.Agents
	tokens            mcp.Tokens
	projects          projectRepos
	projectKeys       []string
	projectTickets    map[string]ports.TicketReader
	trackerProvider   string
	repos             []change.Repo
	testsRoom         change.RoomID
	reviewsRoom       change.RoomID
	standingRooms     []change.RoomID
	provisioner       ports.AgentProvisioner
	agentSetupHandler func(ports.AgentReceiptLedger) (http.Handler, func(context.Context) error, error)
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
func selectAdapters(cfg config.Config, d deps) (adapters, error) {
	projects, keys, repos, err := projectsOf(cfg)
	if err != nil {
		return adapters{}, err
	}
	f := adapterFactory{d: d, clk: clock.System{}, wait: clock.Wait, random: jitter{}, project: keys[0]}
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
	trackerProvider, _ := cfg.Selection(string(registry.KindTracker))
	out := adapters{projects: projects, projectKeys: keys, repos: repos, agents: cfg.Agents, trackerProvider: trackerProvider,
		testsRoom: change.RoomID(cfg.Rooms.Tests), reviewsRoom: change.RoomID(cfg.Rooms.Reviews), standingRooms: standingRoomsOf(cfg)}
	if out.projectTickets, err = projectTicketReaders(cfg, f, keys); err != nil {
		return adapters{}, err
	}
	// Ticket lookups by key use the first reader: the Jira reader's project only scopes the change query.
	out.tickets = out.projectTickets[keys[0]]
	if out.vcs, err = selected[vcsPort](cfg, reg, registry.KindVCS); err != nil {
		return adapters{}, err
	}
	if out.chat, err = selected[ports.Chat](cfg, reg, registry.KindChat); err != nil {
		return adapters{}, err
	}
	if err := selectAgentSetup(cfg, reg, &out); err != nil {
		return adapters{}, err
	}
	tokStore, err := newTokenStore(cfg)
	if err != nil {
		return adapters{}, err
	}
	out.tokens = tokStore
	return out, nil
}

func selectAgentSetup(cfg config.Config, reg *registry.Registry, out *adapters) error {
	if _, configured := cfg.Selection(string(registry.KindAgentRuntime)); !configured {
		return nil
	}
	provisioner, err := selected[ports.AgentProvisioner](cfg, reg, registry.KindAgentRuntime)
	if err != nil {
		return err
	}
	setup := func(receipts ports.AgentReceiptLedger) (http.Handler, func(context.Context) error, error) {
		service, err := agentprovision.New(agentprovision.Config{
			Provisioner: provisioner, Receipts: receipts, Secrets: cfg, Machine: cfg.AgentRuntime.Machine,
			MCPEndpoint: cfg.AgentRuntime.MCPEndpoint, Model: cfg.AgentRuntime.Model,
			SecretsDir: cfg.SecretsDir, AgentsDir: cfg.AgentRuntime.AgentsDir, Agents: provisionAgents(cfg.Agents),
		})
		if err != nil {
			return nil, nil, fmt.Errorf("agent_runtime config: %w", err)
		}
		handler, err := agentsetup.New(service, cfg, cfg.Switch.APIKeySecret)
		if err != nil {
			return nil, nil, fmt.Errorf("agent_runtime handoff: %w", err)
		}
		return handler, service.SyncDefinitions, nil
	}
	out.provisioner = provisioner
	out.agentSetupHandler = setup
	return nil
}

func provisionAgents(agents config.Agents) []agentprovision.Agent {
	out := []agentprovision.Agent{
		{Name: id.AgentName(agents.TestAuthor), Role: workflow.RoleTestAuthor, Description: "Heimdall " + agents.TestAuthor, Instructions: "Follow the installed test-author skill. Get tasks and submit results through Heimdall MCP tools."},
		{Name: id.AgentName(agents.Resolver), Role: workflow.RoleResolver, Description: "Heimdall resolver", Instructions: "Follow the installed resolver skill. Get tasks and submit results through Heimdall MCP tools."},
	}
	for _, name := range agents.Reviewers {
		out = append(out, agentprovision.Agent{Name: id.AgentName(name), Role: workflow.RoleReviewer, Description: "Heimdall " + name, Instructions: "Follow the installed reviewer skill. Get tasks and submit results through Heimdall MCP tools."})
	}
	return out
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

// standingRoomsOf lists the rooms that serve every open change besides the room it opened: the reviews channel and
// every feed room, so a command typed in any of them reaches its ticket.
func standingRoomsOf(cfg config.Config) []change.RoomID {
	var out []change.RoomID
	if cfg.Rooms.Tests != "" {
		out = append(out, change.RoomID(cfg.Rooms.Tests))
	}
	if cfg.Rooms.Reviews != "" {
		out = append(out, change.RoomID(cfg.Rooms.Reviews))
	}
	for _, f := range cfg.Feeds {
		out = append(out, change.RoomID(f.RoomID))
	}
	return out
}

// projectsOf maps each tracker project to its repository and lists the watched repositories. The slice polls one Jira
// project, because the Jira client reads one project and the cursor has one source; a config with more projects is refused.
func projectsOf(cfg config.Config) (projectRepos, []string, []change.Repo, error) {
	if len(cfg.Projects) == 0 {
		return nil, nil, nil, fmt.Errorf("projects: heimdall.toml maps no tracker project")
	}
	out := projectRepos{}
	var keys []string
	var repos []change.Repo
	for _, p := range cfg.Projects {
		repo, err := change.ParseRepo(p.Repo)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("projects %s: %w", p.TrackerProject, err)
		}
		if _, dup := out[p.TrackerProject]; dup {
			return nil, nil, nil, fmt.Errorf("projects %s: mapped twice", p.TrackerProject)
		}
		out[p.TrackerProject] = repo
		keys = append(keys, p.TrackerProject)
		repos = append(repos, repo)
	}
	return out, keys, repos, nil
}

// baseBranchesOf maps each project's repository to the base branch the project names, so a change is cut from that
// branch and its policy is read there.
func baseBranchesOf(cfg config.Config) map[change.Repo]string {
	out := make(map[change.Repo]string, len(cfg.Projects))
	for _, p := range cfg.Projects {
		repo, err := change.ParseRepo(p.Repo)
		if err != nil || p.BaseBranch == "" {
			continue
		}
		out[repo] = p.BaseBranch
	}
	return out
}

// projectTicketReaders builds one tracker reader per project, so each project's poll queries only that project
// (A10 item 10).
func projectTicketReaders(cfg config.Config, f adapterFactory, keys []string) (map[string]ports.TicketReader, error) {
	if provider, _ := cfg.Selection(string(registry.KindTracker)); provider == "file" && len(keys) != 1 {
		return nil, fmt.Errorf("projects: the file tracker polls one project, but heimdall.toml maps %d", len(keys))
	}
	out := make(map[string]ports.TicketReader, len(keys))
	for _, key := range keys {
		fk := f
		fk.project = key
		reg, err := registry.Default(fk.entries())
		if err != nil {
			return nil, fmt.Errorf("register adapters for %s: %w", key, err)
		}
		t, err := selected[ports.TicketReader](cfg, reg, registry.KindTracker)
		if err != nil {
			return nil, err
		}
		out[key] = t
	}
	return out, nil
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
		{Name: "file", Kind: registry.KindTracker,
			RequiredConfig: []string{"tracker.path"},
			Factory:        f.fileTracker, Check: notChecked},
		{Name: "switch", Kind: registry.KindChat,
			RequiredConfig: []string{switchAPIKeyFile, switchRegistrationFile},
			Factory:        f.switchChat, Check: notChecked},
		{Name: "switchroom", Kind: registry.KindAgentRuntime,
			RequiredConfig: []string{switchAPIKeyFile, switchRegistrationFile}, Factory: f.switchRuntime, Check: notChecked},
	}
}

func (f adapterFactory) fileTracker(cfg config.Config) (any, error) {
	a, err := filetracker.New(cfg.TrackerPath, f.clk)
	if err != nil {
		return nil, fmt.Errorf("file tracker: %w", err)
	}
	return a, nil
}

func (f adapterFactory) switchRuntime(cfg config.Config) (any, error) { return f.switchChat(cfg) }

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

		BaseBranches: baseBranchesOf(cfg),
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
		InternalRooms:      cfg.Rooms.Tests != "",
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
