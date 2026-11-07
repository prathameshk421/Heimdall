package main

import (
	"fmt"

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

// resumeEvents releases an escalated step. It refuses a step that is not escalated now, so a resume cannot restart a
// step that is running. The one event it writes, step.resumed, names the person who released it.
func (r *commandRuntime) resumeEvents(cid id.ChangeID, state []change.Event, person string, source commands.Source, sourceRef string, actor change.Actor, cmd commands.Resume) ([]change.Event, error) {
	escalated, err := stepEscalatedNow(state, cmd.Step)
	if err != nil {
		return nil, fmt.Errorf("read step %s: %w", cmd.Step, err)
	}
	if !escalated {
		return nil, fmt.Errorf("step %s is not escalated, so there is nothing to resume", cmd.Step)
	}
	ev, err := r.event(cid, actor, source, change.EventStepResumed, change.StepResumed{Step: cmd.Step, By: change.ActorID(person)}, sourceRef)
	if err != nil {
		return nil, err
	}
	return []change.Event{ev}, nil
}
