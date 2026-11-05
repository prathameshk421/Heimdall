package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// PickReviewersSpec configures the human.pick_reviewers step.
type PickReviewersSpec struct {
	ID               string
	Timeout          time.Duration
	Clock            ports.Clock
	IDs              ports.IDs
	DefaultReviewers func(c ChangeView) []change.ActorID
}

// PickReviewers is the step "human.pick_reviewers": it waits for reviewers to be picked by a person,
// and falls back to default reviewers on timeout.
type PickReviewers struct {
	spec PickReviewersSpec
}

var _ Step = PickReviewers{}

// NewPickReviewers creates a new human.pick_reviewers step.
func NewPickReviewers(spec PickReviewersSpec) (PickReviewers, error) {
	if spec.ID == "" {
		return PickReviewers{}, errors.New("human.pick_reviewers: step id is required")
	}
	if spec.Timeout <= 0 {
		return PickReviewers{}, errors.New("human.pick_reviewers: timeout must be positive")
	}
	if spec.Clock == nil {
		return PickReviewers{}, errors.New("human.pick_reviewers: clock is required")
	}
	if spec.IDs == nil {
		return PickReviewers{}, errors.New("human.pick_reviewers: ids is required")
	}
	if spec.DefaultReviewers == nil {
		return PickReviewers{}, errors.New("human.pick_reviewers: DefaultReviewers is required")
	}
	return PickReviewers{spec: spec}, nil
}

// Type returns the do name of the step.
func (PickReviewers) Type() string { return "human.pick_reviewers" }

// Done reports whether reviewers.picked exists in the window.
func (PickReviewers) Done(c ChangeView) bool {
	return c.Has(change.EventReviewersPicked)
}

// Start does nothing except returning the deadline timer if the step is not done.
func (s PickReviewers) Start(_ context.Context, c ChangeView, _ json.RawMessage) (Result, error) {
	if s.Done(c) {
		return Result{}, nil
	}
	anchor := s.spec.Clock.Now()
	for i := len(c.Events) - 1; i >= 0; i-- {
		if c.Events[i].Type == change.EventSlopEvaluated {
			anchor = c.Events[i].OccurredAt
			break
		}
	}
	deadline := anchor.Add(s.spec.Timeout)
	return Result{Timers: []Timer{{Step: s.spec.ID, Due: deadline}}}, nil
}

// OnEvent returns nothing: human selection arrives as reviewers.picked from outside.
func (PickReviewers) OnEvent(context.Context, ChangeView, change.Event) (Result, error) {
	return Result{}, nil
}

// OnTimeout returns an escalation-free default: a reviewers.picked event with default reviewers (method=default).
func (s PickReviewers) OnTimeout(_ context.Context, c ChangeView) (Result, error) {
	if s.Done(c) {
		return Result{}, nil
	}
	people := s.spec.DefaultReviewers(c)
	if len(people) == 0 {
		return Result{}, errors.New("human.pick_reviewers: no default reviewers provided")
	}
	p := change.ReviewersPicked{
		People: people,
		By:     "heimdall",
		Method: change.PickDefault,
	}
	if err := p.Validate(); err != nil {
		return Result{}, fmt.Errorf("human.pick_reviewers: %w", err)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return Result{}, fmt.Errorf("human.pick_reviewers: encode payload: %w", err)
	}
	now := s.spec.Clock.Now()
	ev := change.Event{
		ID:            s.spec.IDs.NewEvent(),
		ChangeID:      c.ID,
		Type:          change.EventReviewersPicked,
		Actor:         systemActor(),
		OccurredAt:    now,
		CausationID:   lastEventID(c),
		CorrelationID: lastCorrelationID(c),
		Payload:       raw,
	}
	return Result{Events: []change.Event{ev}}, nil
}
