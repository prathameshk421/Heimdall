package engine

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// ChangeView is what a step reads: the stored events of one change, followed by the events of the current run, each
// numbered as it will be stored. A step never reads the database (TECH_SPEC 5.2).
type ChangeView struct {
	ID     id.ChangeID
	Events []change.Event
	// History is every stored event of the change. Events can be a window that starts at a retry (plan.go); History is
	// for facts of the whole change, such as its room, and never for what the step has done. Empty means Events is
	// already the whole change.
	History []change.Event
}

// history returns every stored event of the change: History when the view is a window, else Events.
func (c ChangeView) history() []change.Event {
	if len(c.History) > 0 {
		return c.History
	}
	return c.Events
}

// Has reports whether the view holds an event of any of the given types.
func (c ChangeView) Has(types ...change.EventType) bool {
	for _, e := range c.Events {
		if slices.Contains(types, e.Type) {
			return true
		}
	}
	return false
}

// Timer asks the caller to call Engine.Timeout for Step at Due. The engine never waits; the caller schedules it.
type Timer struct {
	Step string
	Due  time.Time
}

// Result is what a step hands back: the events it decided, the effects to commit with them, and the timers it wants.
// The engine persists them; a step does no I/O (TECH_SPEC 5.2).
type Result struct {
	Events  []change.Event
	Effects []change.Effect
	Timers  []Timer
}

// Step is one stage of a workflow (TECH_SPEC 5.2). The spec lists Type, Start, OnEvent and OnTimeout. Done is added:
// the engine must know, from the events alone, whether a step has finished, so that a replay makes the same decisions.
// The catalog has no step-completion event, so each step reads its own output events.
type Step interface {
	// Type is the name a workflow step gives in its do field, such as "github.ensure_branch".
	Type() string
	// Done reports whether the step has finished. It must be a pure function of the view.
	Done(c ChangeView) bool
	// Start runs the step. The engine calls Start on every run until Done is true, so Start returns no events for
	// work it has already recorded. with holds the step's arguments from the workflow.
	Start(ctx context.Context, c ChangeView, with json.RawMessage) (Result, error)
	// OnEvent is offered each event that arrives in the run, while the step is ready and not done.
	OnEvent(ctx context.Context, c ChangeView, e change.Event) (Result, error)
	// OnTimeout runs when a timer of the step fires (Engine.Timeout).
	OnTimeout(ctx context.Context, c ChangeView) (Result, error)
}

// Condition evaluates the name of a workflow step's when field against the view. A step whose condition is false is
// skipped, and a skipped step counts as finished for the steps after it.
type Condition func(c ChangeView) bool
