package engine

import (
	"context"
	"encoding/json"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

// Slop is the workflow step "rule.slop": it checks whether the PR passed the slop rule.
// Intake evaluates slop and writes slop.evaluated; this step only reads the event.
type Slop struct{}

var _ Step = Slop{}

// NewSlop creates a new rule.slop step.
func NewSlop() Slop {
	return Slop{}
}

// Type returns the do name of the step.
func (Slop) Type() string { return "rule.slop" }

// Done reports whether slop.evaluated exists with pass true in the window.
// If the latest slop.evaluated in the window has pass false, the step is not Done.
func (Slop) Done(c ChangeView) bool {
	for i := len(c.Events) - 1; i >= 0; i-- {
		e := c.Events[i]
		if e.Type != change.EventSlopEvaluated {
			continue
		}
		p, err := change.Decode(e)
		if err != nil {
			return false
		}
		if se, ok := p.(change.SlopEvaluated); ok {
			return se.Pass
		}
	}
	return false
}

// Start returns nothing: a failed slop is final (on_fail: reject in the workflow),
// so a failed slop leaves the step not Done and Start returns nothing (waiting for a new push).
func (Slop) Start(context.Context, ChangeView, json.RawMessage) (Result, error) {
	return Result{}, nil
}

// OnEvent returns nothing: slop.evaluated is written by intake, not decided here.
func (Slop) OnEvent(context.Context, ChangeView, change.Event) (Result, error) {
	return Result{}, nil
}

// OnTimeout returns nothing: the step has no timer.
func (Slop) OnTimeout(context.Context, ChangeView) (Result, error) {
	return Result{}, nil
}
