package main

import (
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/collaboration"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/commands"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// stepEscalatedNow reports whether the workflow step's latest recovery event is an escalation. A retry or a resume
// after the escalation releases the step, so only the latest one decides (A10 item 5).
func stepEscalatedNow(state []change.Event, step string) (bool, error) {
	latest := change.EventType("")
	for _, ev := range state {
		if ev.Type != change.EventStepEscalated && ev.Type != change.EventStepRetried && ev.Type != change.EventStepResumed {
			continue
		}
		p, err := change.Decode(ev)
		if err != nil {
			return false, fmt.Errorf("decode %s: %w", ev.Type, err)
		}
		var name string
		switch v := p.(type) {
		case change.StepEscalated:
			name = v.Step
		case change.StepRetried:
			name = v.Step
		case change.StepResumed:
			name = v.Step
		}
		if name == step {
			latest = ev.Type
		}
	}
	return latest == change.EventStepEscalated, nil
}

// resumable reports whether a person may start a new round of step: it escalated, or its seats used up their failures
// and the row is capped (D173). The cap asks people to resume and never writes step.escalated itself.
func resumable(state []change.Event, step string) (bool, error) {
	escalated, err := stepEscalatedNow(state, step)
	if err != nil {
		return false, err
	}
	if escalated {
		return true, nil
	}
	view, err := change.Fold(state)
	if err != nil {
		return false, fmt.Errorf("read the change for step %s: %w", step, err)
	}
	return change.RowCapped(view, step), nil
}

// resumeEvents releases an escalated or capped step. It refuses a step that is neither, so a resume cannot restart a
// step that is running. The one event it writes, step.resumed, names the person who released it.
func (r *commandRuntime) resumeEvents(cid id.ChangeID, state []change.Event, person string, source commands.Source, sourceRef string, actor change.Actor, cmd commands.Resume) ([]change.Event, error) {
	ok, err := resumable(state, cmd.Step)
	if err != nil {
		return nil, fmt.Errorf("read step %s: %w", cmd.Step, err)
	}
	if !ok {
		return nil, collaboration.Refuse(fmt.Sprintf("step %s is not escalated and has not used up its tasks, so there is nothing to resume", cmd.Step))
	}
	ev, err := r.event(cid, actor, source, change.EventStepResumed, change.StepResumed{Step: cmd.Step, By: change.ActorID(person)}, sourceRef)
	if err != nil {
		return nil, err
	}
	return []change.Event{ev}, nil
}
