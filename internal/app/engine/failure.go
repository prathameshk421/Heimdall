package engine

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// failure builds the task.failed event for a step call that returned cause. It returns nil when the task already has
// a task.failed or task.timed_out event, so a repeated failure is not recorded twice. It returns an error when the step
// has no task to fail: a step without a role, or a role with no task assigned yet.
func (e *Engine) failure(v ChangeView, ws workflow.Step, cause error) (*change.Event, error) {
	assigned, task, ok, err := latestTask(v, ws.Role)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("step %s: %w", ws.ID, cause)
	}
	ended, err := taskEnded(v, task.TaskID)
	if err != nil {
		return nil, err
	}
	if ended {
		return nil, nil
	}
	p := change.TaskFailed{TaskID: task.TaskID, Agent: task.Agent, Role: task.Role, Reason: reason(cause)}
	return e.newEvent(v, assigned, change.EventTaskFailed, p)
}

// newEvent builds an event the engine writes itself. Its id and time come from the injected ports, and it is caused by
// the event that caused the failure.
func (e *Engine) newEvent(v ChangeView, cause change.Event, typ change.EventType, p change.Payload) (*change.Event, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", typ, err)
	}
	causation := cause.ID
	return &change.Event{
		ID:            e.ids.NewEvent(),
		ChangeID:      v.ID,
		Type:          typ,
		Actor:         systemActor(),
		OccurredAt:    e.clock.Now(),
		CausationID:   &causation,
		CorrelationID: cause.CorrelationID,
		Payload:       raw,
	}, nil
}

// latestTask returns the newest task.assigned of the role, with its event. It reports false when the role has no task.
// A step without a role has no task.
func latestTask(v ChangeView, role workflow.Role) (change.Event, change.TaskAssigned, bool, error) {
	if role == "" {
		return change.Event{}, change.TaskAssigned{}, false, nil
	}
	for i := len(v.Events) - 1; i >= 0; i-- {
		ev := v.Events[i]
		if ev.Type != change.EventTaskAssigned {
			continue
		}
		p, err := change.Decode(ev)
		if err != nil {
			return change.Event{}, change.TaskAssigned{}, false, fmt.Errorf("read event %s: %w", ev.ID, err)
		}
		task, ok := p.(change.TaskAssigned)
		if !ok {
			return change.Event{}, change.TaskAssigned{}, false, fmt.Errorf("event %s decoded as %T", ev.ID, p)
		}
		if task.Role == role {
			return ev, task, true, nil
		}
	}
	return change.Event{}, change.TaskAssigned{}, false, nil
}

// taskEnded reports whether the view holds a task.failed or a task.timed_out for the task.
func taskEnded(v ChangeView, task id.TaskID) (bool, error) {
	for _, ev := range v.Events {
		if ev.Type != change.EventTaskFailed && ev.Type != change.EventTaskTimedOut {
			continue
		}
		p, err := change.Decode(ev)
		if err != nil {
			return false, fmt.Errorf("read event %s: %w", ev.ID, err)
		}
		switch t := p.(type) {
		case change.TaskFailed:
			if t.TaskID == task {
				return true, nil
			}
		case change.TaskTimedOut:
			if t.TaskID == task {
				return true, nil
			}
		}
	}
	return false, nil
}

// reason is the text of a task.failed event: the error message, bounded and never blank.
func reason(cause error) string {
	s := cause.Error()
	if strings.TrimSpace(s) == "" {
		return "step failed without a message"
	}
	if len(s) > maxReasonLen {
		return s[:maxReasonLen]
	}
	return s
}

// systemActor is who the engine names as the cause of the events it writes itself.
func systemActor() change.Actor {
	return change.Actor{Kind: change.ActorSystem, ID: "heimdall"}
}
