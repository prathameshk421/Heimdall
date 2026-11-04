package engine

import (
	"fmt"
	"slices"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// taskBudget is the one until value the spec names (TECH_SPEC 5.1): a retry runs while the task has budget.
const taskBudget = "task_budget"

// maxRetryReasons bounds the reasons a retry carries. It is the limit of step.retried (change.StepRetried).
const maxRetryReasons = 8

// checkRetries reports the retries of a workflow that the config cannot run. A retry needs an attempt budget
// (MaxAttempts), or it would never stop, and the only until value the spec gives is task_budget.
func checkRetries(w workflow.Workflow, maxAttempts int) []error {
	var problems []error
	for _, s := range w.Steps {
		if s.OnFail == nil || s.OnFail.Retry == "" {
			continue
		}
		if maxAttempts < 1 {
			problems = append(problems, fmt.Errorf("%w: step %q retries, so MaxAttempts must be 1 or more, got %d",
				workflow.ErrPolicyInvalid, s.ID, maxAttempts))
		}
		if s.OnFail.Until != taskBudget {
			problems = append(problems, fmt.Errorf("%w: step %q: until %q is not %s",
				workflow.ErrPolicyInvalid, s.ID, s.OnFail.Until, taskBudget))
		}
	}
	return problems
}

// checkFailed applies a failed check. The step's own record is stored, then the step's on_fail decides what follows.
// A check on a step with no on_fail escalates, so no failure stops silently.
func (e *Engine) checkFailed(v ChangeView, ws workflow.Step, rec Result, sf StepFailedError) (attempt, error) {
	a := attempt{failed: true, res: Result{Events: rec.Events, Effects: rec.Effects}}
	ev, err := e.onFail(v, ws, sf)
	if err != nil {
		return attempt{}, err
	}
	if ev != nil {
		a.res.Events = append(slices.Clone(rec.Events), *ev)
	}
	return a, nil
}

// onFail returns the event that follows a failed check, or nil when the step's on_fail stops with the record. While
// the retry target has attempts left, the target is reopened with the reasons. When they are used up, the step
// escalates, unless on_fail says reject.
func (e *Engine) onFail(v ChangeView, ws workflow.Step, sf StepFailedError) (*change.Event, error) {
	if ws.OnFail == nil {
		return e.escalation(v, ws.ID, 1, sf)
	}
	attempts := 1
	if ws.OnFail.Retry != "" {
		n, err := attemptsOf(v, ws.OnFail.Retry)
		if err != nil {
			return nil, err
		}
		attempts = n
		if attempts < e.maxAttempts {
			p := change.StepRetried{Step: ws.OnFail.Retry, Attempt: attempts + 1, Reasons: retryReasons(sf.Reasons)}
			return e.eventAfter(v, change.EventStepRetried, p)
		}
	}
	if ws.OnFail.Then == "reject" {
		return nil, nil
	}
	return e.escalation(v, ws.ID, attempts, sf)
}

// escalation builds step.escalated for the failing step.
func (e *Engine) escalation(v ChangeView, stepID string, attempts int, sf StepFailedError) (*change.Event, error) {
	p := change.StepEscalated{Step: stepID, Attempts: attempts, Reason: firstReason(sf.Reasons)}
	return e.eventAfter(v, change.EventStepEscalated, p)
}

// eventAfter builds an engine event caused by the newest stored event of the view.
func (e *Engine) eventAfter(v ChangeView, typ change.EventType, p change.Payload) (*change.Event, error) {
	if len(v.Events) == 0 {
		return nil, fmt.Errorf("change %s: nothing stored to cause %s", v.ID, typ)
	}
	return e.newEvent(v, v.Events[len(v.Events)-1], typ, p)
}

// attemptsOf returns the attempts a retry target has made: its first run, and one more for each retry of it so far.
func attemptsOf(v ChangeView, stepID string) (int, error) {
	retries, _, err := recoveryMarks(v.Events)
	if err != nil {
		return 0, err
	}
	n := 1
	for _, r := range retries {
		if r.step == stepID {
			n++
		}
	}
	return n, nil
}

// retryReasons returns the reasons a retry carries: at most maxRetryReasons, each trimmed and cut to the text limit.
// Blank reasons are dropped.
func retryReasons(in []string) []string {
	out := make([]string, 0, len(in))
	for _, r := range in {
		if len(out) == maxRetryReasons {
			break
		}
		if s := boundText(r); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// firstReason is the first usable reason of a failed check, or a fixed text when there is none.
func firstReason(in []string) string {
	for _, r := range in {
		if s := boundText(r); s != "" {
			return s
		}
	}
	return "check failed without a reason"
}

// boundText trims s and cuts it to the text limit of a payload (maxReasonLen).
func boundText(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > maxReasonLen {
		s = s[:maxReasonLen]
	}
	return s
}
