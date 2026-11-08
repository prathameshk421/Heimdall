package gateuse_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/gateuse"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

func ev(changeID id.ChangeID, typ change.EventType) change.Event {
	return change.Event{ChangeID: changeID, Type: typ}
}

func TestChangesToRecheckTakesEventsThatMoveTheGateOnceEach(t *testing.T) {
	got := gateuse.ChangesToRecheck([]change.Event{
		ev("KAN-1", change.EventReviewSubmitted),
		ev("KAN-2", change.EventTaskProgress),
		ev("KAN-1", change.EventFindingVerified),
		ev("KAN-3", change.EventDecisionRecorded),
		ev("KAN-4", change.EventGateEvaluated),
		ev("KAN-5", change.EventPRPushed),
	})
	require.Equal(t, []id.ChangeID{"KAN-1", "KAN-3"}, got,
		"progress notes do not move the gate, the gate's own event must not feed it, and pull request events are the PR syncer's")
}

type pagedHistory struct{ pages [][]change.Event }

func (h *pagedHistory) EventsAfter(_ context.Context, after int64, _ int) ([]change.Event, int64, error) {
	if int(after) >= len(h.pages) {
		return nil, after, nil
	}
	return h.pages[after], after + 1, nil
}

type memCursor struct{ at int64 }

func (c *memCursor) LoadRefreshCursor(context.Context) (int64, error) { return c.at, nil }
func (c *memCursor) SaveRefreshCursor(_ context.Context, at int64) error {
	c.at = at
	return nil
}

type recordingGate struct {
	calls []id.ChangeID
	fail  map[id.ChangeID]error
}

func (g *recordingGate) Evaluate(_ context.Context, changeID id.ChangeID) (gateuse.Result, error) {
	g.calls = append(g.calls, changeID)
	return gateuse.Result{}, g.fail[changeID]
}

func TestRefresherEvaluatesEachChangeMovesThePastFailuresAndSkipsChangesWithoutAGate(t *testing.T) {
	history := &pagedHistory{pages: [][]change.Event{{
		ev("KAN-1", change.EventReviewSubmitted),
		ev("KAN-2", change.EventFindingFiled),
		ev("KAN-3", change.EventApprovalRecorded),
		ev("KAN-4", change.EventDecisionRecorded),
	}}}
	gate := &recordingGate{fail: map[id.ChangeID]error{
		"KAN-2": errors.New("github is down"),
		"KAN-3": gateuse.ErrPullRequestClosed,
		"KAN-4": gateuse.ErrNoPullRequest,
	}}
	cursor := &memCursor{}
	r, err := gateuse.NewRefresher(history, cursor, gate)
	require.NoError(t, err)

	evaluated, err := r.RunOnce(context.Background())
	require.Equal(t, 1, evaluated)
	require.ErrorContains(t, err, "KAN-2", "a real failure is reported")
	require.NotContains(t, err.Error(), "KAN-3", "a closed pull request has no gate")
	require.NotContains(t, err.Error(), "KAN-4", "a change without a pull request has no gate")
	require.Equal(t, []id.ChangeID{"KAN-1", "KAN-2", "KAN-3", "KAN-4"}, gate.calls)
	require.Equal(t, int64(1), cursor.at, "one bad change never holds the cursor back")

	gate.calls = nil
	_, err = r.RunOnce(context.Background())
	require.NoError(t, err)
	require.Empty(t, gate.calls, "events already read are not read again")
}
