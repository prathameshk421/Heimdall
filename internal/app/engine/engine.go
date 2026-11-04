package engine

import (
	"context"
	"errors"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Config is what New needs. Roles and Schemas are the registry the workflow is validated against (workflow.Registry).
type Config struct {
	Workflow   workflow.Workflow
	Steps      []Step
	Store      ports.EventStore
	Roles      []workflow.Role
	Schemas    []string
	Conditions map[string]Condition
}

// Engine runs one workflow. It holds no per-change state, so one Engine serves every change; callers serialize the
// runs of one change (TECH_SPEC 2.4).
type Engine struct{}

// Outcome says what one run did.
type Outcome struct {
	Version  int64
	Events   int
	Effects  int
	Timers   []Timer
	Complete bool
}

// New validates the workflow against the registered steps and refuses to build an engine for one that does not
// validate (fail closed, workflow.ErrPolicyInvalid).
func New(cfg Config) (*Engine, error) {
	return nil, errors.New("not implemented")
}

// Advance runs the workflow for one change. incoming holds events that arrived from outside and are not stored yet.
func (e *Engine) Advance(ctx context.Context, changeID id.ChangeID, incoming []change.Event) (Outcome, error) {
	return Outcome{}, errors.New("not implemented")
}

// Timeout runs OnTimeout of the named step, then the rest of the run.
func (e *Engine) Timeout(ctx context.Context, changeID id.ChangeID, stepID string) (Outcome, error) {
	return Outcome{}, errors.New("not implemented")
}
