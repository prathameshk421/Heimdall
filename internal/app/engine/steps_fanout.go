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

// AgentFanoutSpec configures the agent.fanout step.
type AgentFanoutSpec struct {
	ID       string
	Timeout  time.Duration
	Clock    ports.Clock
	IDs      AgentTaskIDs
	AgentFor func(person change.ActorID) (id.AgentName, bool)
	Bundle   BundleBuilder
}

// AgentFanout is the step agent.fanout.
type AgentFanout struct {
	spec AgentFanoutSpec
}

var _ Step = AgentFanout{}

// NewAgentFanout creates a new agent.fanout step.
func NewAgentFanout(spec AgentFanoutSpec) (AgentFanout, error) {
	if spec.ID == "" {
		return AgentFanout{}, errors.New("agent.fanout: step id is required")
	}
	if spec.Timeout <= 0 {
		return AgentFanout{}, errors.New("agent.fanout: timeout must be positive")
	}
	if spec.Clock == nil {
		return AgentFanout{}, errors.New("agent.fanout: clock is required")
	}
	if spec.IDs == nil {
		return AgentFanout{}, errors.New("agent.fanout: ids is required")
	}
	if spec.Bundle == nil {
		return AgentFanout{}, errors.New("agent.fanout: bundle builder is required")
	}
	return AgentFanout{spec: spec}, nil
}

// Type returns the do name of the step.
func (AgentFanout) Type() string { return "agent.fanout" }

func (s AgentFanout) agentFor(person change.ActorID) (id.AgentName, bool) {
	if s.spec.AgentFor != nil {
		return s.spec.AgentFor(person)
	}
	a, err := id.ParseAgentName(string(person))
	if err != nil {
		return "", false
	}
	return a, true
}

func (s AgentFanout) latestPicked(c ChangeView) (*change.Event, *change.ReviewersPicked, bool, error) {
	for i := len(c.Events) - 1; i >= 0; i-- {
		ev := c.Events[i]
		if ev.Type == change.EventReviewersPicked {
			p, err := change.Decode(ev)
			if err != nil {
				return nil, nil, false, fmt.Errorf("read reviewers.picked: %w", err)
			}
			rp, ok := p.(change.ReviewersPicked)
			if !ok {
				return nil, nil, false, fmt.Errorf("payload of %s is not change.ReviewersPicked", ev.Type)
			}
			return &ev, &rp, true, nil
		}
	}
	return nil, nil, false, nil
}

// Done reports whether every picked reviewer has review.submitted, review.unavailable or review.waived.
func (s AgentFanout) Done(c ChangeView) bool {
	_, rp, ok, err := s.latestPicked(c)
	if err != nil || !ok || len(rp.People) == 0 {
		return false
	}

	terminal := terminalReviewers(c)
	for _, person := range rp.People {
		agent, ok := s.agentFor(person)
		if !ok || !terminal[agent] {
			return false
		}
	}
	return true
}

// Start assigns one reviewer task and effect per unassigned picked reviewer.
func (s AgentFanout) Start(_ context.Context, c ChangeView, _ json.RawMessage) (Result, error) {
	if s.Done(c) {
		return Result{}, nil
	}
	pickedEv, rp, ok, err := s.latestPicked(c)
	if err != nil {
		return Result{}, fmt.Errorf("agent.fanout %s: %w", s.spec.ID, err)
	}
	if !ok {
		return Result{}, nil
	}

	unassigned, err := s.findUnassigned(c, rp.People)
	if err != nil {
		return Result{}, err
	}
	if len(unassigned) == 0 {
		// Everyone has a task: keep reporting the review deadline so the timer loop fires it after a restart.
		if due, ok := latestTaskDeadline(ChangeView{ID: c.ID, Events: c.Events[reviewCycleStart(c.Events):]}, workflow.RoleReviewer); ok {
			return Result{Timers: []Timer{{Step: s.spec.ID, Due: due}}}, nil
		}
		return Result{}, nil
	}

	bundleBytes, bundleRef, err := s.spec.Bundle(workflow.RoleReviewer, c.ID, c.Events)
	if err != nil {
		return Result{}, fmt.Errorf("agent.fanout %s: build bundle: %w", s.spec.ID, err)
	}

	deadline := s.spec.Clock.Now().Add(s.spec.Timeout)
	commitVersion := int64(len(c.history())) + int64(len(unassigned))

	var evs []change.Event
	var fxs []change.Effect
	var timers []Timer

	for _, agent := range unassigned {
		ev, fx, err := s.assignReviewer(c, agent, *pickedEv, bundleBytes, bundleRef, deadline, commitVersion)
		if err != nil {
			return Result{}, err
		}
		evs = append(evs, ev)
		fxs = append(fxs, fx)
		timers = append(timers, Timer{Step: s.spec.ID, Due: deadline})
	}
	return Result{Events: evs, Effects: fxs, Timers: timers}, nil
}

func (s AgentFanout) findUnassigned(c ChangeView, people []change.ActorID) ([]id.AgentName, error) {
	assigned := assignedReviewers(c)
	var unassigned []id.AgentName
	for _, person := range people {
		agent, ok := s.agentFor(person)
		if !ok {
			return nil, fmt.Errorf("agent.fanout %s: unknown agent for %s", s.spec.ID, person)
		}
		if !assigned[agent] {
			unassigned = append(unassigned, agent)
		}
	}
	return unassigned, nil
}

func (s AgentFanout) assignReviewer(c ChangeView, agent id.AgentName, pickedEv change.Event, bundleBytes []byte, bundleRef workflow.BlobRef, deadline time.Time, commitVersion int64) (change.Event, change.Effect, error) {
	taskID := s.spec.IDs.NewTask()
	ta := change.TaskAssigned{
		TaskID:    taskID,
		Agent:     agent,
		Role:      workflow.RoleReviewer,
		Deadline:  deadline,
		BundleRef: bundleRef,
	}
	if err := ta.Validate(); err != nil {
		return change.Event{}, change.Effect{}, fmt.Errorf("agent.fanout %s: %w", s.spec.ID, err)
	}
	rawTa, err := json.Marshal(ta)
	if err != nil {
		return change.Event{}, change.Effect{}, fmt.Errorf("agent.fanout %s: encode task.assigned: %w", s.spec.ID, err)
	}
	causation := pickedEv.ID
	ev := change.Event{
		ID:            s.spec.IDs.NewEvent(),
		ChangeID:      c.ID,
		Type:          change.EventTaskAssigned,
		Actor:         systemActor(),
		OccurredAt:    s.spec.Clock.Now(),
		CausationID:   &causation,
		CorrelationID: pickedEv.CorrelationID,
		BlobRefs:      []workflow.BlobRef{bundleRef},
		Payload:       rawTa,
	}

	payload := change.TaskAssignPayload{
		Agent:  agent,
		Role:   workflow.RoleReviewer,
		TaskID: taskID,
		RoomID: roomIDOf(c),
		Bundle: bundleBytes,
	}
	if err := payload.Validate(); err != nil {
		return change.Event{}, change.Effect{}, fmt.Errorf("agent.fanout %s: %w", s.spec.ID, err)
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return change.Event{}, change.Effect{}, fmt.Errorf("agent.fanout %s: encode task_assign payload: %w", s.spec.ID, err)
	}
	key, err := change.IdempotencyKey(c.ID, commitVersion, change.EffectTaskAssign, string(taskID))
	if err != nil {
		return change.Event{}, change.Effect{}, fmt.Errorf("agent.fanout %s: %w", s.spec.ID, err)
	}
	fx := change.Effect{
		Key:     key,
		Kind:    change.EffectTaskAssign,
		Target:  string(bundleRef),
		Payload: rawPayload,
	}
	return ev, fx, nil
}

// OnEvent does nothing on review events.
func (AgentFanout) OnEvent(_ context.Context, _ ChangeView, _ change.Event) (Result, error) {
	return Result{}, nil
}

// OnTimeout marks every reviewer that has not submitted by the deadline as review.unavailable, acked or not. An acked
// reviewer can hang (KAN-19: reviewer-1 waited on a question nobody would answer), and it must not hold the review open
// for ever.
func (s AgentFanout) OnTimeout(_ context.Context, c ChangeView) (Result, error) {
	_, rp, ok, err := s.latestPicked(c)
	if err != nil || !ok {
		return Result{}, err
	}

	acked := ackedAgents(c)
	terminal := terminalReviewers(c)
	taskEventForAgent := taskEventsForReviewers(c)

	var timeoutEvs []change.Event
	for _, person := range rp.People {
		agent, ok := s.agentFor(person)
		if !ok || terminal[agent] {
			continue
		}
		assignedEv, hasTask := taskEventForAgent[agent]
		if !hasTask {
			continue
		}
		ev, err := s.buildUnavailableEvent(c, agent, assignedEv, acked[agent])
		if err != nil {
			return Result{}, err
		}
		timeoutEvs = append(timeoutEvs, ev)
	}
	return Result{Events: timeoutEvs}, nil
}

func (s AgentFanout) buildUnavailableEvent(c ChangeView, agent id.AgentName, assignedEv change.Event, acked bool) (change.Event, error) {
	reason := "task timed out without ack"
	if acked {
		reason = "task timed out without a review"
	}
	p := change.ReviewUnavailable{
		Reviewer: agent,
		Reason:   reason,
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return change.Event{}, fmt.Errorf("agent.fanout timeout %s: encode payload: %w", s.spec.ID, err)
	}
	causation := assignedEv.ID
	return change.Event{
		ID:            s.spec.IDs.NewEvent(),
		ChangeID:      c.ID,
		Type:          change.EventReviewUnavailable,
		Actor:         systemActor(),
		OccurredAt:    s.spec.Clock.Now(),
		CausationID:   &causation,
		CorrelationID: assignedEv.CorrelationID,
		Payload:       raw,
	}, nil
}

func terminalReviewers(c ChangeView) map[id.AgentName]bool {
	terminal := make(map[id.AgentName]bool)
	for _, ev := range c.Events[reviewCycleStart(c.Events):] {
		if ev.Type == change.EventReviewSubmitted || ev.Type == change.EventReviewUnavailable || ev.Type == change.EventReviewWaived {
			recordTerminalReview(terminal, ev)
		}
	}
	return terminal
}

// reviewCycleStart returns the first event at the current PR head. Review submissions, acknowledgements and task
// assignments before that event belong to an older diff and cannot complete or suppress current review work.
func reviewCycleStart(events []change.Event) int {
	start := 0
	for i, ev := range events {
		if ev.Type == change.EventPROpened || ev.Type == change.EventPRPushed {
			start = i
		}
	}
	return start
}

func recordTerminalReview(m map[id.AgentName]bool, ev change.Event) {
	p, err := change.Decode(ev)
	if err != nil {
		return
	}
	switch v := p.(type) {
	case change.ReviewSubmitted:
		m[v.Reviewer] = true
	case change.ReviewUnavailable:
		m[v.Reviewer] = true
	case change.ReviewWaived:
		m[v.Reviewer] = true
	default:
	}
}

func assignedReviewers(c ChangeView) map[id.AgentName]bool {
	assigned := make(map[id.AgentName]bool)
	for _, ev := range c.Events[reviewCycleStart(c.Events):] {
		if ev.Type == change.EventTaskAssigned {
			if p, err := change.Decode(ev); err == nil {
				if ta, ok := p.(change.TaskAssigned); ok && ta.Role == workflow.RoleReviewer {
					assigned[ta.Agent] = true
				}
			}
		}
	}
	return assigned
}

func ackedAgents(c ChangeView) map[id.AgentName]bool {
	acked := make(map[id.AgentName]bool)
	for _, ev := range c.Events[reviewCycleStart(c.Events):] {
		if ev.Type == change.EventTaskAcked {
			if p, err := change.Decode(ev); err == nil {
				if ta, ok := p.(change.TaskAcked); ok {
					acked[ta.Agent] = true
				}
			}
		}
	}
	return acked
}

func taskEventsForReviewers(c ChangeView) map[id.AgentName]change.Event {
	taskEventForAgent := make(map[id.AgentName]change.Event)
	for _, ev := range c.Events[reviewCycleStart(c.Events):] {
		if ev.Type == change.EventTaskAssigned {
			if p, err := change.Decode(ev); err == nil {
				if ta, ok := p.(change.TaskAssigned); ok && ta.Role == workflow.RoleReviewer {
					taskEventForAgent[ta.Agent] = ev
				}
			}
		}
	}
	return taskEventForAgent
}
