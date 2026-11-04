package engine

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// maxRetries is how many times one decision is made again after ports.ErrVersionConflict (TECH_SPEC 2.4).
const maxRetries = 5

// maxReasonLen bounds the reason of a task.failed event. It is the limit of free text in the change payloads
// (change.maxRefLen).
const maxReasonLen = 256

// Config is what New needs. Roles and Schemas are the registry the workflow is validated against (workflow.Registry).
// Clock and IDs are used only for the events the engine writes itself (task.failed); steps get their own.
type Config struct {
	Workflow   workflow.Workflow
	Steps      []Step
	Store      ports.EventStore
	Clock      ports.Clock
	IDs        ports.IDs
	Roles      []workflow.Role
	Schemas    []string
	Conditions map[string]Condition
}

// Engine runs one workflow. It holds no per-change state, so one Engine serves every change; callers serialize the
// runs of one change (TECH_SPEC 2.4).
type Engine struct {
	wf    workflow.Workflow
	steps map[string]Step          // by step type, the do field
	byID  map[string]workflow.Step // by step id
	conds map[string]Condition
	store ports.EventStore
	clock ports.Clock
	ids   ports.IDs
}

// Outcome says what one run did. Events and Effects count what the run stored, incoming events included. Timers are
// the deadlines the steps asked for, each once. Failed names the steps whose call failed in this run. Complete is true
// when every step is finished or skipped.
type Outcome struct {
	Version  int64
	Events   int
	Effects  int
	Timers   []Timer
	Failed   []string
	Complete bool
}

// New validates the workflow against the registered steps and refuses to build an engine for one that does not
// validate (fail closed, workflow.ErrPolicyInvalid).
func New(cfg Config) (*Engine, error) {
	var problems []error
	if cfg.Store == nil {
		problems = append(problems, errors.New("store is nil"))
	}
	if cfg.Clock == nil {
		problems = append(problems, errors.New("clock is nil"))
	}
	if cfg.IDs == nil {
		problems = append(problems, errors.New("ids is nil"))
	}
	steps, stepProblems := indexSteps(cfg.Steps)
	problems = append(problems, stepProblems...)
	reg := workflow.Registry{StepTypes: slices.Collect(maps.Keys(steps)), Roles: cfg.Roles, Schemas: cfg.Schemas}
	if err := cfg.Workflow.Validate(reg); err != nil {
		problems = append(problems, err)
	}
	problems = append(problems, checkConditions(cfg.Workflow, cfg.Conditions)...)
	if len(problems) > 0 {
		return nil, fmt.Errorf("engine: %w", errors.Join(problems...))
	}
	byID := make(map[string]workflow.Step, len(cfg.Workflow.Steps))
	for _, s := range cfg.Workflow.Steps {
		byID[s.ID] = s
	}
	return &Engine{
		wf:    cfg.Workflow,
		steps: steps,
		byID:  byID,
		conds: maps.Clone(cfg.Conditions),
		store: cfg.Store,
		clock: cfg.Clock,
		ids:   cfg.IDs,
	}, nil
}

// indexSteps maps each step by its type. A nil step, a step with no type and a type given twice are problems.
func indexSteps(in []Step) (map[string]Step, []error) {
	out := make(map[string]Step, len(in))
	var problems []error
	for i, s := range in {
		if s == nil {
			problems = append(problems, fmt.Errorf("steps[%d] is nil", i))
			continue
		}
		typ := s.Type()
		if typ == "" {
			problems = append(problems, fmt.Errorf("steps[%d] has no type", i))
			continue
		}
		if _, dup := out[typ]; dup {
			problems = append(problems, fmt.Errorf("step type %q is given twice", typ))
			continue
		}
		out[typ] = s
	}
	return out, problems
}

// checkConditions reports every when name the workflow uses that the config does not supply.
func checkConditions(w workflow.Workflow, conds map[string]Condition) []error {
	var problems []error
	for _, s := range w.Steps {
		if s.When == "" {
			continue
		}
		if cond, ok := conds[s.When]; !ok || cond == nil {
			problems = append(problems, fmt.Errorf("%w: step %q names condition %q, which the config does not supply",
				workflow.ErrPolicyInvalid, s.ID, s.When))
		}
	}
	return problems
}

// Advance runs the workflow for one change. incoming holds events that arrived from outside and are not stored yet.
// The engine stores them first, offers each one to the steps that are ready, and then calls every open step until a
// pass writes nothing.
func (e *Engine) Advance(ctx context.Context, changeID id.ChangeID, incoming []change.Event) (Outcome, error) {
	r := &run{e: e, cid: changeID}
	if err := r.persist(ctx, incoming); err != nil {
		return Outcome{}, err
	}
	for _, ev := range incoming {
		if err := r.offer(ctx, ev); err != nil {
			return Outcome{}, err
		}
	}
	if err := r.drive(ctx); err != nil {
		return Outcome{}, err
	}
	return r.finish(ctx)
}

// Timeout runs OnTimeout of the named step, then the rest of the run. A timer that fires after the step is finished
// finds nothing to do.
func (e *Engine) Timeout(ctx context.Context, changeID id.ChangeID, stepID string) (Outcome, error) {
	ws, ok := e.byID[stepID]
	if !ok {
		return Outcome{}, fmt.Errorf("timeout of %s: step %q is not in the workflow", changeID, stepID)
	}
	r := &run{e: e, cid: changeID}
	step := e.steps[ws.Do]
	if _, err := r.step(ctx, ws, func(v ChangeView) (Result, error) { return step.OnTimeout(ctx, v) }); err != nil {
		return Outcome{}, err
	}
	if err := r.drive(ctx); err != nil {
		return Outcome{}, err
	}
	return r.finish(ctx)
}

// run is the state of one Advance or one Timeout: the change, and what the run has stored so far.
type run struct {
	e   *Engine
	cid id.ChangeID
	out Outcome
}

// attempt is what one decision holds: the result to write, and whether the step failed on the view.
type attempt struct {
	res    Result
	failed bool
}

// persist stores the events that arrived from outside, as they are.
func (r *run) persist(ctx context.Context, evs []change.Event) error {
	if len(evs) == 0 {
		return nil
	}
	a, err := r.commit(ctx, func(ChangeView) (attempt, error) { return attempt{res: Result{Events: evs}}, nil })
	if err != nil {
		return err
	}
	r.note(a)
	return nil
}

// offer hands one incoming event to every step that is open on the view, in workflow order.
func (r *run) offer(ctx context.Context, ev change.Event) error {
	for _, ws := range r.e.wf.Steps {
		step := r.e.steps[ws.Do]
		if _, err := r.step(ctx, ws, func(v ChangeView) (Result, error) { return step.OnEvent(ctx, v, ev) }); err != nil {
			return err
		}
	}
	return nil
}

// drive calls every open step in workflow order, pass after pass, until a pass stores nothing. A step can open only
// after a later step in the list has finished, so one pass is not always enough. A run that still stores events after
// one pass per step has a step that never settles, and it fails.
func (r *run) drive(ctx context.Context) error {
	limit := len(r.e.wf.Steps) + 1
	for pass := 1; ; pass++ {
		wrote := false
		for _, ws := range r.e.wf.Steps {
			step := r.e.steps[ws.Do]
			stored, err := r.step(ctx, ws, func(v ChangeView) (Result, error) { return step.Start(ctx, v, ws.With) })
			if err != nil {
				return err
			}
			wrote = wrote || stored
		}
		if !wrote {
			return nil
		}
		if pass >= limit {
			return fmt.Errorf("run of %s: steps still storing events after %d passes", r.cid, pass)
		}
	}
}

// step runs one call of a workflow step, stores what it returns and records the outcome. It reports whether the call
// stored an event or an effect.
func (r *run) step(ctx context.Context, ws workflow.Step, call func(ChangeView) (Result, error)) (bool, error) {
	a, err := r.commit(ctx, r.stepDecision(ctx, ws, call))
	if err != nil {
		return false, err
	}
	stored := len(a.res.Events)+len(a.res.Effects) > 0
	r.noteStep(ws.ID, a)
	return stored, nil
}

// stepDecision is one call of a step on a view. The call runs only when the step is open there. A failed call becomes
// a task.failed event for the step's task (see failure).
func (r *run) stepDecision(ctx context.Context, ws workflow.Step, call func(ChangeView) (Result, error)) func(ChangeView) (attempt, error) {
	return func(v ChangeView) (attempt, error) {
		if !r.e.open(v, ws) {
			return attempt{}, nil
		}
		res, err := call(v)
		if err == nil {
			return attempt{res: res}, nil
		}
		if ctx.Err() != nil {
			return attempt{}, fmt.Errorf("step %s of %s: %w", ws.ID, r.cid, err)
		}
		ev, err := r.e.failure(v, ws, err)
		if err != nil {
			return attempt{}, err
		}
		a := attempt{failed: true}
		if ev != nil {
			a.res = Result{Events: []change.Event{*ev}}
		}
		return a, nil
	}
}

// commit asks decide for an attempt on the change as it is now and stores what the attempt holds. A version conflict
// means another writer moved the change first, so commit loads the change again and decides again, up to maxRetries
// times (TECH_SPEC 2.4).
func (r *run) commit(ctx context.Context, decide func(ChangeView) (attempt, error)) (attempt, error) {
	for retries := 0; ; retries++ {
		if err := ctx.Err(); err != nil {
			return attempt{}, fmt.Errorf("change %s: %w", r.cid, err)
		}
		v, err := r.load(ctx)
		if err != nil {
			return attempt{}, err
		}
		a, err := decide(v)
		if err != nil {
			return attempt{}, err
		}
		err = r.write(ctx, v, a.res)
		if err == nil {
			return a, nil
		}
		if !errors.Is(err, ports.ErrVersionConflict) || retries == maxRetries {
			return attempt{}, err
		}
	}
}

// write appends the events and effects of res at the version the view holds. An empty result appends nothing.
func (r *run) write(ctx context.Context, v ChangeView, res Result) error {
	if len(res.Events) == 0 {
		if len(res.Effects) > 0 {
			return fmt.Errorf("change %s: effects without an event: an effect is committed with the event that causes it", r.cid)
		}
		return nil
	}
	evs := slices.Clone(res.Events)
	expected := int64(len(v.Events))
	for i := range evs {
		evs[i].Version = expected + int64(i) + 1
	}
	if _, err := r.e.store.Append(ctx, r.cid, expected, evs, res.Effects); err != nil {
		return fmt.Errorf("append to %s at version %d: %w", r.cid, expected, err)
	}
	return nil
}

// load reads the change as the store holds it now.
func (r *run) load(ctx context.Context) (ChangeView, error) {
	evs, err := r.e.store.Load(ctx, r.cid)
	if err != nil {
		return ChangeView{}, fmt.Errorf("load %s: %w", r.cid, err)
	}
	return ChangeView{ID: r.cid, Events: evs}, nil
}

// note adds what a stored attempt holds to the outcome of the run, for the calls that are not steps.
func (r *run) note(a attempt) {
	r.noteStep("", a)
}

// noteStep adds what a stored attempt holds to the outcome of the run, and names the step if the attempt failed.
func (r *run) noteStep(stepID string, a attempt) {
	r.out.Events += len(a.res.Events)
	r.out.Effects += len(a.res.Effects)
	for _, t := range a.res.Timers {
		r.addTimer(t)
	}
	if a.failed && !slices.Contains(r.out.Failed, stepID) {
		r.out.Failed = append(r.out.Failed, stepID)
	}
}

// addTimer records a deadline once, even when the step asks for it again on a later pass.
func (r *run) addTimer(t Timer) {
	for _, have := range r.out.Timers {
		if have.Step == t.Step && have.Due.Equal(t.Due) {
			return
		}
	}
	r.out.Timers = append(r.out.Timers, t)
}

// finish reads the change once more and fills in the version and completion of the outcome.
func (r *run) finish(ctx context.Context) (Outcome, error) {
	v, err := r.load(ctx)
	if err != nil {
		return Outcome{}, err
	}
	r.out.Version = int64(len(v.Events))
	r.out.Complete = r.e.complete(v)
	return r.out, nil
}

// complete reports whether every step is finished or skipped on the view.
func (e *Engine) complete(v ChangeView) bool {
	for _, ws := range e.wf.Steps {
		if !e.finished(v, ws) {
			return false
		}
	}
	return true
}

// open reports whether a step is ready and not finished on the view. Only open steps are called.
func (e *Engine) open(v ChangeView, ws workflow.Step) bool {
	return e.ready(v, ws) && !e.finished(v, ws)
}

// ready reports whether every step the given step waits for is finished on the view.
func (e *Engine) ready(v ChangeView, ws workflow.Step) bool {
	for _, dep := range ws.After {
		if !e.finished(v, e.byID[dep]) {
			return false
		}
	}
	return true
}

// finished reports whether a step is skipped or done on the view. A skipped step counts as finished for the steps
// after it (step.go).
func (e *Engine) finished(v ChangeView, ws workflow.Step) bool {
	return e.skipped(v, ws) || e.steps[ws.Do].Done(v)
}

// skipped reports whether the step's when condition is false on the view.
func (e *Engine) skipped(v ChangeView, ws workflow.Step) bool {
	return ws.When != "" && !e.conds[ws.When](v)
}
