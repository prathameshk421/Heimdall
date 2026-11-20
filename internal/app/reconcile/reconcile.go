package reconcile

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/liveness"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// maxDecisions is how many times one Run decides again after a version conflict.
const maxDecisions = 5

// kickBuffer is the size of the kick channel; a full channel drops the kick and the sweep covers it.
const kickBuffer = 256

// SweepEvery is how often Loop runs every open change.
const SweepEvery = 30 * time.Second

// InputSource reads what the rows need from outside the log: the policy at a commit, the facts that need the
// repository, and the repository's promoted profile. The reconciler caches the answers for a minute.
type InputSource interface {
	// Policy returns the policy at sha of repo; an empty sha means the repository's default branch.
	Policy(ctx context.Context, repo change.Repo, sha id.SHA) (policy.Policy, error)
	// Outside returns the outside facts of a change with a pull request.
	Outside(ctx context.Context, v change.View) (change.Outside, error)
	// Profile returns the promoted profile of repo, or the zero Profile when there is none.
	Profile(ctx context.Context, repo change.Repo) (change.Profile, error)
}

// Config is everything a Reconciler needs.
type Config struct {
	Store    ports.EventStore
	Effects  ports.EffectEnsurer
	Open     ports.OpenChanges
	Dead     ports.DeadEffects
	Inputs   InputSource
	Calls    ports.ToolCalls
	Health   ports.ModelHealth
	Agents   func(kind, subject string) (id.AgentName, bool)
	IDs      ports.IDs
	Clock    ports.Clock
	Settings change.Settings
	Windows  liveness.Windows
	Logger   *slog.Logger
	// Wait pauses for d or until ctx is done (clock.Wait); the app layer may not import the platform package.
	Wait func(ctx context.Context, d time.Duration) error
}

// Reconciler is the only component that starts work (HARNESS_DESIGN B3): it reads a change's rows and does what each
// open row says. It decides from the log and nothing else.
type Reconciler struct {
	cfg    Config
	kicks  chan id.ChangeID
	cache  *inputCache
	mu     sync.Mutex
	perRun map[id.ChangeID]*sync.Mutex
}

// New checks the configuration and builds a Reconciler.
func New(cfg Config) (*Reconciler, error) {
	missing := map[string]bool{
		"store": cfg.Store == nil, "effects": cfg.Effects == nil, "open changes": cfg.Open == nil,
		"dead effects": cfg.Dead == nil, "inputs": cfg.Inputs == nil, "tool calls": cfg.Calls == nil,
		"model health": cfg.Health == nil, "agents": cfg.Agents == nil, "ids": cfg.IDs == nil, "clock": cfg.Clock == nil,
		"logger": cfg.Logger == nil, "wait": cfg.Wait == nil,
	}
	for name, isMissing := range missing {
		if isMissing {
			return nil, fmt.Errorf("reconciler: %s is required", name)
		}
	}
	if cfg.Settings.TestAttempts < 1 {
		return nil, errors.New("reconciler: settings.TestAttempts must be 1 or more")
	}
	return &Reconciler{cfg: cfg, kicks: make(chan id.ChangeID, kickBuffer), cache: newInputCache(cfg.Clock),
		perRun: map[id.ChangeID]*sync.Mutex{}}, nil
}

// Kick asks for a Run of the change soon. It never blocks: when the channel is full the kick is dropped, because the
// sweep runs every change anyway.
func (r *Reconciler) Kick(changeID id.ChangeID) {
	select {
	case r.kicks <- changeID:
	default:
	}
}

// Sweep runs every change that is not over, one after another. A change whose Run fails is logged and does not stop
// the others.
func (r *Reconciler) Sweep(ctx context.Context) error {
	open, err := r.cfg.Open.OpenChanges(ctx)
	if err != nil {
		return fmt.Errorf("sweep: list open changes: %w", err)
	}
	for _, c := range open {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := r.Run(ctx, c); err != nil {
			r.cfg.Logger.ErrorContext(ctx, "reconcile failed", "change", string(c), "error", err)
		}
	}
	return nil
}

// Loop runs until ctx is done: one goroutine runs the changes that were kicked, another sweeps every SweepEvery.
func (r *Reconciler) Loop(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case c := <-r.kicks:
				if err := r.Run(ctx, c); err != nil && ctx.Err() == nil {
					r.cfg.Logger.ErrorContext(ctx, "reconcile failed", "change", string(c), "error", err)
				}
			}
		}
	}()
	go func() {
		defer wg.Done()
		for {
			if err := r.Sweep(ctx); err != nil && ctx.Err() == nil {
				r.cfg.Logger.ErrorContext(ctx, "sweep failed", "error", err)
			}
			if r.cfg.Wait(ctx, SweepEvery) != nil {
				return
			}
		}
	}()
	wg.Wait()
}

// lockOf returns the mutex that serializes Runs of one change.
func (r *Reconciler) lockOf(c id.ChangeID) *sync.Mutex {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.perRun[c]
	if !ok {
		m = &sync.Mutex{}
		r.perRun[c] = m
	}
	return m
}

// Run decides what the change needs now and does it: it appends the facts and attaches the jobs the open rows ask
// for. A version conflict reloads and decides again. Runs of one change are serialized; a change with nothing to do
// appends nothing.
func (r *Reconciler) Run(ctx context.Context, changeID id.ChangeID) error {
	m := r.lockOf(changeID)
	m.Lock()
	defer m.Unlock()
	for attempt := 1; attempt <= maxDecisions; attempt++ {
		err := r.runOnce(ctx, changeID)
		if !errors.Is(err, ports.ErrVersionConflict) {
			return err
		}
	}
	return fmt.Errorf("reconcile %s: still conflicting after %d decisions: %w", changeID, maxDecisions, ports.ErrVersionConflict)
}

func (r *Reconciler) runOnce(ctx context.Context, changeID id.ChangeID) error {
	events, err := r.cfg.Store.Load(ctx, changeID)
	if err != nil {
		return fmt.Errorf("reconcile %s: load: %w", changeID, err)
	}
	if len(events) == 0 {
		return nil
	}
	v, err := change.Fold(events)
	if err != nil {
		return fmt.Errorf("reconcile %s: fold: %w", changeID, err)
	}
	if v.Over() || (v.Trigger == nil && v.PR == nil) {
		return nil
	}
	in, err := r.buildIn(ctx, changeID, v)
	if err != nil {
		return fmt.Errorf("reconcile %s: %w", changeID, err)
	}
	d, err := r.decide(ctx, in)
	if err != nil {
		return fmt.Errorf("reconcile %s: %w", changeID, err)
	}
	return r.apply(ctx, changeID, events, d)
}

// buildIn reads everything the rows may need. An input that cannot be read stops the Run with an error; the sweep tries
// again. A row never sees an empty default in place of an unread input. A policy that was read but cannot be used
// (policy.ErrInvalid) is not a failed read: retrying cannot fix it, so it becomes In.PolicyProblem, no row opens and
// Missing names it.
func (r *Reconciler) buildIn(ctx context.Context, changeID id.ChangeID, v change.View) (change.In, error) {
	in := change.In{Change: changeID, View: v, Settings: r.cfg.Settings, Now: r.cfg.Clock.Now()}
	repo, base := repoAndBase(v)
	var err error
	in.Policy, err = r.cache.policy(ctx, r.cfg.Inputs, repo, base)
	switch {
	case errors.Is(err, policy.ErrInvalid):
		in.PolicyProblem = err.Error()
		r.cfg.Logger.WarnContext(ctx, "the policy cannot be used; nothing runs on the change", "change", string(changeID),
			"repo", string(repo), "sha", string(base), "error", err)
		if in.Dead, err = r.cfg.Dead.DeadEffects(ctx, changeID); err != nil {
			return in, fmt.Errorf("read dead jobs: %w", err)
		}
		return in, nil
	case err != nil:
		return in, fmt.Errorf("read policy at %q: %w", base, err)
	}
	if in.Profile, err = r.cache.profile(ctx, r.cfg.Inputs, repo); err != nil {
		return in, fmt.Errorf("read profile: %w", err)
	}
	if v.PR != nil {
		if in.Outside, err = r.cache.outside(ctx, r.cfg.Inputs, changeID, v); err != nil {
			return in, fmt.Errorf("read outside facts: %w", err)
		}
	}
	if in.Dead, err = r.cfg.Dead.DeadEffects(ctx, changeID); err != nil {
		return in, fmt.Errorf("read dead jobs: %w", err)
	}
	return in, nil
}

// repoAndBase is the repository of the change and the commit its policy is read at: the pull request's base when there
// is one, else the branch's base, else none (the default branch).
func repoAndBase(v change.View) (change.Repo, id.SHA) {
	switch {
	case v.PR != nil:
		return v.PR.Repo, v.PR.Base
	case v.Branch != nil:
		return v.Trigger.Repo, v.Branch.BaseSHA
	case v.Trigger != nil:
		return v.Trigger.Repo, ""
	}
	return "", ""
}

// Needs says in plain words what the change still needs (change.Missing), for the board and for get_task. It reads the
// same inputs a Run reads and decides nothing.
func (r *Reconciler) Needs(ctx context.Context, changeID id.ChangeID) ([]string, error) {
	events, err := r.cfg.Store.Load(ctx, changeID)
	if err != nil {
		return nil, fmt.Errorf("needs of %s: load: %w", changeID, err)
	}
	v, err := change.Fold(events)
	if err != nil {
		return nil, fmt.Errorf("needs of %s: fold: %w", changeID, err)
	}
	if v.Trigger == nil && v.PR == nil {
		return []string{}, nil
	}
	in, err := r.buildIn(ctx, changeID, v)
	if err != nil {
		return nil, fmt.Errorf("needs of %s: %w", changeID, err)
	}
	needs := change.Missing(in)
	out := make([]string, 0, len(needs))
	for _, n := range needs {
		out = append(out, n.Text)
	}
	return out, nil
}
