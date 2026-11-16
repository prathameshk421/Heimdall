package change

import (
	"errors"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// ErrRefused is the refusal of a request on the current view (AGENT_INTERACTION_ARCH F1 rule 2): what someone asks for
// is checked before it becomes an event. A fact from outside (GitHub, Jira, the sandbox) is never refused. Branch with
// errors.Is.
var ErrRefused = errors.New("refused")

func refused(why string) error { return fmt.Errorf("%w: %s", ErrRefused, why) }

// CanAppend says whether a request may add e to the change as v shows it. It is the one check the agent tools and the
// command paths make before they append; events nobody asks for (facts) pass.
func CanAppend(v View, e Event) error {
	if v.Over() {
		return refused("the change has ended")
	}
	check, ok := requestChecks()[e.Type]
	if !ok {
		return nil
	}
	return check(v, e)
}

// requestChecks are the predicates of the event types a request may produce. Every other type is a fact.
func requestChecks() map[EventType]func(View, Event) error {
	review := func(v View, _ Event) error { return CanReview(v) }
	task := func(v View, e Event) error {
		p, err := Decode(e)
		if err != nil {
			return err
		}
		return CanReportOnTask(v, taskOf(p))
	}
	return map[EventType]func(View, Event) error{
		EventTestsSubmitted:  func(v View, _ Event) error { return CanSubmitTests(v) },
		EventTestsApproved:   func(v View, _ Event) error { return CanApproveTests(v) },
		EventReviewSubmitted: review, EventFindingFiled: review, EventChallengeFiled: review,
		EventTaskAcked: task, EventTaskProgress: task, EventTaskFailed: task, EventTaskTimedOut: task,
	}
}

// CanSubmitTests reports whether tests may be submitted: they may be submitted when none are, or when the last submission was rejected.
func CanSubmitTests(v View) error {
	switch {
	case v.Over():
		return refused("the change has ended")
	case v.Tests == nil, len(v.Tests.Rejected) > 0:
		return nil
	case v.Tests.Committed != nil:
		return refused("the tests are already committed")
	default:
		return refused("tests were submitted and are not rejected")
	}
}

// CanApproveTests reports whether a person may sign off tests that are verified, need a sign-off and have none yet.
func CanApproveTests(v View) error {
	switch {
	case v.Over():
		return refused("the change has ended")
	case v.Tests == nil || v.Tests.Verified == nil:
		return refused("there are no verified tests")
	case !v.Tests.Verified.NeedsSignoff:
		return refused("these tests need no sign-off")
	case v.Tests.ApprovedBy != "":
		return refused("the tests are already approved")
	}
	return nil
}

// CanReview reports whether a review may be filed: reviews and findings are about an open pull request.
func CanReview(v View) error {
	switch {
	case v.Over():
		return refused("the change has ended")
	case v.PR == nil:
		return refused("the change has no pull request")
	}
	return nil
}

// CanReportOnTask reports whether an agent may report on task: it reports only on a task that was assigned.
func CanReportOnTask(v View, task id.TaskID) error {
	if _, ok := v.Tasks[task]; !ok {
		return refused("no such task is assigned")
	}
	return nil
}

func taskOf(p Payload) id.TaskID {
	switch p := p.(type) {
	case TaskAcked:
		return p.TaskID
	case TaskProgress:
		return p.TaskID
	case TaskFailed:
		return p.TaskID
	case TaskTimedOut:
		return p.TaskID
	}
	return ""
}
