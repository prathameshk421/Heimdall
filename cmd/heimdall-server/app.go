package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/blobfs"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/graphiti"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/policyyaml"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/postgres"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/profileyaml"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/rootlessrunner"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/agenttools"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/collaboration"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/effects"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/engine"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/gateuse"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/intake"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/knowledge"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/outbox"
	appprofile "github.com/prathameshk421/flip_the_switch_hackathon/internal/app/profile"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/prsync"
	appreview "github.com/prathameshk421/flip_the_switch_hackathon/internal/app/review"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/roomstream"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/trigger"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
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
	// blobStoreRoot is the on-disk blob store until heimdall.toml gains a blobs path key.
	blobStoreRoot = ".heimdall/blobs"
	// githubAPIRoot is the production GitHub API, which is not site-specific and so needs no config key.
	githubAPIRoot = "https://api.github.com"
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
	pool              *pgxpool.Pool
	poller            poller
	drainer           drainer
	syncer            syncer
	roomStream        roomStreamRunner
	comments          commentPoller
	mcpHandler        http.Handler
	agentSetupHandler http.Handler
	episodeWorker     interface {
		RunOnce(context.Context, int) (int, error)
	}
	blobs     io.Closer
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

type roomStreamRunner interface{ Run(context.Context) error }
type commentPoller interface {
	PollOnce(context.Context) (collaboration.CommentPollResult, error)
}

// close releases the blob store and the database pool. Call it after the loops have stopped.
func (a *app) close() {
	if a.blobs != nil {
		_ = a.blobs.Close()
	}
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
	tracker, _ := cfg.Selection("tracker")
	switch tracker {
	case "jira":
		add("jira.base_url", cfg.Jira.BaseURL)
		if len(cfg.Jira.ProjectKeys) == 0 {
			missing = append(missing, "jira.project_keys")
		}
		add("jira.email_secret", cfg.Jira.EmailSecret)
		add("jira.api_token_secret", cfg.Jira.APITokenSecret)
	case "file":
		add("tracker.path", cfg.TrackerPath)
	default:
		missing = append(missing, "tracker.provider")
	}
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
	tracker, _ := cfg.Selection("tracker")
	if tracker != "jira" {
		if tracker == "file" && len(cfg.Projects) != 1 {
			return fmt.Errorf("projects: the slice polls one tracker project, but heimdall.toml maps %d", len(cfg.Projects))
		}
		return nil
	}
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
		{"switch.api_key_secret", cfg.Switch.APIKeySecret},
		{"switch.registration_token_secret", cfg.Switch.RegistrationTokenSecret},
	}
	if tracker, _ := cfg.Selection("tracker"); tracker == "jira" {
		single = append(single, struct{ field, name string }{"jira.email_secret", cfg.Jira.EmailSecret}, struct{ field, name string }{"jira.api_token_secret", cfg.Jira.APITokenSecret})
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
	ad, err := selectAdapters(cfg, d)
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
	var setupHandler http.Handler
	if ad.agentSetupHandler != nil {
		var err error
		setupHandler, err = ad.agentSetupHandler(store)
		if err != nil {
			return nil, err
		}
	}
	gen := ids.New()
	wf, steps, err := sliceSteps(ad.agents, gen, clk)
	if err != nil {
		return nil, err
	}
	eng, err := engine.New(engine.Config{
		Workflow: wf, Steps: steps, Store: store, Clock: clk, IDs: gen, MaxAttempts: maxAttempts,
		Roles: sliceRoles(), Schemas: sliceSchemas(),
	})
	if err != nil {
		return nil, fmt.Errorf("build engine: %w", err)
	}
	blobStore, err := blobfs.New(blobStoreRoot)
	if err != nil {
		return nil, fmt.Errorf("open blob store: %w", err)
	}
	a, err := assembleWithBlobs(ctx, d, ad, store, pool, blobStore, eng, gen, clk, setupHandler)
	if err != nil {
		_ = blobStore.Close()
		return nil, err
	}
	return a, nil
}

func assembleWithBlobs(
	ctx context.Context, d deps, ad adapters, store *postgres.Store, pool *pgxpool.Pool, blobs ports.BlobStore,
	eng *engine.Engine, gen ports.IDs, clk ports.Clock, setupHandler http.Handler,
) (*app, error) {
	profileManager, err := assembleProfileLifecycle(ad, store, blobs, clk)
	if err != nil {
		return nil, err
	}
	execs, err := executors(ctx, ad, store, store, blobs, ingestor{eng: eng}, gen, clk)
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
		Cursors:  cursorStore{table: store, source: ad.trackerProvider + ":" + ad.project},
		Projects: ad.projects,
		Blobs:    blobs,
		Clock:    clk,
		IDs:      gen,
	})
	if err != nil {
		return nil, fmt.Errorf("build ticket poller: %w", err)
	}
	sync, intakeCase, err := assembleSyncer(ad, store, gen, clk)
	if err != nil {
		return nil, err
	}
	roomConsumer, commentConsumer, err := assembleCollaboration(store, ad, eng, gen, clk, d.syncEvery, intakeCase, profileManager)
	if err != nil {
		return nil, err
	}
	files, episodeWorker, err := assembleKnowledgeRetention(d, ad, store, blobs, clk)
	if err != nil {
		return nil, err
	}
	mcpH, err := assembleMCP(d, ad, store, pool, blobs, eng, gen, clk, files, profileManager)
	if err != nil {
		return nil, err
	}
	var closer io.Closer
	if c, ok := blobs.(io.Closer); ok {
		closer = c
	}
	return &app{
		pool: pool, poller: p, drainer: box, syncer: sync, roomStream: roomConsumer, comments: commentConsumer, mcpHandler: mcpH, agentSetupHandler: setupHandler, episodeWorker: episodeWorker, blobs: closer,
		logger: d.logger, clock: clk,
		pollEvery: d.pollEvery, syncEvery: d.syncEvery, drainTick: d.drainTick,
	}, nil
}

func assembleCollaboration(store *postgres.Store, ad adapters, eng *engine.Engine, gen ports.IDs, clk ports.Clock, refreshEvery time.Duration, intakeCase *intake.Intake, profiles mcp.Profiles) (roomStreamRunner, commentPoller, error) {
	runtime, err := newCommandRuntime(store, ad, eng, gen, clk, prIntake{in: intakeCase}, profiles)
	if err != nil {
		return nil, nil, err
	}
	stream, ok := ad.chat.(ports.ChatStream)
	if !ok {
		return nil, nil, errors.New("selected chat adapter does not implement the Switch room stream")
	}
	roomConsumer, err := roomstream.New(roomstream.Config{Rooms: store, Cursors: store, Stream: stream, Handler: runtime.processor, RefreshEvery: refreshEvery, Wait: clock.Wait})
	if err != nil {
		return nil, nil, fmt.Errorf("build Switch room consumer: %w", err)
	}
	commentConsumer, err := collaboration.NewGitHubCommentPoller(collaboration.CommentPollConfig{Repos: ad.repos, PRs: ad.vcs, Comments: ad.vcs, Cursors: store, Handler: runtime, Clock: clk})
	if err != nil {
		return nil, nil, fmt.Errorf("build GitHub comment consumer: %w", err)
	}
	return roomConsumer, commentConsumer, nil
}

// assembleMCP wires the task index, agent tools service, adapters, and MCP server.
func assembleMCP(
	d deps, ad adapters, store ports.EventStore, pool *pgxpool.Pool, blobs ports.BlobStore, eng *engine.Engine, gen ports.IDs, clk ports.Clock, files *knowledge.FileService, profiles mcp.Profiles,
) (http.Handler, error) {
	taskIndex := postgres.NewTaskIndex(pool)
	asker := &chatAsker{chat: ad.chat, store: store, ids: gen}
	toolsSvc, err := agenttools.New(agenttools.Config{
		Events:          store,
		Index:           taskIndex,
		Blobs:           blobs,
		Clock:           clk,
		IDs:             gen,
		Asker:           asker,
		ReviewRefs:      reviewRefs{code: ad.vcs},
		ReviewValidator: semanticReviewValidator{refs: reviewRefs{code: ad.vcs}},
	})
	if err != nil {
		return nil, fmt.Errorf("build agenttools: %w", err)
	}

	tokStore := ad.tokens
	if tokStore == nil {
		tokStore = &tokenStore{principals: make(map[string]mcp.Principal)}
	}
	tracker := &assignmentAgentTracker{agents: make(map[id.TaskID]id.AgentName)}
	tasksAdp := &tasksAdapter{svc: toolsSvc, tracker: tracker, advanceTests: func(ctx context.Context, changeID id.ChangeID, event change.Event) error {
		_, err := eng.Advance(ctx, changeID, []change.Event{event})
		return err
	}}
	outputFilter, err := appreview.NewOutputFilter(nil, outputAudit{logger: d.logger})
	if err != nil {
		return nil, fmt.Errorf("build agent output filter: %w", err)
	}
	tasksAdp.filter = outputFilter
	reviewsAdp := &mcpReviewsAdapter{svc: toolsSvc, tracker: tracker}
	graphitiCaller, err := graphiti.NewHTTPCaller("http://graphiti:8000/mcp", d.httpClient)
	if err != nil {
		return nil, fmt.Errorf("build Graphiti caller: %w", err)
	}
	memory, err := graphiti.New(graphitiCaller)
	if err != nil {
		return nil, fmt.Errorf("build Graphiti adapter: %w", err)
	}
	knowledgeSvc, err := knowledge.NewSearchService(memory, knowledge.PolicyScopes{Code: ad.vcs, Decode: policyyaml.Decode}, knowledgeQueryAudit{logger: d.logger}, clk)
	if err != nil {
		return nil, fmt.Errorf("build knowledge search service: %w", err)
	}

	mcpSrv, err := mcp.NewServer(mcp.Deps{
		Logger: d.logger, Clock: clk, Tokens: tokStore, Tasks: tasksAdp,
		Reviews: reviewsAdp, Code: ad.vcs, Blobs: blobs, Knowledge: knowledgeSvc,
		Files:    newKnowledgeFileManager(files, ad.vcs),
		Profiles: profiles,
	})
	if err != nil {
		return nil, fmt.Errorf("build mcp server: %w", err)
	}
	return mcpSrv.Handler(), nil
}

func assembleProfileLifecycle(ad adapters, store ports.ProfileStore, blobs ports.BlobStore, clk ports.Clock) (mcp.Profiles, error) {
	client := rootlessrunner.Client{Socket: "/run/heimdall-runner/runner.sock", Source: ad.vcs, Blobs: blobs}
	service, err := appprofile.New(appprofile.Config{
		Store: store, Parser: profileyaml.Parser{}, Policy: client, Trials: client, Syncer: client,
		Warmer: client, Authorizer: profileAuthorizer{code: ad.vcs, repos: ad.vcs}, Clock: clk,
	})
	if err != nil {
		return nil, fmt.Errorf("build sandbox profile lifecycle: %w", err)
	}
	return profileAPI{service: service}, nil
}

// assembleSyncer wires the policy decoder, gate source, evaluator, intake and PR syncer.
func assembleSyncer(ad adapters, store ports.EventStore, gen ports.IDs, clk ports.Clock) (*prsync.Syncer, *intake.Intake, error) {
	gateSrc, err := gateuse.NewSource(gateuse.SourceConfig{
		PRs:    ad.vcs,
		Code:   ad.vcs,
		Decode: policyyaml.Decode,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("build gate source: %w", err)
	}
	gateEval, err := gateuse.New(gateuse.Config{
		Events: store,
		Source: gateSrc,
		Clock:  clk,
		IDs:    gen,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("build gate evaluator: %w", err)
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
		return nil, nil, fmt.Errorf("build intake: %w", err)
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
		return nil, nil, fmt.Errorf("build pr syncer: %w", err)
	}
	return sync, intakeCase, nil
}

// baseExecutors builds the room_open, github_branch, and github_check executors.
func baseExecutors(
	ad adapters, store ports.EventStore, in effects.Ingest, gen ports.IDs, clk ports.Clock,
) (room effects.RoomOpen, branch effects.GitHubBranch, check effects.GitHubCheck, err error) {
	room, err = effects.NewRoomOpen(effects.RoomOpenDeps{
		Tickets: ad.tickets, Chat: ad.chat, Store: store, Ingest: in, IDs: gen, Clock: clk,
	})
	if err != nil {
		return room, branch, check, fmt.Errorf("build room_open executor: %w", err)
	}
	branch, err = effects.NewGitHubBranch(effects.GitHubBranchDeps{
		Tickets: ad.tickets, Base: ad.vcs, Branches: ad.vcs, Store: store, Ingest: in, IDs: gen, Clock: clk,
	})
	if err != nil {
		return room, branch, check, fmt.Errorf("build github_branch executor: %w", err)
	}
	check, err = effects.NewGitHubCheck(effects.GitHubCheckDeps{
		Checks: ad.vcs,
	})
	if err != nil {
		return room, branch, check, fmt.Errorf("build github_check executor: %w", err)
	}
	return room, branch, check, nil
}

// executors maps each effect kind to its executor. Each executor checks the kind of the effect it receives, so a
// swapped entry fails every effect it is given.
func executors(ctx context.Context, ad adapters, store ports.EventStore, profiles ports.ProfileStore, blobs ports.BlobStore, in effects.Ingest, gen ports.IDs, clk ports.Clock) (map[change.EffectKind]outbox.Executor, error) {
	room, branch, check, err := baseExecutors(ad, store, in, gen, clk)
	if err != nil {
		return nil, err
	}
	taskAssign, err := effects.NewTaskAssign(effects.TaskAssignDeps{
		Blobs: blobs, Chat: ad.chat,
	})
	if err != nil {
		return nil, fmt.Errorf("build task_assign executor: %w", err)
	}
	commitTests, err := effects.NewCommitTests(effects.CommitTestsDeps{
		Blobs: blobs, Branches: ad.vcs, Store: store, Ingest: in, IDs: gen, Clock: clk, Code: ad.vcs,
	})
	if err != nil {
		return nil, fmt.Errorf("build github_commit_tests executor: %w", err)
	}
	sandbox := profileSandbox{
		Sandbox:  rootlessRunnerSandbox{client: rootlessrunner.Client{Socket: "/run/heimdall-runner/runner.sock", Source: ad.vcs, Blobs: blobs}},
		Profiles: profiles,
	}
	testQuality, err := effects.NewTestQuality(effects.TestQualityDeps{
		Blobs: blobs, Source: vcsSource{vcs: ad.vcs}, Sandbox: sandbox, Store: store, Ingest: in, IDs: gen, Clock: clk, Code: ad.vcs,
		ForbiddenImports: testQualityForbiddenImports(), NeedsSignoff: testQualityNeedsSignoff(ctx, blobs, ad.vcs),
	})
	if err != nil {
		return nil, fmt.Errorf("build test_quality executor: %w", err)
	}
	verifyEvidence, err := effects.NewVerifyEvidence(effects.VerifyEvidenceDeps{
		Source: vcsSource{vcs: ad.vcs}, Sandbox: sandbox, Store: store, Ingest: in, IDs: gen, Clock: clk,
	})
	if err != nil {
		return nil, fmt.Errorf("build verify_evidence executor: %w", err)
	}
	return map[change.EffectKind]outbox.Executor{
		change.EffectRoomOpen:          room,
		change.EffectGitHubBranch:      branch,
		change.EffectGitHubCheck:       check,
		change.EffectTaskAssign:        taskAssign,
		change.EffectGitHubCommitTests: commitTests,
		change.EffectTestQuality:       testQuality,
		change.EffectVerifyEvidence:    verifyEvidence,
	}, nil
}
