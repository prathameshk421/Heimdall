package outbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Executor performs one kind of effect on the outside system. Effect.Key is the idempotency key to pass on.
// An error it returns becomes the reason of Fail, verbatim, so it must not contain secrets. An error means the
// action did not happen: the drainer retries the effect after an error, and it does not retry after a crash.
type Executor interface {
	Execute(ctx context.Context, fx change.Effect) error
}

// Config holds what New needs. See the package doc for each rule.
type Config struct {
	Outbox        ports.Outbox
	Executors     map[change.EffectKind]Executor
	Logger        *slog.Logger
	Batch         int
	Lease         time.Duration
	EffectTimeout time.Duration
}

// Drainer runs the outbox. Build it with New.
type Drainer struct {
	box     ports.Outbox
	execs   map[change.EffectKind]Executor
	log     *slog.Logger
	batch   int
	lease   time.Duration
	timeout time.Duration
}

// DrainResult counts what one batch did.
type DrainResult struct {
	Claimed int
	Done    int
	Failed  int
}

// New checks the configuration and builds a drainer. It refuses every configuration the package doc names.
func New(cfg Config) (*Drainer, error) {
	var problems []error
	if cfg.Outbox == nil {
		problems = append(problems, errors.New("outbox is nil"))
	}
	if cfg.Logger == nil {
		problems = append(problems, errors.New("logger is nil"))
	}
	execs, execProblems := checkExecutors(cfg.Executors)
	problems = append(problems, execProblems...)
	problems = append(problems, checkTiming(cfg)...)
	if len(problems) > 0 {
		return nil, fmt.Errorf("outbox drainer: %w", errors.Join(problems...))
	}
	return &Drainer{
		box:     cfg.Outbox,
		execs:   execs,
		log:     cfg.Logger,
		batch:   cfg.Batch,
		lease:   cfg.Lease,
		timeout: cfg.EffectTimeout,
	}, nil
}

// checkExecutors copies the executor map and reports an empty map, a kind that is not an effect kind, and a nil
// executor.
func checkExecutors(in map[change.EffectKind]Executor) (map[change.EffectKind]Executor, []error) {
	var problems []error
	if len(in) == 0 {
		problems = append(problems, errors.New("no executors: a drainer with nothing to perform would hold effects until they dead-letter"))
	}
	out := make(map[change.EffectKind]Executor, len(in))
	for kind, ex := range in {
		switch {
		case !kind.Valid():
			problems = append(problems, fmt.Errorf("executor for kind %q: not an effect kind", kind))
		case ex == nil:
			problems = append(problems, fmt.Errorf("executor for kind %q is nil", kind))
		default:
			out[kind] = ex
		}
	}
	return out, problems
}

// checkTiming reports batch, deadline and lease values that cannot work, including a lease that does not cover a
// whole batch of effect deadlines.
func checkTiming(cfg Config) []error {
	var problems []error
	if cfg.Batch < 1 {
		problems = append(problems, fmt.Errorf("batch %d must be 1 or more", cfg.Batch))
	}
	if cfg.EffectTimeout <= 0 {
		problems = append(problems, fmt.Errorf("effect timeout %s must be positive", cfg.EffectTimeout))
	}
	if cfg.Lease <= 0 {
		problems = append(problems, fmt.Errorf("lease %s must be positive", cfg.Lease))
	}
	if cfg.Batch >= 1 && cfg.EffectTimeout > 0 && cfg.Lease > 0 {
		covered := time.Duration(cfg.Batch) * cfg.EffectTimeout
		if cfg.Lease <= covered {
			problems = append(problems, fmt.Errorf(
				"lease %s must exceed batch %d times the effect timeout %s (%s), so no effect of a batch runs after its lease expires",
				cfg.Lease, cfg.Batch, cfg.EffectTimeout, covered))
		}
	}
	return problems
}

// RunOnce claims one batch and works through it in claim order. An effect the executor fails, or one with no executor,
// is failed with its reason. A failed Start, Done or Fail, or a cancelled context, ends the batch with an error; the
// effects not yet reached keep their leases and are claimed again when those expire.
func (d *Drainer) RunOnce(ctx context.Context) (DrainResult, error) {
	var res DrainResult
	effects, err := d.box.Claim(ctx, d.batch, d.lease)
	if err != nil {
		return res, fmt.Errorf("drain outbox: claim: %w", err)
	}
	res.Claimed = len(effects)
	for i, fx := range effects {
		if err := ctx.Err(); err != nil {
			return res, fmt.Errorf("drain outbox: stopped with %d of %d effects unreached: %w", len(effects)-i, len(effects), err)
		}
		done, err := d.drainOne(ctx, fx)
		if err != nil {
			return res, err
		}
		if done {
			res.Done++
		} else {
			res.Failed++
		}
	}
	return res, nil
}

// RunEffect executes only the effect identified by key. A live lease is returned to River as an error for retry.
func (d *Drainer) RunEffect(ctx context.Context, key change.EffectKey) error {
	fx, err := d.box.ClaimEffect(ctx, key, d.lease)
	if err != nil {
		return fmt.Errorf("drain effect %s: claim: %w", key, err)
	}
	if fx == nil {
		return nil
	}
	done, err := d.drainOne(ctx, *fx)
	if err != nil {
		return err
	}
	if !done {
		return fmt.Errorf("drain effect %s: executor failed", key)
	}
	return nil
}

// drainOne performs one claimed effect and records the outcome. It reports whether the effect is done.
func (d *Drainer) drainOne(ctx context.Context, fx change.Effect) (bool, error) {
	ex, ok := d.execs[fx.Kind]
	if !ok {
		reason := fmt.Sprintf("no executor for effect kind %q", fx.Kind)
		d.log.WarnContext(ctx, "outbox effect failed", "kind", fx.Kind, "key", fx.Key, "reason", reason)
		return false, d.failEffect(ctx, fx, reason)
	}

	if err := d.startEffect(ctx, fx.Key); err != nil {
		return false, err
	}

	execCtx, cancelExec := context.WithTimeout(ctx, d.timeout)
	execErr := ex.Execute(execCtx, fx)
	cancelExec()
	if execErr != nil {
		d.log.WarnContext(ctx, "outbox effect failed", "kind", fx.Kind, "key", fx.Key, "reason", execErr.Error(), "cause", causeOf(execErr))
		return false, d.failEffect(ctx, fx, execErr.Error())
	}

	if err := d.markDone(ctx, fx.Key); err != nil {
		return false, err
	}
	d.log.DebugContext(ctx, "outbox effect done", "kind", fx.Kind, "key", fx.Key)
	return true, nil
}

// bookkeeping returns the context for recording an outcome. It starts when the outcome is known, so the execution
// deadline does not spend its budget. It outlives a cancelled ctx, so an effect that ran is still recorded during
// shutdown, and it has its own EffectTimeout, so a stuck database cannot hold the drainer.
func (d *Drainer) bookkeeping(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), d.timeout)
}

// startEffect records the start of an effect before its executor runs. A refused start means this drainer no longer
// holds the lease, so the executor must not run; the error ends the batch and the effect is left to its new holder.
func (d *Drainer) startEffect(ctx context.Context, key change.EffectKey) error {
	bctx, cancel := d.bookkeeping(ctx)
	defer cancel()
	if err := d.box.Start(bctx, key); err != nil {
		return fmt.Errorf("drain outbox: start %s: %w", key, err)
	}
	return nil
}

func (d *Drainer) failEffect(ctx context.Context, fx change.Effect, reason string) error {
	bctx, cancel := d.bookkeeping(ctx)
	defer cancel()
	if err := d.box.Fail(bctx, fx.Key, reason); err != nil {
		return fmt.Errorf("drain outbox: record failure of %s: %w", fx.Key, err)
	}
	return nil
}

func (d *Drainer) markDone(ctx context.Context, key change.EffectKey) error {
	bctx, cancel := d.bookkeeping(ctx)
	defer cancel()
	if err := d.box.Done(bctx, key); err != nil {
		return fmt.Errorf("drain outbox: mark %s done: %w", key, err)
	}
	return nil
}

// Run calls RunOnce when wake receives, when tick fires, and once at the start. It returns nil when ctx is cancelled.
// A batch that completed at least one effect and filled the batch is followed at once by another, because more may be
// waiting; a batch that failed to claim, or that only failed effects, waits for the next wake or tick. A batch error is
// logged and the loop carries on, so a database outage delays the drain and does not stop it.
func (d *Drainer) Run(ctx context.Context, wake <-chan struct{}, tick <-chan time.Time) error {
	for ctx.Err() == nil {
		res, err := d.RunOnce(ctx)
		if err != nil && ctx.Err() == nil {
			d.log.ErrorContext(ctx, "outbox drain failed; retrying on the next wake or tick", "error", err)
		}
		if err == nil && res.Claimed == d.batch && res.Done > 0 {
			continue
		}
		select {
		case <-ctx.Done():
		case <-wake:
		case <-tick:
		}
	}
	return nil
}

// causeOf is the text of the error an executor wrapped, for the server log only: the stored reason deliberately omits it.
func causeOf(err error) string {
	cause := errors.Unwrap(err)
	if cause == nil {
		return ""
	}
	text := cause.Error()
	if len(text) > 400 {
		text = text[:400]
	}
	return text
}
