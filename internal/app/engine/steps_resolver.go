package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// ResolverFanout creates one scoped resolver task for each open challenge case.
type ResolverFanout struct {
	id      string
	agent   id.AgentName
	timeout time.Duration
	clock   ports.Clock
	ids     AgentTaskIDs
	bundle  func(id.ChangeID, id.CaseID, []change.Event) ([]byte, workflow.BlobRef, error)
}

// NewResolverFanout builds the challenge resolver step. Each open case gets one resolver task carrying its CaseID.
func NewResolverFanout(stepID string, agent id.AgentName, timeout time.Duration, clock ports.Clock, ids AgentTaskIDs, build func(id.ChangeID, id.CaseID, []change.Event) ([]byte, workflow.BlobRef, error)) (ResolverFanout, error) {
	if stepID == "" || timeout <= 0 || clock == nil || ids == nil || build == nil {
		return ResolverFanout{}, errors.New("deliberation.run: step id, resolver, timeout, clock, ids and bundle builder are required")
	}
	return ResolverFanout{id: stepID, agent: agent, timeout: timeout, clock: clock, ids: ids, bundle: build}, nil
}

// Type is the workflow step type the resolver fan-out implements.
func (ResolverFanout) Type() string { return "deliberation.run" }

// Done is false: the step stays active so a new challenge after the first also wakes its resolver.
func (ResolverFanout) Done(ChangeView) bool { return false }

// Start assigns a resolver task for every open case that has no resolver assignment yet.
func (s ResolverFanout) Start(_ context.Context, c ChangeView, _ json.RawMessage) (Result, error) {
	return s.assign(c)
}

// OnEvent reacts to case.opened by assigning that case's resolver.
func (s ResolverFanout) OnEvent(_ context.Context, c ChangeView, e change.Event) (Result, error) {
	if e.Type != change.EventCaseOpened {
		return Result{}, nil
	}
	return s.assign(c)
}

// OnTimeout is a no-op: the resolver's deadline is enforced by the task effect, not by this step.
func (ResolverFanout) OnTimeout(context.Context, ChangeView) (Result, error) { return Result{}, nil }
func (s ResolverFanout) assign(c ChangeView) (Result, error) {
	var result Result
	for _, caseID := range openCases(c) {
		if hasResolverAssignment(c, caseID) {
			continue
		}
		if !s.agent.Valid() {
			return Result{}, errors.New("deliberation.run: resolver agent is not configured")
		}
		bundle, ref, err := s.bundle(c.ID, caseID, c.Events)
		if err != nil {
			return Result{}, fmt.Errorf("deliberation.run: build case %s bundle: %w", caseID, err)
		}
		task := s.ids.NewTask()
		deadline := s.clock.Now().Add(s.timeout)
		p := change.TaskAssigned{TaskID: task, CaseID: caseID, Agent: s.agent, Role: workflow.RoleResolver, Deadline: deadline, BundleRef: ref}
		raw, err := json.Marshal(p)
		if err != nil {
			return Result{}, fmt.Errorf("deliberation.run: encode assignment: %w", err)
		}
		cause := latestCaseEvent(c, caseID)
		ev := change.Event{ID: s.ids.NewEvent(), ChangeID: c.ID, Type: change.EventTaskAssigned, Actor: systemActor(), OccurredAt: s.clock.Now(), CausationID: &cause, CorrelationID: lastCorrelationID(c), BlobRefs: []workflow.BlobRef{ref}, Payload: raw}
		fxp := change.TaskAssignPayload{Agent: s.agent, Role: workflow.RoleResolver, TaskID: task, RoomID: roomIDOf(c), Bundle: bundle}
		if err := fxp.Validate(); err != nil {
			return Result{}, fmt.Errorf("deliberation.run: %w", err)
		}
		fxraw, err := json.Marshal(fxp)
		if err != nil {
			return Result{}, fmt.Errorf("deliberation.run: encode task effect: %w", err)
		}
		key, err := change.IdempotencyKey(c.ID, int64(len(c.history())+len(result.Events)+1), change.EffectTaskAssign, string(task))
		if err != nil {
			return Result{}, err
		}
		result.Events = append(result.Events, ev)
		result.Effects = append(result.Effects, change.Effect{Key: key, Kind: change.EffectTaskAssign, Target: string(ref), Payload: fxraw})
		result.Timers = append(result.Timers, Timer{Step: s.id, Due: deadline})
	}
	return result, nil
}
func openCases(c ChangeView) []id.CaseID {
	var out []id.CaseID
	seen := map[id.CaseID]bool{}
	for _, e := range c.Events {
		if e.Type != change.EventCaseOpened {
			continue
		}
		p, err := change.Decode(e)
		if err != nil {
			continue
		}
		v, ok := p.(change.CaseOpened)
		if ok && !seen[v.CaseID] {
			seen[v.CaseID] = true
			out = append(out, v.CaseID)
		}
	}
	return out
}
func latestCaseEvent(c ChangeView, caseID id.CaseID) change.EventID {
	for i := len(c.Events) - 1; i >= 0; i-- {
		e := c.Events[i]
		if e.Type != change.EventCaseOpened {
			continue
		}
		p, err := change.Decode(e)
		if err == nil {
			if v, ok := p.(change.CaseOpened); ok && v.CaseID == caseID {
				return e.ID
			}
		}
	}
	return ""
}
func hasResolverAssignment(c ChangeView, caseID id.CaseID) bool {
	for _, e := range c.Events {
		if e.Type != change.EventTaskAssigned {
			continue
		}
		p, err := change.Decode(e)
		if err == nil {
			if v, ok := p.(change.TaskAssigned); ok && v.Role == workflow.RoleResolver && v.CaseID == caseID {
				return true
			}
		}
	}
	return false
}
