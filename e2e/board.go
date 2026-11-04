package e2e

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Status is the outcome of one step.
type Status int

// The outcomes. Blocked is never counted as green.
const (
	StatusPass Status = iota
	StatusRed
	StatusBlocked
	StatusNotReached
	StatusManual
)

func (s Status) String() string {
	switch s {
	case StatusPass:
		return "pass"
	case StatusRed:
		return "red"
	case StatusBlocked:
		return "blocked"
	case StatusNotReached:
		return "not reached"
	case StatusManual:
		return "manual"
	}
	return "unknown"
}

// Env is what a step gets: the world, the story it runs on, and the capture it adds what a person saw to.
type Env struct {
	World   World
	Story   *Story
	Capture *Capture
	// Vars holds what the steps of one scenario share, such as the ticket key, the room and the PR. The board
	// makes a new map for each scenario, so two scenarios never see each other's values.
	Vars map[string]any
}

// Set stores a value for the later steps of this scenario.
func (e *Env) Set(key string, v any) {
	if e.Vars == nil {
		e.Vars = map[string]any{}
	}
	e.Vars[key] = v
}

// Get returns a value an earlier step of this scenario stored, and whether it is there with that type.
func Get[T any](e *Env, key string) (T, bool) {
	v, ok := e.Vars[key].(T)
	return v, ok
}

// Step is one thing a person does and what they must see. Run asserts only what a person could see.
type Step struct {
	ID    string        // P2.1
	Does  string        // what the person does
	Sees  string        // what they must see
	Tags  []string      // local, vm, scripted, live, manual
	Needs []Need        // prerequisites, probed before the step runs
	Bar   time.Duration // the spec's latency bar; zero means none is stated
	// NoStory marks a step that never reads the story (the command line, configuration), so an incomplete story does
	// not block it.
	NoStory bool
	After   string // id of the earlier step in this scenario whose result this one depends on; empty if independent
	Run     func(ctx context.Context, e *Env) error
}

// Scenario is an ordered list of steps (P1 to P9).
type Scenario struct {
	ID     string
	Title  string
	Steps  []Step
	Budget Budget // messages the product may post in this scenario (quality check 4)
}

// Result is what the board records for one step.
type Result struct {
	Step       string
	Status     Status
	Why        string // "expected X, saw Y", the blocked prerequisite and reason, or the failed quality check
	BlockedOn  Need
	Task       string // the task that builds a missing driver, when that is the reason
	Elapsed    time.Duration
	Bar        time.Duration
	CostUSD    float64
	Violations []Violation
	CostKnown  bool
}

// ScenarioResult is the results of one scenario's steps in order.
type ScenarioResult struct {
	ID      string
	Title   string
	Results []Result
	Quality []Violation // scenario-level quality checks: quiet, agents stay out, idempotent replay
	// Unmeasured names the scenario-level checks that could not be measured yet, and why.
	Unmeasured []string
}

// Board runs scenarios against a world and a story.
type Board struct {
	World World
	Story *Story
	// Now is the clock for latency; tests inject a fake.
	Now func() time.Time
	// Scripted says the agents are scripted, so every step must cost $0 (quality check 8).
	Scripted bool

	probed map[Need]Availability
}

// Run executes the scenarios in order and returns their results.
func (b *Board) Run(ctx context.Context, scenarios []Scenario) []ScenarioResult {
	out := make([]ScenarioResult, 0, len(scenarios))
	for _, sc := range scenarios {
		out = append(out, b.runScenario(ctx, sc))
	}
	return out
}

func (b *Board) runScenario(ctx context.Context, sc Scenario) ScenarioResult {
	res := ScenarioResult{ID: sc.ID, Title: sc.Title}
	status := map[string]Status{}
	capture := &Capture{}
	vars := map[string]any{}
	for _, st := range sc.Steps {
		r := b.runStep(ctx, st, status, capture, vars)
		status[st.ID] = r.Status
		res.Results = append(res.Results, r)
	}
	if executed(res.Results) {
		res.Quality, res.Unmeasured = b.scenarioChecks(ctx, sc, capture)
	}
	return res
}

// executed reports whether any step really ran (passed or failed), as opposed to being blocked.
func executed(rs []Result) bool {
	for _, r := range rs {
		if r.Status == StatusPass || r.Status == StatusRed {
			return true
		}
	}
	return false
}

// scenarioChecks runs the checks that need the whole scenario: the message budget, agents staying out of the
// room's decisions, and a replay of the poller that must change nothing.
func (b *Board) scenarioChecks(ctx context.Context, sc Scenario, c *Capture) (violations []Violation, unmeasured []string) {
	msgs := c.Messages()
	for _, p := range QuietProblems(msgs, sc.Budget) {
		violations = append(violations, Violation{CheckQuiet, p})
	}
	for _, p := range AgentLeakProblems(msgs, b.Story.Canaries, c.AllSubmitted()) {
		violations = append(violations, Violation{CheckAgentsStay, p})
	}
	obs := b.World.Server()
	before, err := obs.Counts(ctx)
	if err == nil {
		err = obs.Replay(ctx)
	}
	var after Counts
	if err == nil {
		after, err = obs.Counts(ctx)
	}
	if err != nil {
		return violations, append(unmeasured, CheckReplay+": "+err.Error())
	}
	for _, p := range ReplayProblems(before, after) {
		violations = append(violations, Violation{CheckReplay, p})
	}
	return violations, unmeasured
}

func (b *Board) runStep(ctx context.Context, st Step, done map[string]Status, capture *Capture, vars map[string]any) Result {
	r := Result{Step: st.ID, Bar: st.Bar}
	if b.blocked(ctx, st, &r) {
		return r
	}
	if st.After != "" && done[st.After] != StatusPass {
		r.Status = StatusNotReached
		r.Why = fmt.Sprintf("%s did not pass", st.After)
		return r
	}
	before := capture.Len()
	spent0, spendErr := b.World.Server().Spend(ctx)
	started := b.Now()
	err := st.Run(ctx, &Env{World: b.World, Story: b.Story, Capture: capture, Vars: vars})
	r.Elapsed = b.Now().Sub(started)
	if spendErr == nil {
		if spent1, e := b.World.Server().Spend(ctx); e == nil {
			r.CostKnown, r.CostUSD = true, max(spent1-spent0, 0)
		}
	}
	b.judge(&r, st, err, capture.CheckFrom(before))
	return r
}

// blocked fills r and returns true when a prerequisite is missing or the story is incomplete. The label is the first
// missing prerequisite in the step's own order (the story last), and the reason also lists every other one, so the
// board shows all that is missing rather than only the first. Every step reports its own blockers, whatever happened
// to the steps before it.
func (b *Board) blocked(ctx context.Context, st Step, r *Result) bool {
	type gap struct {
		need Need
		why  string
		task string
	}
	var gaps []gap
	for _, need := range st.Needs {
		if av := b.probe(ctx, need); !av.OK {
			gaps = append(gaps, gap{need: need, why: av.Why})
		}
	}
	if b.Story.Pending != "" && !st.NoStory {
		gaps = append(gaps, gap{
			need: Need("story"), task: b.Story.Pending,
			why: fmt.Sprintf("story %s is incomplete until %s", b.Story.Name, b.Story.Pending),
		})
	}
	if len(gaps) == 0 {
		return false
	}
	r.Status, r.BlockedOn, r.Task, r.Why = StatusBlocked, gaps[0].need, gaps[0].task, gaps[0].why
	if len(gaps) > 1 {
		var also []string
		for _, g := range gaps[1:] {
			label := string(g.need)
			if g.task != "" {
				label += " " + g.task
			}
			also = append(also, label)
		}
		r.Why += "; also missing: " + strings.Join(also, ", ")
	}
	return true
}

// probe asks the world whether a prerequisite holds, once per board run: a probe can call GitHub or the shell.
func (b *Board) probe(ctx context.Context, n Need) Availability {
	if av, ok := b.probed[n]; ok {
		return av
	}
	if b.probed == nil {
		b.probed = map[Need]Availability{}
	}
	av := b.World.Prereq(ctx, n)
	b.probed[n] = av
	return av
}

// judge turns what a step returned, how long it took, what it cost and what a person saw into a status.
func (b *Board) judge(r *Result, st Step, err error, seen []Violation) {
	if err != nil {
		if task, ok := IsNotBuilt(err); ok {
			r.Status, r.BlockedOn, r.Task, r.Why = StatusBlocked, Need("driver"), task, err.Error()
			return
		}
		r.Status, r.Why = StatusRed, failure(err)
		return
	}
	if st.Bar > 0 && r.Elapsed > st.Bar {
		r.Status, r.Why = StatusRed, (&Red{Expected: "within " + st.Bar.String(), Saw: r.Elapsed.String()}).Error()
		return
	}
	if r.CostKnown {
		for _, p := range CostProblems(b.Scripted, r.CostUSD) {
			seen = append(seen, Violation{CheckCost, p})
		}
	}
	if len(seen) > 0 {
		r.Status, r.Violations, r.Why = StatusRed, seen, "quality: "+seen[0].String()
		return
	}
	r.Status = StatusPass
}

func failure(err error) string {
	var red *Red
	if errors.As(err, &red) {
		return red.Error()
	}
	return "error: " + err.Error()
}
