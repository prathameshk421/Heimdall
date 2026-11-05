package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/localrunner"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/policyyaml"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/postgres"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/agenttools"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/effects"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/engine"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/gateuse"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/intake"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/outbox"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/prsync"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/trigger"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/ids"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport/mcp"
)

// Settings the slice reads from the config or names here. The config has no key for a poll interval or an outbox
// lease, so they are named constants. The config has no key for the endpoints of the adapters or for the database
// either; productionDeps refuses to start until those keys exist in heimdall.toml.
const (
	// pollInterval is the wait between two ticket polls.
	pollInterval = 15 * time.Second
	// syncInterval is the wait between two pull request syncs.
	syncInterval = 10 * time.Second
	// drainTick is the longest the outbox waits between two batches when no wake arrives.
	drainTick = 2 * time.Second
	// maxAttempts is the task budget of a retry (engine.Config.MaxAttempts). The slice has no retry, so it is a bound only.
	maxAttempts = 3
	// outboxBatch is how many effects one drain claims at once.
	outboxBatch = 8
	// effectTimeout bounds one executor call. The lease must exceed outboxBatch times this (outbox.New).
	effectTimeout = 20 * time.Second
	// outboxLease is how long a claimed effect stays leased to one drainer.
	outboxLease = 3 * time.Minute
	// githubAPIRoot is the production GitHub API, which is not site-specific and so needs no config key.
	githubAPIRoot = "https://api.github.com"
	// cursorSourcePrefix names the Jira poll cursor: one source per tracker project.
	cursorSourcePrefix = "jira:"
)

// deps is what build needs that differs between production and a test: the database, the adapter endpoints, the HTTP
// client and the timing. Every field is required. openPool and migrate are injected so a test can prove the wiring
// without a live Postgres; productionDeps fills them with the real pgxpool and migration functions.
type deps struct {
	databaseURL string
	endpoints   endpoints
	httpClient  *http.Client
	logger      *slog.Logger
	pollEvery   time.Duration
	syncEvery   time.Duration
	drainTick   time.Duration
	openPool    func(ctx context.Context, url string) (*pgxpool.Pool, error)
	migrate     func(ctx context.Context, pool *pgxpool.Pool) error
}

// String implements fmt.Stringer, redacting the database URL.
func (d deps) String() string {
	return d.redactedFormat(false, false)
}

// GoString implements fmt.GoStringer, redacting the database URL.
func (d deps) GoString() string {
	return d.redactedFormat(false, true)
}

// Format implements fmt.Formatter, redacting the database URL across %v, %+v, %#v, and %s.
func (d deps) Format(f fmt.State, verb rune) {
	switch verb {
	case 'v':
		plus := f.Flag('+')
		sharp := f.Flag('#')
		_, _ = io.WriteString(f, d.redactedFormat(plus, sharp))
	case 's':
		_, _ = io.WriteString(f, d.String())
	case 'q':
		_, _ = fmt.Fprintf(f, "%q", d.String())
	default:
		_, _ = fmt.Fprintf(f, "%%!%c(main.deps=%s)", verb, d.String())
	}
}

// LogValue implements slog.LogValuer, redacting the database URL.
func (d deps) LogValue() slog.Value {
	dbVal := ""
	if d.databaseURL != "" {
		dbVal = "[redacted]"
	}
	return slog.GroupValue(
		slog.String("databaseURL", dbVal),
		slog.Any("endpoints", d.endpoints),
		slog.Duration("pollEvery", d.pollEvery),
		slog.Duration("syncEvery", d.syncEvery),
		slog.Duration("drainTick", d.drainTick),
	)
}

func (d deps) redactedFormat(plus, sharp bool) string {
	redactedURL := ""
	if d.databaseURL != "" {
		redactedURL = "[redacted]"
	}
	openPoolStr := "<nil>"
	if d.openPool != nil {
		openPoolStr = "<func>"
	}
	migrateStr := "<nil>"
	if d.migrate != nil {
		migrateStr = "<func>"
	}
	if sharp {
		return fmt.Sprintf("main.deps{databaseURL:%q, endpoints:%#v, httpClient:%#v, logger:%#v, pollEvery:%#v, syncEvery:%#v, drainTick:%#v, openPool:%s, migrate:%s}",
			redactedURL, d.endpoints, d.httpClient, d.logger, d.pollEvery, d.syncEvery, d.drainTick, openPoolStr, migrateStr)
	}
	if plus {
		return fmt.Sprintf("{databaseURL:%s endpoints:%+v httpClient:%+v logger:%+v pollEvery:%+v syncEvery:%+v drainTick:%+v openPool:%s migrate:%s}",
			redactedURL, d.endpoints, d.httpClient, d.logger, d.pollEvery, d.syncEvery, d.drainTick, openPoolStr, migrateStr)
	}
	return fmt.Sprintf("{%s %v %v %v %v %v %v %s %s}",
		redactedURL, d.endpoints, d.httpClient, d.logger, d.pollEvery, d.syncEvery, d.drainTick, openPoolStr, migrateStr)
}

// endpoints are the base URLs and the Switch identities of the adapters. A test points them at httptest servers.
type endpoints struct {
	jiraBaseURL     string
	githubAPI       string
	switchBaseURL   string
	switchAgentID   string
	switchAgentName string
	switchBridgeID  string
}

// app is the running slice: the ticket poller, the outbox drainer and the database they share.
type app struct {
	pool      *pgxpool.Pool
	poller    poller
	drainer   drainer
	syncer    syncer
	logger    *slog.Logger
	clock     ports.Clock
	pollEvery time.Duration
	syncEvery time.Duration
	drainTick time.Duration
}

// poller is the part of trigger.Poller the loop uses. A test supplies a fake one.
type poller interface {
	PollOnce(ctx context.Context) (trigger.PollResult, error)
}

// drainer is the part of outbox.Drainer the loop uses. A test supplies a fake one.
type drainer interface {
	Run(ctx context.Context, wake <-chan struct{}, tick <-chan time.Time) error
}

// syncer is the part of prsync.Syncer the loop uses. A test supplies a fake one.
type syncer interface {
	SyncOnce(ctx context.Context) (prsync.Result, error)
}

// close releases the database pool. Call it after the loops have stopped.
func (a *app) close() {
	a.pool.Close()
}

// openApp builds the slice when the config selects a slice provider, and returns nil when it selects none. A server
// with no provider serves /healthz only, as before the slice landed.
func openApp(ctx context.Context, cfg config.Config, logger *slog.Logger) (*app, error) {
	if !selectsSlice(cfg) {
		return nil, nil
	}
	d, err := productionDeps(cfg, logger)
	if err != nil {
		return nil, err
	}
	return build(ctx, cfg, d)
}

// selectsSlice reports whether the config names a provider of the slice.
func selectsSlice(cfg config.Config) bool {
	for _, kind := range sliceKinds() {
		if _, ok := cfg.Selection(string(kind)); ok {
			return true
		}
	}
	return false
}

// productionDeps reads what the slice needs from the config. It names every missing key at once, then reads the
// database URL through its secret file name and checks that every credential file the adapters read is present and
// usable. The secret values never enter an error: each message names the key and the file only.
func productionDeps(cfg config.Config, logger *slog.Logger) (deps, error) {
	if missing := missingConfigKeys(cfg); len(missing) > 0 {
		return deps{}, fmt.Errorf("heimdall.toml selects a slice provider but has no key for: %s", strings.Join(missing, ", "))
	}
	if err := checkProjectSelection(cfg); err != nil {
		return deps{}, err
	}
	databaseURL, err := cfg.Secret(cfg.Database.URLSecret)
	if err != nil {
		return deps{}, fmt.Errorf("database.url_secret: %w", err)
	}
	// The URL is a secret. Parse it here so a typo fails at boot, but never include it (or pgx's message, which
	// may quote it) in the error.
	if _, err := pgxpool.ParseConfig(databaseURL); err != nil {
		return deps{}, fmt.Errorf("database.url_secret: secret %q is not a Postgres URL", cfg.Database.URLSecret)
	}
	if err := checkAdapterSecrets(cfg); err != nil {
		return deps{}, err
	}
	return deps{
		databaseURL: databaseURL,
		httpClient:  &http.Client{},
		logger:      logger,
		endpoints: endpoints{
			jiraBaseURL:     cfg.Jira.BaseURL,
			githubAPI:       githubAPIRoot,
			switchBaseURL:   cfg.Switch.BaseURL,
			switchAgentID:   cfg.Switch.AgentID,
			switchAgentName: cfg.Switch.AgentName,
			switchBridgeID:  cfg.Switch.BridgeID,
		},
		pollEvery: pollInterval,
		syncEvery: syncInterval,
		drainTick: drainTick,
		openPool:  pgxpool.New,
		migrate:   postgres.Migrate,
	}, nil
}

// missingConfigKeys lists every key the slice needs that the config does not set, in a stable order. A key is
// missing when the section is absent or its value is empty; Load already refuses an empty value in a present
// section, so this mainly reports absent sections.
func missingConfigKeys(cfg config.Config) []string {
	var missing []string
	add := func(key, value string) {
		if value == "" {
			missing = append(missing, key)
		}
	}
	add("database.url_secret", cfg.Database.URLSecret)
	add("jira.base_url", cfg.Jira.BaseURL)
	if len(cfg.Jira.ProjectKeys) == 0 {
		missing = append(missing, "jira.project_keys")
	}
	add("jira.email_secret", cfg.Jira.EmailSecret)
	add("jira.api_token_secret", cfg.Jira.APITokenSecret)
	add("switch.base_url", cfg.Switch.BaseURL)
	add("switch.agent_id", cfg.Switch.AgentID)
	add("switch.agent_name", cfg.Switch.AgentName)
	add("switch.bridge_id", cfg.Switch.BridgeID)
	add("switch.api_key_secret", cfg.Switch.APIKeySecret)
	add("switch.registration_token_secret", cfg.Switch.RegistrationTokenSecret)
	add("github.app_id_secret", cfg.GitHub.AppIDSecret)
	add("github.installation_id_secret", cfg.GitHub.InstallationIDSecret)
	add("github.private_key_secret", cfg.GitHub.PrivateKeySecret)
	return missing
}

// checkProjectSelection requires one Jira project key and requires it to be the one project the slice maps. The
// poller uses one cursor source and one repository mapping, so a mismatch or a second key would poll a project it
// cannot start.
func checkProjectSelection(cfg config.Config) error {
	if len(cfg.Jira.ProjectKeys) != 1 {
		return fmt.Errorf("jira.project_keys: the slice polls one tracker project, but heimdall.toml lists %d", len(cfg.Jira.ProjectKeys))
	}
	if len(cfg.Projects) != 1 {
		return fmt.Errorf("projects: the slice polls one tracker project, but heimdall.toml maps %d", len(cfg.Projects))
	}
	if cfg.Jira.ProjectKeys[0] != cfg.Projects[0].TrackerProject {
		return fmt.Errorf("jira.project_keys: %q is not the project mapped by projects, which maps %q", cfg.Jira.ProjectKeys[0], cfg.Projects[0].TrackerProject)
	}
	return nil
}

// checkAdapterSecrets reads every credential file the adapters read once, at boot. A missing file, a multi-line
// token, a world-readable private key or a non-integer App id is refused here, with the key and file named and no
// value in the message. The adapters read the same files again when they are built.
func checkAdapterSecrets(cfg config.Config) error {
	single := []struct{ field, name string }{
		{"jira.email_secret", cfg.Jira.EmailSecret},
		{"jira.api_token_secret", cfg.Jira.APITokenSecret},
		{"switch.api_key_secret", cfg.Switch.APIKeySecret},
		{"switch.registration_token_secret", cfg.Switch.RegistrationTokenSecret},
	}
	for _, s := range single {
		if _, err := cfg.Secret(s.name); err != nil {
			return fmt.Errorf("%s: %w", s.field, err)
		}
	}
	if _, err := cfg.SecretMultiline(cfg.GitHub.PrivateKeySecret); err != nil {
		return fmt.Errorf("github.private_key_secret: %w", err)
	}
	for _, s := range []struct{ field, name string }{
		{"github.app_id_secret", cfg.GitHub.AppIDSecret},
		{"github.installation_id_secret", cfg.GitHub.InstallationIDSecret},
	} {
		if _, err := secretInt(cfg, s.name); err != nil {
			return fmt.Errorf("%s: %w", s.field, err)
		}
	}
	return nil
}

// validate refuses a deps value that cannot run. It names each field.
func (d deps) validate() error {
	var problems []string
	if d.databaseURL == "" {
		problems = append(problems, "database url is empty")
	}
	if d.httpClient == nil {
		problems = append(problems, "http client is nil")
	}
	if d.logger == nil {
		problems = append(problems, "logger is nil")
	}
	if d.pollEvery <= 0 {
		problems = append(problems, "poll interval must be positive")
	}
	if d.syncEvery <= 0 {
		problems = append(problems, "sync interval must be positive")
	}
	if d.drainTick <= 0 {
		problems = append(problems, "drain tick must be positive")
	}
	if d.openPool == nil {
		problems = append(problems, "database opener is nil")
	}
	if d.migrate == nil {
		problems = append(problems, "migration function is nil")
	}
	if len(problems) > 0 {
		return fmt.Errorf("server deps: %s", strings.Join(problems, "; "))
	}
	return nil
}

// build validates the config against the adapters, opens the database, and assembles the slice. It refuses a missing
// or unknown provider before it opens any connection, and it names the field it refuses.
func build(ctx context.Context, cfg config.Config, d deps) (*app, error) {
	if err := d.validate(); err != nil {
		return nil, err
	}
	clk := clock.System{}
	ad, err := selectAdapters(cfg, d, clk)
	if err != nil {
		return nil, err
	}
	pool, err := d.openPool(ctx, d.databaseURL)
	if err != nil {
		msg := err.Error()
		if d.databaseURL != "" && strings.Contains(msg, d.databaseURL) {
			msg = strings.ReplaceAll(msg, d.databaseURL, "[redacted]")
		}
		return nil, fmt.Errorf("open database: %s", msg)
	}
	a, err := assemble(ctx, d, ad, pool, clk)
	if err != nil {
		pool.Close()
		return nil, err
	}
	return a, nil
}

// assemble migrates the database and wires the engine, the executors, the poller and the drainer on it.
func assemble(ctx context.Context, d deps, ad adapters, pool *pgxpool.Pool, clk ports.Clock) (*app, error) {
	if err := d.migrate(ctx, pool); err != nil {
		return nil, fmt.Errorf("migrate database: %w", err)
	}
	store := postgres.NewStore(pool)
	gen := ids.New()
	wf, steps, err := sliceSteps()
	if err != nil {
		return nil, err
	}
	eng, err := engine.New(engine.Config{
		Workflow: wf, Steps: steps, Store: store, Clock: clk, IDs: gen, MaxAttempts: maxAttempts,
	})
	if err != nil {
		return nil, fmt.Errorf("build engine: %w", err)
	}
	execs, err := executors(ad, store, ingestor{eng: eng}, gen, clk)
	if err != nil {
		return nil, err
	}
	box, err := outbox.New(outbox.Config{
		Outbox: store, Executors: execs, Logger: d.logger,
		Batch: outboxBatch, Lease: outboxLease, EffectTimeout: effectTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("build outbox drainer: %w", err)
	}
	p, err := trigger.New(trigger.Config{
		Tickets:  ad.tickets,
		Starter:  starter{eng: eng},
		Cursors:  cursorStore{table: store, source: cursorSourcePrefix + ad.project},
		Projects: ad.projects,
		Clock:    clk,
		IDs:      gen,
	})
	if err != nil {
		return nil, fmt.Errorf("build ticket poller: %w", err)
	}
	sync, err := assembleSyncer(ad, store, gen, clk)
	if err != nil {
		return nil, err
	}
	return &app{
		pool: pool, poller: p, drainer: box, syncer: sync, logger: d.logger, clock: clk,
		pollEvery: d.pollEvery, syncEvery: d.syncEvery, drainTick: d.drainTick,
	}, nil
}

// assembleSyncer wires the policy decoder, gate source, evaluator, intake and PR syncer.
func assembleSyncer(ad adapters, store ports.EventStore, gen ports.IDs, clk ports.Clock) (*prsync.Syncer, error) {
	gateSrc, err := gateuse.NewSource(gateuse.SourceConfig{
		PRs:    ad.vcs,
		Code:   ad.vcs,
		Decode: policyyaml.Decode,
	})
	if err != nil {
		return nil, fmt.Errorf("build gate source: %w", err)
	}
	gateEval, err := gateuse.New(gateuse.Config{
		Events: store,
		Source: gateSrc,
		Clock:  clk,
		IDs:    gen,
	})
	if err != nil {
		return nil, fmt.Errorf("build gate evaluator: %w", err)
	}
	intakeCase, err := intake.New(intake.Config{
		Events: store,
		PRs:    ad.vcs,
		Code:   ad.vcs,
		Clock:  clk,
		IDs:    gen,
		Policy: policyyaml.Decode,
	})
	if err != nil {
		return nil, fmt.Errorf("build intake: %w", err)
	}
	sync, err := prsync.New(prsync.Config{
		Repos:   ad.repos,
		PRs:     ad.vcs,
		Intake:  prIntake{in: intakeCase},
		Reviews: reviewsAdapter{reader: ad.vcs},
		Checks:  checksAdapter{reader: ad.vcs},
		Gate:    prGate{eval: gateEval},
		Events:  store,
		Clock:   clk,
		IDs:     gen,
	})
	if err != nil {
		return nil, fmt.Errorf("build pr syncer: %w", err)
	}
	return sync, nil
}

// executors maps each effect kind to its executor. Each executor checks the kind of the effect it receives, so a
// swapped entry fails every effect it is given.
func executors(ad adapters, store ports.EventStore, in effects.Ingest, gen ports.IDs, clk ports.Clock) (map[change.EffectKind]outbox.Executor, error) {
	room, err := effects.NewRoomOpen(effects.RoomOpenDeps{
		Tickets: ad.tickets, Chat: ad.chat, Store: store, Ingest: in, IDs: gen, Clock: clk,
	})
	if err != nil {
		return nil, fmt.Errorf("build room_open executor: %w", err)
	}
	branch, err := effects.NewGitHubBranch(effects.GitHubBranchDeps{
		Tickets: ad.tickets, Base: ad.vcs, Branches: ad.vcs, Store: store, Ingest: in, IDs: gen, Clock: clk,
	})
	if err != nil {
		return nil, fmt.Errorf("build github_branch executor: %w", err)
	}
	check, err := effects.NewGitHubCheck(effects.GitHubCheckDeps{
		Checks: ad.vcs,
	})
	if err != nil {
		return nil, fmt.Errorf("build github_check executor: %w", err)
	}
	return map[change.EffectKind]outbox.Executor{
		change.EffectRoomOpen:     room,
		change.EffectGitHubBranch: branch,
		change.EffectGitHubCheck:  check,
	}, nil
}
