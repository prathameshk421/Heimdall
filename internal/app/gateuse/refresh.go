package gateuse

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// The PR syncer evaluates the gate only after new pull request events. A review, a verified finding, a verdict, a
// human decision or an approval also changes the gate's answer, and before the refresher none of them asked again: on
// KAN-23 the gate kept "the review requests are not known" after all three reviews were in. The refresher follows the
// global event log and evaluates the gate of each change that gets such an event. Evaluate skips a result equal to the
// last one, so asking again is safe.

// RefreshCursor stores how far the refresher has read the global event log.
type RefreshCursor interface {
	LoadRefreshCursor(context.Context) (int64, error)
	SaveRefreshCursor(context.Context, int64) error
}

// GateEvaluate is the part of Evaluator the refresher calls.
type GateEvaluate interface {
	Evaluate(context.Context, id.ChangeID) (Result, error)
}

// Refresher re-evaluates the gate after events that can change its answer.
type Refresher struct {
	history ports.WorkflowHistory
	cursor  RefreshCursor
	gate    GateEvaluate
	batch   int
}

// refreshBatch is how many events one read takes from the log.
const refreshBatch = 200

// NewRefresher requires the event log, a cursor and the evaluator.
func NewRefresher(history ports.WorkflowHistory, cursor RefreshCursor, gate GateEvaluate) (*Refresher, error) {
	if history == nil || cursor == nil || gate == nil {
		return nil, errors.New("gate refresher: history, cursor and evaluator are required")
	}
	return &Refresher{history: history, cursor: cursor, gate: gate, batch: refreshBatch}, nil
}

// RunOnce reads every event after the cursor and evaluates the gate of each change that needs it. It moves the cursor
// past every page it read, even when one change fails, so one bad change never holds back the others; the error joins
// every change that failed. A change with no pull request, or a closed one, has no gate and is skipped.
func (r *Refresher) RunOnce(ctx context.Context) (int, error) {
	after, err := r.cursor.LoadRefreshCursor(ctx)
	if err != nil {
		return 0, fmt.Errorf("gate refresher: load cursor: %w", err)
	}
	evaluated := 0
	var failed []error
	for {
		events, next, err := r.history.EventsAfter(ctx, after, r.batch)
		if err != nil {
			return evaluated, errors.Join(append(failed, fmt.Errorf("gate refresher: read events: %w", err))...)
		}
		for _, changeID := range ChangesToRecheck(events) {
			if _, err := r.gate.Evaluate(ctx, changeID); err != nil {
				if !errors.Is(err, ErrNoPullRequest) && !errors.Is(err, ErrPullRequestClosed) {
					failed = append(failed, fmt.Errorf("change %s: %w", changeID, err))
				}
				continue
			}
			evaluated++
		}
		if len(events) == 0 || next <= after {
			return evaluated, errors.Join(failed...)
		}
		if err := r.cursor.SaveRefreshCursor(ctx, next); err != nil {
			return evaluated, errors.Join(append(failed, fmt.Errorf("gate refresher: save cursor: %w", err))...)
		}
		after = next
		if len(events) < r.batch {
			return evaluated, errors.Join(failed...)
		}
	}
}

// ChangesToRecheck lists, in first-seen order and once each, the changes whose events can change the gate's answer.
// The gate's own event never counts, so the refresher cannot feed itself. Pull request events are left to the PR
// syncer, which already evaluates after them.
func ChangesToRecheck(events []change.Event) []id.ChangeID {
	var out []id.ChangeID
	seen := map[id.ChangeID]bool{}
	for _, event := range events {
		if !movesGate(event.Type) || seen[event.ChangeID] {
			continue
		}
		seen[event.ChangeID] = true
		out = append(out, event.ChangeID)
	}
	return out
}

func movesGate(t change.EventType) bool {
	return slices.Contains([]change.EventType{
		change.EventReviewersPicked, change.EventReviewSubmitted, change.EventReviewUnavailable, change.EventReviewWaived,
		change.EventFindingFiled, change.EventFindingVerified, change.EventFindingStateChanged,
		change.EventChallengeFiled, change.EventCaseOpened, change.EventVerdictApplied, change.EventVerdictEscalated,
		change.EventDecisionRecorded, change.EventApprovalRecorded, change.EventApprovalInvalidated,
		change.EventTestsApproved, change.EventCIReported,
	}, t)
}
