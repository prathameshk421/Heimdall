package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/postgres"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/effects"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/engine"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/outbox"
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
// client and the timing. Every field is required.
type deps struct {
	databaseURL string
	endpoints   endpoints
	httpClient  *http.Client
	logger      *slog.Logger
	pollEvery   time.Duration
	drainTick   time.Duration
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
	logger    *slog.Logger
	clock     ports.Clock
	pollEvery time.Duration
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
	d, err := productionDeps(logger)
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

// productionDeps reads what the slice needs from the config. The config has no key for the database URL, the Jira and
// Switch endpoints or the Switch identities yet. A config that selects a slice provider is therefore refused, and the
// error names every missing key. When those keys are added, delete the list and read them here.
func productionDeps(logger *slog.Logger) (deps, error) {
	missing := []string{"database url", "jira base url", "switch base url", "switch agent id", "switch agent name", "switch bridge id"}
	if len(missing) > 0 {
		return deps{}, fmt.Errorf("heimdall.toml selects a slice provider but has no key for: %s", strings.Join(missing, ", "))
	}
	return deps{
		httpClient: &http.Client{},
		logger:     logger,
		endpoints:  endpoints{githubAPI: githubAPIRoot},
		pollEvery:  pollInterval,
		drainTick:  drainTick,
	}, nil
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
	if d.drainTick <= 0 {
		problems = append(problems, "drain tick must be positive")
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
	pool, err := pgxpool.New(ctx, d.databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
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
	if err := postgres.Migrate(ctx, pool); err != nil {
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
	return &app{
		pool: pool, poller: p, drainer: box, logger: d.logger, clock: clk,
		pollEvery: d.pollEvery, drainTick: d.drainTick,
	}, nil
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
	return map[change.EffectKind]outbox.Executor{
		change.EffectRoomOpen:     room,
		change.EffectGitHubBranch: branch,
	}, nil
}
