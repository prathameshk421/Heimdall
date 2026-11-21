package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/blobfs"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/postgres"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/announce"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/collaboration"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/effects"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/knowledge"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/outbox"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/prsync"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/reconcile"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/trigger"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/ids"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Settings the slice reads from the config or names here. The config has no key for a poll interval or an outbox
// lease, so they are named constants. The config has no key for the endpoints of the adapters or for the database
// either; productionDeps refuses to start until those keys exist in heimdall.toml.
const (
	// pollInterval is the wait between two ticket polls.
	pollInterval = 15 * time.Second
	// syncInterval is the wait between two pull request syncs.
	syncInterval = 10 * time.Second
	// reconcileInterval is the wait between two reconcile passes, which also run at boot (TECH_SPEC 2.2, D56).
	reconcileInterval = 5 * time.Minute
	// drainTick is the longest the outbox waits between two batches when no wake arrives.
	drainTick = 2 * time.Second
	// knowledgeRetentionInterval bounds how long expired approved knowledge remains available.
	knowledgeRetentionInterval = 30 * time.Minute
	// outboxBatch is how many effects one drain claims at once.
	outboxBatch = 1
	// drainWorkers is how many effects can run at once.
	drainWorkers = 3
	// effectTimeout bounds one executor call. The lease must exceed outboxBatch times this (outbox.New).
	effectTimeout = 20 * time.Second
	// testQualityTimeout bounds a test_quality effect: two sandbox jobs of up to 3 minutes each, plus the runner's
	// margin. With the 20 s default the outbox cancelled the run and every retry started another container (KAN-9).
	testQualityTimeout = 7 * time.Minute
	// verifyEvidenceTimeout bounds a verify_evidence effect: a source fetch, one sandbox job of up to 3 minutes, and the
	// event ingest after it. With the 20 s default the run was cut and the ingest after it timed out on every retry, so
	// E1 findings dropped to E4 or never settled (KAN-23, Nov 8).
	verifyEvidenceTimeout = 5 * time.Minute
	// outboxLease is how long a claimed effect stays leased to one drainer. It must exceed outboxBatch times the longest
	// effect timeout (outbox.New). An effect that was running when the server stopped is in doubt and is not re-run on
	// lease expiry (T22), so a long lease does not delay recovery.
	outboxLease = 60 * time.Minute
	// blobStoreRoot is the on-disk blob store until heimdall.toml gains a blobs path key.
	blobStoreRoot = ".heimdall/blobs"
	// githubAPIRoot is the production GitHub API, which is not site-specific and so needs no config key.
	githubAPIRoot = "https://api.github.com"
	// runnerSocketPath is the runner's Unix socket on the VM (deploy/heimdall-runner); cmd/heimdall-runner listens on it.
	runnerSocketPath = "/run/heimdall-runner/runner.sock"
	// graphitiEndpoint is the team-memory MCP endpoint on the Compose network.
	graphitiEndpoint = "http://graphiti:8000/mcp"
)

// deps is what build needs that differs between production and a test: the database, the adapter endpoints, the HTTP
// client and the timing. Every field is required. openPool and migrate are injected so a test can prove the wiring
// without a live Postgres; productionDeps fills them with the real pgxpool and migration functions.
type deps struct {
	databaseURL   string
	endpoints     endpoints
	httpClient    *http.Client
	logger        *slog.Logger
	configuration config.Config
	pollEvery     time.Duration
	syncEvery     time.Duration
	drainTick     time.Duration
	openPool      func(ctx context.Context, url string) (*pgxpool.Pool, error)
	migrate       func(ctx context.Context, pool *pgxpool.Pool) error
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
	// runnerSocket is the runner's Unix socket and graphitiURL the team-memory endpoint; the boot self-check calls both.
	runnerSocket string
	graphitiURL  string
	// modelURL is the agents' model provider API root (modelProviderOf); the reconciler's liveness probe calls it.
	modelURL string
}

// app is the running slice: the ticket poller, the outbox drainer and the database they share.
type app struct {
	pool              *pgxpool.Pool
	pollers           []poller
	drainer           drainer
	syncer            syncer
	roomStream        roomStreamRunner
	comments          commentPoller
	mcpHandler        http.Handler
	agentSetupHandler http.Handler
	// agentSync moves the role agents in Switch to the configured model. It is nil without an agent runtime.
	agentSync      func(context.Context) error
	operatorAnswer http.Handler
	operatorReject http.Handler
	// operatorRecheck repeats one finding's evidence on the current head after a run that gave a wrong answer.
	operatorRecheck http.Handler
	operatorRoom    http.Handler
	// degraded names each tool the boot self-check withheld, with the reason; /healthz reports it.
	degraded []string
	narrator *announce.Narrator
	// reconciler starts all work of every change (HARNESS_DESIGN B3).
	reconciler     *reconcile.Reconciler
	cardsUpload    http.Handler
	commands       *commandRuntime
	workflowMemory *knowledge.WorkflowMemory
	proposer       *cardProposer
	retention      knowledgeRetention
	episodeWorker  episodeDrainer
	blobs          io.Closer
	logger         *slog.Logger
	clock          ports.Clock
	pollEvery      time.Duration
	syncEvery      time.Duration
	drainTick      time.Duration
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
	Reconcile(ctx context.Context) (prsync.Result, error)
}

type roomStreamRunner interface{ Run(context.Context) error }
type commentPoller interface {
	PollOnce(context.Context) (collaboration.CommentPollResult, error)
}

type knowledgeRetention interface {
	Expire(context.Context) (int, error)
}

type episodeDrainer interface {
	RunOnce(context.Context, int) (int, error)
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
	provider, err := modelProviderOf(cfg, cfg.AgentModel())
	if err != nil {
		return deps{}, fmt.Errorf("agent_runtime.model: %w", err)
	}
	return deps{
		databaseURL:   databaseURL,
		httpClient:    &http.Client{},
		logger:        logger,
		configuration: cfg,
		endpoints: endpoints{
			jiraBaseURL:     cfg.Jira.BaseURL,
			githubAPI:       githubAPIRoot,
			switchBaseURL:   cfg.Switch.BaseURL,
			switchAgentID:   cfg.Switch.AgentID,
			switchAgentName: cfg.Switch.AgentName,
			switchBridgeID:  cfg.Switch.BridgeID,
			runnerSocket:    runnerSocketPath,
			graphitiURL:     graphitiEndpoint,
			modelURL:        provider.url,
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
	// The MCP edge authenticates each agent; without [agents] it refuses every token, so the slice needs it.
	add("agents.test_author", cfg.Agents.TestAuthor)
	if len(cfg.Agents.Reviewers) == 0 {
		missing = append(missing, "agents.reviewers")
	}
	add("agents.resolver", cfg.Agents.Resolver)
	return missing
}

// checkProjectSelection requires the Jira project keys and the mapped projects to be the same set, with no repeats. Each
// listed project gets its own poller and cursor (A10 item 10). The file tracker still polls one project: its poll is
// not scoped by project.
func checkProjectSelection(cfg config.Config) error {
	tracker, _ := cfg.Selection("tracker")
	if tracker != "jira" {
		if tracker == "file" && len(cfg.Projects) != 1 {
			return fmt.Errorf("projects: the file tracker polls one project, but heimdall.toml maps %d", len(cfg.Projects))
		}
		return nil
	}
	if len(cfg.Projects) == 0 {
		return fmt.Errorf("projects: heimdall.toml maps no tracker project")
	}
	listed := map[string]bool{}
	for _, k := range cfg.Jira.ProjectKeys {
		if listed[k] {
			return fmt.Errorf("jira.project_keys: %q is listed twice", k)
		}
		listed[k] = true
	}
	mapped := map[string]bool{}
	for _, p := range cfg.Projects {
		if mapped[p.TrackerProject] {
			return fmt.Errorf("projects: %q is mapped twice", p.TrackerProject)
		}
		mapped[p.TrackerProject] = true
		if !listed[p.TrackerProject] {
			return fmt.Errorf("projects: %q is mapped but not listed in jira.project_keys", p.TrackerProject)
		}
	}
	for _, k := range cfg.Jira.ProjectKeys {
		if !mapped[k] {
			return fmt.Errorf("jira.project_keys: %q has no [[projects]] mapping", k)
		}
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
	for _, c := range []struct {
		bad     bool
		problem string
	}{
		{d.databaseURL == "", "database url is empty"},
		{d.httpClient == nil, "http client is nil"},
		{d.logger == nil, "logger is nil"},
		{d.pollEvery <= 0, "poll interval must be positive"},
		{d.syncEvery <= 0, "sync interval must be positive"},
		{d.drainTick <= 0, "drain tick must be positive"},
		{d.openPool == nil, "database opener is nil"},
		{d.migrate == nil, "migration function is nil"},
		{d.endpoints.runnerSocket == "", "runner socket is empty"},
		{d.endpoints.graphitiURL == "", "graphiti endpoint is empty"},
		{d.endpoints.modelURL == "", "model provider endpoint is empty"},
	} {
		if c.bad {
			problems = append(problems, c.problem)
		}
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
	a.operatorAnswer = operatorAnswer{answer: a.commands.OperatorAnswer, secrets: cfg, keyName: cfg.Switch.APIKeySecret}
	if maker, ok := ad.chat.(interface {
		CreateChannel(ctx context.Context, bridgeID, name, description string) (change.RoomID, error)
	}); ok {
		a.operatorRoom = operatorRoom{create: maker.CreateChannel, secrets: cfg, keyName: cfg.Switch.APIKeySecret}
	}
	a.operatorReject = operatorReject{reject: a.commands.OperatorReject, secrets: cfg, keyName: cfg.Switch.APIKeySecret}
	a.operatorRecheck = operatorRecheck{recheck: a.commands.OperatorRecheck, secrets: cfg, keyName: cfg.Switch.APIKeySecret}
	return a, nil
}

// assemble migrates the database and wires the engine, the executors, the poller and the drainer on it.
func assemble(ctx context.Context, d deps, ad adapters, pool *pgxpool.Pool, clk ports.Clock) (*app, error) {
	if err := d.migrate(ctx, pool); err != nil {
		return nil, fmt.Errorf("migrate database: %w", err)
	}
	store := postgres.NewStore(pool)
	var setupHandler http.Handler
	var syncAgents func(context.Context) error
	if ad.agentSetupHandler != nil {
		var err error
		setupHandler, syncAgents, err = ad.agentSetupHandler(store)
		if err != nil {
			return nil, err
		}
	}
	gen := ids.New()
	provider, err := modelProviderOf(d.configuration, d.configuration.AgentModel())
	if err != nil {
		return nil, fmt.Errorf("liveness probe: %w", err)
	}
	prober := proberFor(provider, keyReader{cfg: d.configuration})
	prober.BaseURL = d.endpoints.modelURL // deps carry the URL so a test can point the probe at a fake
	rec, err := newReconciler(ad, store, prober, gen, clk, d.logger)
	if err != nil {
		return nil, fmt.Errorf("build reconciler: %w", err)
	}
	eng := reconcile.Recorder{Store: reconcile.Kicking{EventStore: store, Kicker: rec}}
	blobStore, err := blobfs.New(blobStoreRoot)
	if err != nil {
		return nil, fmt.Errorf("open blob store: %w", err)
	}
	check, err := bootSelfCheck(ctx, d, ad, blobStore)
	if err != nil {
		_ = blobStore.Close()
		return nil, err
	}
	a, err := assembleWithBlobs(rec, d, ad, store, pool, blobStore, eng, gen, clk, setupHandler, check)
	if err != nil {
		_ = blobStore.Close()
		return nil, err
	}
	a.reconciler = rec
	a.degraded = check.degraded
	a.agentSync = syncAgents
	return a, nil
}

func assembleWithBlobs(
	rec *reconcile.Reconciler, d deps, ad adapters, store *postgres.Store, pool *pgxpool.Pool, blobs ports.BlobStore,
	eng reconcile.Recorder, gen ports.IDs, clk ports.Clock, setupHandler http.Handler, check selfCheck,
) (*app, error) {
	profileManager, err := assembleProfileLifecycle(ad, store, blobs, clk)
	if err != nil {
		return nil, err
	}
	evidence, err := newEvidenceRecheck(d, ad, store, clk)
	if err != nil {
		return nil, err
	}
	execs, err := executors(ad, store, store, blobs, ingestor{eng: eng}, gen, clk, evidence)
	if err != nil {
		return nil, err
	}
	box, err := newDrainer(d, store, execs)
	if err != nil {
		return nil, err
	}
	// One poller per project, each with its own cursor source provider:project (A10 item 10).
	pollers, err := newTicketPollers(ad, store, eng, blobs, gen, clk)
	if err != nil {
		return nil, err
	}
	sync, intakeCase, err := assembleSyncer(ad, store, eng.Store, gen, clk)
	if err != nil {
		return nil, err
	}
	roomConsumer, commentConsumer, runtime, err := assembleCollaboration(store, ad, eng, gen, clk, d.syncEvery, intakeCase, profileManager)
	if err != nil {
		return nil, err
	}
	files, episodeWorker, workflowMemory, err := assembleKnowledgeRetention(d, ad, store, blobs, clk)
	if err != nil {
		return nil, err
	}
	mcpSrv, proposer, err := assembleMCP(d, ad, store, pool, blobs, eng, gen, clk, files, profileManager, runtime, rec.Needs, store, check.withheld)
	if err != nil {
		return nil, err
	}
	// A wiring problem stops the server here, before any loop starts, with every problem named.
	if err := checkWiring(mcpSrv, execs, check.off); err != nil {
		return nil, err
	}
	cardsUpload, narrator, err := assembleHumanEdges(d, ad, store)
	if err != nil {
		return nil, err
	}
	return &app{
		narrator: narrator,
		pool:     pool, pollers: pollers, drainer: box, syncer: sync, roomStream: roomConsumer, comments: commentConsumer, mcpHandler: mcpSrv.Handler(), agentSetupHandler: setupHandler, cardsUpload: cardsUpload, commands: runtime, episodeWorker: episodeWorker, workflowMemory: workflowMemory, proposer: proposer, retention: files, blobs: closerOf(blobs),
		logger: d.logger, clock: clk,
		pollEvery: d.pollEvery, syncEvery: d.syncEvery, drainTick: d.drainTick,
	}, nil
}

// assembleHumanEdges builds what people meet: the card upload handler and the room narrator.
func assembleHumanEdges(d deps, ad adapters, store *postgres.Store) (http.Handler, *announce.Narrator, error) {
	cardsUpload, err := newUploadHandlers(d, ad)
	if err != nil {
		return nil, nil, err
	}
	narrator, err := assembleNarrator(d, ad, store)
	if err != nil {
		return nil, nil, err
	}
	return cardsUpload, narrator, nil
}

// newDrainer builds the outbox drainer over the executors, with the longer timeouts of the sandbox effects.
func newDrainer(d deps, store *postgres.Store, execs map[change.EffectKind]outbox.Executor) (*outbox.Drainer, error) {
	box, err := outbox.New(outbox.Config{
		Outbox: store, Executors: execs, Logger: d.logger,
		Batch: outboxBatch, Lease: outboxLease, EffectTimeout: effectTimeout,
		KindTimeouts: map[change.EffectKind]time.Duration{
			change.EffectTestQuality: testQualityTimeout, change.EffectAcceptance: testQualityTimeout,
			change.EffectVerifyEvidence: verifyEvidenceTimeout,
			change.EffectProfileTrial:   testQualityTimeout, // a trial runs the profile's tests in the sandbox, like acceptance
		},
	})
	if err != nil {
		return nil, fmt.Errorf("build outbox drainer: %w", err)
	}
	return box, nil
}

// executors maps each effect kind to its executor. Each area builds its own (wire_<area>.go). Each executor checks the
// kind of the effect it receives, so a swapped entry fails every effect it is given.
func executors(ad adapters, store ports.EventStore, profiles ports.ProfileStore, blobs ports.BlobStore, in effects.Ingest, gen ports.IDs, clk ports.Clock, recheck effects.MemoryRecheck) (map[change.EffectKind]outbox.Executor, error) {
	sandbox := profileSandbox{
		Sandbox:  rootlessRunnerSandbox{client: ad.runner(blobs)},
		Profiles: profiles,
	}
	core, err := coreExecutors(ad, store, in, gen, clk)
	if err != nil {
		return nil, err
	}
	tests, err := testsExecutors(ad, blobs, sandbox, in, gen, clk)
	if err != nil {
		return nil, err
	}
	review, err := reviewExecutors(ad, profiles, sandbox, in, gen, clk, recheck)
	if err != nil {
		return nil, err
	}
	lifecycle, err := newProfileService(ad, profiles, blobs, clk)
	if err != nil {
		return nil, err
	}
	trial, err := effects.NewProfileTrial(effects.ProfileTrialDeps{Trials: lifecycle})
	if err != nil {
		return nil, fmt.Errorf("build profile_trial executor: %w", err)
	}
	review[change.EffectProfileTrial] = trial
	all := map[change.EffectKind]outbox.Executor{}
	for _, area := range []map[change.EffectKind]outbox.Executor{core, tests, review} {
		maps.Copy(all, area)
	}
	return all, nil
}
