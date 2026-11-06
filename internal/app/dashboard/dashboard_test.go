package dashboard_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/dashboard"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/testkit/changelog"
)

const (
	pay7      = id.ChangeID("PAY-7")
	pay8      = id.ChangeID("PAY-8")
	findingID = "fnd_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	sha       = id.SHA("3f2a9c1d0b7e4a5f6c8d9e0a1b2c3d4e5f607182")
)

// triggered is the first event of every change in these tests.
func triggered(ticket id.ChangeID) change.Payload {
	return change.Triggered{Ticket: ticket, Repo: "owner/name", Assignee: "alice", Source: change.TriggerJira}
}

// reviewedLog is a change with two reviewers, one finding, one open question and a blocked gate.
func reviewedLog(t testing.TB) []change.Event {
	return changelog.New(t, pay7).
		Add(change.EventChangeTriggered, changelog.Human("alice"), triggered(pay7)).
		Add(change.EventRoomOpened, changelog.Agent("heimdall"), change.RoomOpened{RoomID: "room-7"}).
		Add(change.EventReviewersPicked, changelog.Human("alice"), change.ReviewersPicked{
			People: []change.ActorID{"reviewer-b", "reviewer-a"}, By: "alice", Method: change.PickDashboard,
		}).
		Add(change.EventReviewSubmitted, changelog.Agent("reviewer-a"), change.ReviewSubmitted{
			Reviewer: "reviewer-a", FindingRefs: []id.FindingRef{1}, PackDate: "2026-11-07",
		}).
		Add(change.EventReviewUnavailable, changelog.Agent("heimdall"), change.ReviewUnavailable{
			Reviewer: "reviewer-b", Reason: "no answer in time",
		}).
		Add(change.EventFindingFiled, changelog.Agent("reviewer-a"), change.FindingFiled{
			FindingID: findingID, Ref: 1, Tier: finding.TierE1, State: finding.StateOpen, By: "reviewer-a",
		}).
		Add(change.EventQuestionAsked, changelog.Agent("heimdall"), change.QuestionAsked{
			QuestionID: "q-1", Refs: []string{"PAY-7 spec"}, Sender: "sender-1", MessageID: "msg-1",
		}).
		Add(change.EventGateEvaluated, changelog.Agent("heimdall"), change.GateEvaluated{
			OK: false, Reasons: []string{"F1 is open on the head commit"}, SHA: sha, TreeHash: sha, PolicyVersion: 1,
		}).
		Events()
}

func history(t testing.TB, logs map[id.ChangeID][]change.Event) *changelog.History {
	t.Helper()
	h := changelog.NewHistory()
	for cid, evs := range logs {
		h.Put(cid, evs)
	}
	return h
}

func service(t testing.TB, h dashboard.History) *dashboard.Service {
	t.Helper()
	svc, err := dashboard.New(h)
	require.NoError(t, err)
	return svc
}

func TestNewRefusesNilHistory(t *testing.T) {
	_, err := dashboard.New(nil)
	require.Error(t, err)
}

func TestChangesListsEveryChangeWithItsGateAndOpenFindings(t *testing.T) {
	h := history(t, map[id.ChangeID][]change.Event{
		pay7: reviewedLog(t),
		pay8: changelog.New(t, pay8).Add(change.EventChangeTriggered, changelog.Human("bob"), triggered(pay8)).Events(),
	})

	rows, err := service(t, h).Changes(t.Context())
	require.NoError(t, err)
	require.Len(t, rows, 2)

	require.Equal(t, pay7, rows[0].ID)
	require.Equal(t, "PAY-7", rows[0].Ticket)
	require.Equal(t, "blocked", rows[0].Gate)
	require.Equal(t, 1, rows[0].OpenFindings)
	require.Equal(t, int64(8), rows[0].Version)

	require.Equal(t, pay8, rows[1].ID)
	require.Equal(t, "not evaluated", rows[1].Gate)
	require.Equal(t, 0, rows[1].OpenFindings)
}

func TestEmptyLogHasNoChanges(t *testing.T) {
	rows, err := service(t, changelog.NewHistory()).Changes(t.Context())
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestDetailNamesGateFindingsReviewersAndOpenAsks(t *testing.T) {
	svc := service(t, history(t, map[id.ChangeID][]change.Event{pay7: reviewedLog(t)}))

	d, found, err := svc.Detail(t.Context(), pay7)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "PAY-7", d.Ticket)
	require.Equal(t, "owner/name", d.Repo)
	require.Equal(t, int64(8), d.Version)

	require.Equal(t, "blocked", d.Gate.Status)
	require.Equal(t, []string{"F1 is open on the head commit"}, d.Gate.Reasons)
	require.Equal(t, string(sha), d.Gate.SHA)
	require.Contains(t, d.Blockers, "F1 is open on the head commit")

	require.Len(t, d.Findings, 1)
	require.Equal(t, "F1", d.Findings[0].Ref)
	require.Equal(t, "E1", d.Findings[0].Tier)
	require.Equal(t, "open", d.Findings[0].State)

	require.Equal(t, []dashboard.Reviewer{
		{Name: "reviewer-a", Status: "submitted"},
		{Name: "reviewer-b", Status: "unavailable"},
	}, d.Reviewers)

	require.Len(t, d.OpenAsks, 1)
	require.Equal(t, "q-1", d.OpenAsks[0].ID)

	require.Len(t, d.Timeline, 8)
	require.Equal(t, change.EventChangeTriggered, change.EventType(d.Timeline[0].Type))
	require.Equal(t, change.EventGateEvaluated, change.EventType(d.Timeline[7].Type))
}

func TestDetailTakesTheLatestFindingRecord(t *testing.T) {
	evs := changelog.New(t, pay7).
		Add(change.EventChangeTriggered, changelog.Human("alice"), triggered(pay7)).
		Add(change.EventFindingFiled, changelog.Agent("reviewer-a"), change.FindingFiled{
			FindingID: findingID, Ref: 1, Tier: finding.TierE2, State: finding.StateOpen, By: "reviewer-a",
		}).
		Add(change.EventFindingStateChanged, changelog.Agent("heimdall"), change.FindingStateChanged{
			FindingID: findingID, Ref: 1, Tier: finding.TierE2, State: finding.StateResolved, By: "reviewer-a",
		}).
		Events()
	svc := service(t, history(t, map[id.ChangeID][]change.Event{pay7: evs}))

	d, found, err := svc.Detail(t.Context(), pay7)
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, d.Findings, 1)
	require.Equal(t, "resolved", d.Findings[0].State)
	require.Equal(t, "not evaluated", d.Gate.Status)
}

func TestDetailFollowsTheReducerForTheState(t *testing.T) {
	evs := changelog.New(t, pay7).
		Add(change.EventChangeTriggered, changelog.Human("alice"), triggered(pay7)).
		Add(change.EventRoomOpened, changelog.Agent("heimdall"), change.RoomOpened{RoomID: "room-7"}).
		Events()
	svc := service(t, history(t, map[id.ChangeID][]change.Event{pay7: evs}))

	d, found, err := svc.Detail(t.Context(), pay7)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "ROOM_OPEN", d.State)
	require.Empty(t, d.Problem)
}

func TestDetailNamesALogThatDoesNotReplayAndKeepsTheTimeline(t *testing.T) {
	// tests.committed is not a move from TRIGGERED, so the reducer refuses it.
	evs := changelog.New(t, pay7).
		Add(change.EventChangeTriggered, changelog.Human("alice"), triggered(pay7)).
		Add(change.EventTestsCommitted, changelog.Agent("heimdall"), change.TestsCommitted{SHA: sha, TreeHash: sha}).
		Events()
	svc := service(t, history(t, map[id.ChangeID][]change.Event{pay7: evs}))

	d, found, err := svc.Detail(t.Context(), pay7)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "unknown", d.State)
	require.NotEmpty(t, d.Problem)
	require.Len(t, d.Timeline, 2)
}

func TestDetailShowsAnUnreadablePayloadAsUnreadable(t *testing.T) {
	evs := changelog.New(t, pay7).
		Add(change.EventChangeTriggered, changelog.Human("alice"), triggered(pay7)).
		Raw(change.EventFindingFiled, changelog.Agent("reviewer-a"), json.RawMessage(`{"bogus":true}`)).
		Events()
	svc := service(t, history(t, map[id.ChangeID][]change.Event{pay7: evs}))

	d, found, err := svc.Detail(t.Context(), pay7)
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, d.Timeline, 2)
	require.Equal(t, "payload not readable", d.Timeline[1].Note)
	require.Empty(t, d.Findings)
}

func TestUnknownChangeIsNotFound(t *testing.T) {
	svc := service(t, history(t, map[id.ChangeID][]change.Event{pay7: reviewedLog(t)}))

	_, found, err := svc.Detail(t.Context(), id.ChangeID("PAY-404"))
	require.NoError(t, err)
	require.False(t, found)
}
