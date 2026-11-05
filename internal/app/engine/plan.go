package engine

import (
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// plan is the change as the engine reads it for one decision: the view each step gets, and which steps an escalation
// holds back. A step's view starts at the latest step.retried whose step is the step itself or one it waits for, so a
// retry reopens its target and everything downstream of it (TECH_SPEC 5.1, on_fail). The view keeps that retry event,
// which carries the reasons. Done, Start and OnEvent all see the step's view, never the whole change.
type plan struct {
	e       *Engine
	full    ChangeView
	windows map[string]ChangeView
	held    map[string]bool
}

// mark is one recovery event of the change: its position in the stored events, and the workflow step it names.
type mark struct {
	at   int
	step string
}

// newPlan decodes the retries and escalations of v once, and works out each step's view and whether it is held.
func (e *Engine) newPlan(v ChangeView) (*plan, error) {
	retries, escalations, err := recoveryMarks(v.Events)
	if err != nil {
		return nil, err
	}
	p := &plan{e: e, full: v, windows: make(map[string]ChangeView, len(e.wf.Steps)), held: make(map[string]bool, len(e.wf.Steps))}
	for _, ws := range e.wf.Steps {
		scope := e.scope[ws.ID]
		cut := -1
		for _, r := range retries {
			if scope[r.step] && r.at > cut {
				cut = r.at
			}
		}
		p.windows[ws.ID] = ChangeView{ID: v.ID, Events: v.Events[max(cut, 0):]}
		p.held[ws.ID] = escalatedAndNotResumed(ws.ID, scope, retries, escalations)
	}
	return p, nil
}

// recoveryMarks returns the positions and step names of the step.retried and step.escalated events, in order.
func recoveryMarks(evs []change.Event) (retries, escalations []mark, err error) {
	for i, ev := range evs {
		if ev.Type != change.EventStepRetried && ev.Type != change.EventStepEscalated {
			continue
		}
		p, err := change.Decode(ev)
		if err != nil {
			return nil, nil, fmt.Errorf("read event %s: %w", ev.ID, err)
		}
		switch r := p.(type) {
		case change.StepRetried:
			retries = append(retries, mark{at: i, step: r.Step})
		case change.StepEscalated:
			escalations = append(escalations, mark{at: i, step: r.Step})
		default:
			return nil, nil, fmt.Errorf("event %s decoded as %T", ev.ID, p)
		}
	}
	return retries, escalations, nil
}

// escalatedAndNotResumed reports whether the step's latest escalation has no retry after it, from the step or a step it
// waits for. Such a step waits for a person: the engine does not call it, and the change is not complete.
func escalatedAndNotResumed(stepID string, scope map[string]bool, retries, escalations []mark) bool {
	last := -1
	for _, m := range escalations {
		if m.step == stepID && m.at > last {
			last = m.at
		}
	}
	if last < 0 {
		return false
	}
	for _, r := range retries {
		if scope[r.step] && r.at > last {
			return false
		}
	}
	return true
}

// finished reports whether a step is skipped, or held back is false and its Done holds on its own view.
func (p *plan) finished(ws workflow.Step) bool {
	if p.e.skipped(p.full, ws) {
		return true
	}
	if p.held[ws.ID] {
		return false
	}
	return p.e.steps[ws.Do].Done(p.windows[ws.ID])
}

// ready reports whether every step the given step waits for is finished.
func (p *plan) ready(ws workflow.Step) bool {
	for _, dep := range ws.After {
		if !p.finished(p.e.byID[dep]) {
			return false
		}
	}
	return true
}

// open reports whether a step is ready, not finished and not held. Only open steps are called.
func (p *plan) open(ws workflow.Step) bool {
	return !p.held[ws.ID] && p.ready(ws) && !p.finished(ws)
}

// complete reports whether every step is finished or skipped.
func (p *plan) complete() bool {
	for _, ws := range p.e.wf.Steps {
		if !p.finished(ws) {
			return false
		}
	}
	return true
}

// scopes maps each step id to the ids of the step and of every step it waits for, through any chain of after edges.
// The workflow is validated before this runs, so the walk ends.
func scopes(w workflow.Workflow) map[string]map[string]bool {
	after := make(map[string][]string, len(w.Steps))
	for _, s := range w.Steps {
		after[s.ID] = s.After
	}
	out := make(map[string]map[string]bool, len(w.Steps))
	for _, s := range w.Steps {
		set := map[string]bool{}
		stack := []string{s.ID}
		for len(stack) > 0 {
			cur := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if set[cur] {
				continue
			}
			set[cur] = true
			stack = append(stack, after[cur]...)
		}
		out[s.ID] = set
	}
	return out
}
