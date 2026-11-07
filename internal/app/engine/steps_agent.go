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

// AgentTaskIDs is the ID generator needed by agent steps.
type AgentTaskIDs interface {
	NewTask() id.TaskID
	NewEvent() change.EventID
}

// AgentTaskSpec configures the agent.task step.
type AgentTaskSpec struct {
	ID       string
	Role     workflow.Role
	Timeout  time.Duration
	Output   change.EventType
	AgentFor func(role workflow.Role) (id.AgentName, bool)
	Clock    ports.Clock
	IDs      AgentTaskIDs
	Bundle   BundleBuilder
}

// BundleBuilder is the canonical context-bundle seam. The engine decides and
// the configured app bundle package owns projection, canonicalization and hashing.
type BundleBuilder func(workflow.Role, id.ChangeID, []change.Event) ([]byte, workflow.BlobRef, error)

// AgentTask is the step agent.task.
type AgentTask struct {
	spec AgentTaskSpec
}

var _ Step = AgentTask{}

// NewAgentTask creates a new agent.task step.
func NewAgentTask(spec AgentTaskSpec) (AgentTask, error) {
	if spec.ID == "" {
		return AgentTask{}, errors.New("agent.task: step id is required")
	}
	if spec.Role == "" {
		return AgentTask{}, errors.New("agent.task: role is required")
	}
	if spec.Timeout <= 0 {
		return AgentTask{}, errors.New("agent.task: timeout must be positive")
	}
	if spec.Clock == nil {
		return AgentTask{}, errors.New("agent.task: clock is required")
	}
	if spec.IDs == nil {
		return AgentTask{}, errors.New("agent.task: ids is required")
	}
	if spec.AgentFor == nil {
		return AgentTask{}, errors.New("agent.task: AgentFor is required")
	}
	if spec.Output == "" {
		if spec.Role == workflow.RoleTestAuthor {
			spec.Output = change.EventTestsSubmitted
		} else {
			return AgentTask{}, errors.New("agent.task: output is required")
		}
	}
	if _, ok := spec.AgentFor(spec.Role); !ok {
		return AgentTask{}, fmt.Errorf("unknown agent role %q", spec.Role)
	}
	if spec.Bundle == nil {
		return AgentTask{}, errors.New("agent.task: bundle builder is required")
	}
	return AgentTask{spec: spec}, nil
}

// Type returns the do name of the step.
func (AgentTask) Type() string { return "agent.task" }

// Done reports whether the step output event is present.
func (s AgentTask) Done(c ChangeView) bool {
	return c.Has(s.spec.Output)
}

// Start creates the task.assigned event and task_assign effect if not already assigned.
func (s AgentTask) Start(_ context.Context, c ChangeView, _ json.RawMessage) (Result, error) {
	if s.Done(c) {
		return Result{}, nil
	}
	if hasTaskForRole(c, s.spec.Role) {
		// Keep reporting the open task's deadline, so a timer loop that recomputes deadlines after a restart still
		// fires it (before this the deadline was reported only once, at assignment).
		if due, ok := latestTaskDeadline(c, s.spec.Role); ok {
			return Result{Timers: []Timer{{Step: s.spec.ID, Due: due}}}, nil
		}
		return Result{}, nil
	}
	agent, ok := s.spec.AgentFor(s.spec.Role)
	if !ok {
		return Result{}, fmt.Errorf("unknown agent role %q", s.spec.Role)
	}
	return s.buildTaskResult(c, agent)
}

func (s AgentTask) buildTaskResult(c ChangeView, agent id.AgentName) (Result, error) {
	taskID := s.spec.IDs.NewTask()
	now := s.spec.Clock.Now()
	deadline := now.Add(s.spec.Timeout)

	bundleBytes, bundleRef, err := s.spec.Bundle(s.spec.Role, c.ID, c.Events)
	if err != nil {
		return Result{}, fmt.Errorf("agent.task %s: build bundle: %w", s.spec.ID, err)
	}

	taskAssigned := change.TaskAssigned{
		TaskID:    taskID,
		Agent:     agent,
		Role:      s.spec.Role,
		Deadline:  deadline,
		BundleRef: bundleRef,
	}
	if err := taskAssigned.Validate(); err != nil {
		return Result{}, fmt.Errorf("agent.task %s: %w", s.spec.ID, err)
	}
	rawTaskAssigned, err := json.Marshal(taskAssigned)
	if err != nil {
		return Result{}, fmt.Errorf("agent.task %s: encode task.assigned: %w", s.spec.ID, err)
	}

	taskEvent := change.Event{
		ID:            s.spec.IDs.NewEvent(),
		ChangeID:      c.ID,
		Type:          change.EventTaskAssigned,
		Actor:         systemActor(),
		OccurredAt:    now,
		CausationID:   lastEventID(c),
		CorrelationID: lastCorrelationID(c),
		BlobRefs:      []workflow.BlobRef{bundleRef},
		Payload:       rawTaskAssigned,
	}

	fx, err := s.buildEffect(c, agent, taskID, bundleBytes, bundleRef)
	if err != nil {
		return Result{}, err
	}

	timer := Timer{
		Step: s.spec.ID,
		Due:  deadline,
	}
	return Result{
		Events:  []change.Event{taskEvent},
		Effects: []change.Effect{fx},
		Timers:  []Timer{timer},
	}, nil
}

func (s AgentTask) buildEffect(c ChangeView, agent id.AgentName, taskID id.TaskID, bundleBytes []byte, bundleRef workflow.BlobRef) (change.Effect, error) {
	payload := change.TaskAssignPayload{
		Agent:  agent,
		Role:   s.spec.Role,
		TaskID: taskID,
		RoomID: roomIDOf(c),
		Bundle: bundleBytes,
	}
	if err := payload.Validate(); err != nil {
		return change.Effect{}, fmt.Errorf("agent.task %s: %w", s.spec.ID, err)
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return change.Effect{}, fmt.Errorf("agent.task %s: encode task_assign payload: %w", s.spec.ID, err)
	}
	version := int64(len(c.history())) + 1
	key, err := change.IdempotencyKey(c.ID, version, change.EffectTaskAssign, string(taskID))
	if err != nil {
		return change.Effect{}, fmt.Errorf("agent.task %s: %w", s.spec.ID, err)
	}
	return change.Effect{
		Key:     key,
		Kind:    change.EffectTaskAssign,
		Target:  string(bundleRef),
		Payload: rawPayload,
	}, nil
}

// OnEvent does nothing on task.acked.
func (AgentTask) OnEvent(_ context.Context, _ ChangeView, _ change.Event) (Result, error) {
	return Result{}, nil
}

// OnTimeout emits task.timed_out if the task has not finished.
func (s AgentTask) OnTimeout(_ context.Context, c ChangeView) (Result, error) {
	if s.Done(c) {
		return Result{}, nil
	}
	assignedEv, ta, ok, err := latestTask(c, s.spec.Role)
	if err != nil {
		return Result{}, fmt.Errorf("agent.task timeout %s: %w", s.spec.ID, err)
	}
	if !ok {
		return Result{}, nil
	}
	ended, err := taskEnded(c, ta.TaskID)
	if err != nil {
		return Result{}, fmt.Errorf("agent.task timeout %s: %w", s.spec.ID, err)
	}
	if ended {
		return Result{}, nil
	}

	p := change.TaskTimedOut{
		TaskID: ta.TaskID,
		Agent:  ta.Agent,
		Role:   ta.Role,
		Reason: "task timed out before completion",
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return Result{}, fmt.Errorf("agent.task timeout %s: encode payload: %w", s.spec.ID, err)
	}
	causation := assignedEv.ID
	ev := change.Event{
		ID:            s.spec.IDs.NewEvent(),
		ChangeID:      c.ID,
		Type:          change.EventTaskTimedOut,
		Actor:         systemActor(),
		OccurredAt:    s.spec.Clock.Now(),
		CausationID:   &causation,
		CorrelationID: assignedEv.CorrelationID,
		Payload:       raw,
	}
	return Result{Events: []change.Event{ev}}, nil
}

func hasTaskForRole(c ChangeView, role workflow.Role) bool {
	for _, ev := range c.Events {
		if ev.Type == change.EventTaskAssigned {
			if p, err := change.Decode(ev); err == nil {
				if ta, ok := p.(change.TaskAssigned); ok && ta.Role == role {
					return true
				}
			}
		}
	}
	return false
}

func lastEventID(c ChangeView) *change.EventID {
	if len(c.Events) == 0 {
		return nil
	}
	return &c.Events[len(c.Events)-1].ID
}

func lastCorrelationID(c ChangeView) change.CorrelationID {
	if len(c.Events) == 0 {
		return ""
	}
	return c.Events[len(c.Events)-1].CorrelationID
}

// roomIDOf returns the change's room from its whole history, because a retried step's window starts after room.opened.
func roomIDOf(c ChangeView) change.RoomID {
	for _, e := range c.history() {
		if e.Type == change.EventRoomOpened {
			if p, err := change.Decode(e); err == nil {
				if ro, ok := p.(change.RoomOpened); ok && ro.RoomID != "" {
					return ro.RoomID
				}
			}
		}
	}
	return change.RoomID(c.ID)
}

// latestTaskDeadline is the deadline of the newest task.assigned for the role in the view.
func latestTaskDeadline(c ChangeView, role workflow.Role) (time.Time, bool) {
	for i := len(c.Events) - 1; i >= 0; i-- {
		if c.Events[i].Type != change.EventTaskAssigned {
			continue
		}
		p, err := change.Decode(c.Events[i])
		if err != nil {
			continue
		}
		if ta, ok := p.(change.TaskAssigned); ok && ta.Role == role {
			return ta.Deadline, true
		}
	}
	return time.Time{}, false
}
