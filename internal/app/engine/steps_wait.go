package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

// Wait is the workflow step "wait": it waits until a specified event appears in the window.
type Wait struct {
	event change.EventType
}

var _ Step = Wait{}

type waitWith struct {
	Event string `json:"event"`
}

// NewWait reads the with object of the wait step and refuses one that does not validate.
func NewWait(with json.RawMessage) (Wait, error) {
	var w waitWith
	if err := decodeWith(with, []string{"event"}, &w); err != nil {
		return Wait{}, fmt.Errorf("wait with: %w", err)
	}
	et := change.EventType(w.Event)
	if !et.Valid() {
		return Wait{}, fmt.Errorf("wait with: unknown event type %q", w.Event)
	}
	return Wait{event: et}, nil
}

// Type returns the do name of the step.
func (Wait) Type() string { return "wait" }

// Done reports whether the event named in with.event exists in the window.
func (w Wait) Done(c ChangeView) bool {
	return c.Has(w.event)
}

// Start returns nothing: the step only waits.
func (Wait) Start(context.Context, ChangeView, json.RawMessage) (Result, error) {
	return Result{}, nil
}

// OnEvent returns nothing: the step has no effects or events to commit.
func (Wait) OnEvent(context.Context, ChangeView, change.Event) (Result, error) {
	return Result{}, nil
}

// OnTimeout returns nothing: the step has no deadline.
func (Wait) OnTimeout(context.Context, ChangeView) (Result, error) {
	return Result{}, nil
}
