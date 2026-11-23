package change_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

func TestTrackerCommitRowEmitsTrackerWriteAfterTestsCommitted(t *testing.T) {
	t.Parallel()
	base := []change.Event{
		triggered(t),
		ev(t, 2, change.EventRoomOpened, change.RoomOpened{RoomID: "switch:r1"}),
		ev(t, 3, change.EventBranchEnsured, change.BranchEnsured{Repo: "acme/pay", Branch: "KAN-5-x", BaseSHA: fBase}),
		submission(t, 4, blobA),
		ev(t, 5, change.EventTestsVerified, change.TestsVerified{RunIDs: []id.RunID{"run_01HZX3K9QW7M2P5R8T0V4YB6CD"}, SHA: fHead, TreeHash: fBase}),
		ev(t, 6, change.EventTestsCommitted, change.TestsCommitted{SHA: fHead, TreeHash: fBase}),
	}
	got := actionsOf(inOf("KAN-5", fold(t, base...), t0))
	require.Len(t, got["tracker_commit"], 1)
	job := got["tracker_commit"][0].Job
	require.NotNil(t, job)
	require.Equal(t, change.EffectTrackerWrite, job.Kind)
	require.Equal(t, "KAN-5", job.Target)
	p := job.Payload.(change.TrackerWritePayload)
	require.Equal(t, change.TrackerStageCommit, p.Stage)
	require.Contains(t, p.Comment, "KAN-5-x")
	require.Empty(t, p.Transition)
	require.NoError(t, p.Validate())

	done := append(append([]change.Event{}, base...),
		ev(t, 7, change.EventTrackerUpdated, change.TrackerUpdated{Ticket: "KAN-5", Stage: change.TrackerStageCommit}))
	require.NotContains(t, actionsOf(inOf("KAN-5", fold(t, done...), t0)), "tracker_commit")
}

func TestMergedPREmitsTrackerWriteWithPolicyTransitionAndRoomClose(t *testing.T) {
	t.Parallel()
	base := []change.Event{
		triggered(t),
		ev(t, 2, change.EventRoomOpened, change.RoomOpened{RoomID: "switch:r1"}),
		opened(t),
		ev(t, 4, change.EventGateEvaluated, change.GateEvaluated{OK: true, SHA: fHead, TreeHash: fBase, PolicyVersion: 2}),
		ev(t, 5, change.EventApprovalRecorded, change.ApprovalRecorded{Login: "bob", CommitSHA: fHead}),
		ev(t, 6, change.EventPRClosed, change.PRClosed{Repo: "acme/pay", Number: 7, HeadSHA: fHead, Merged: true}),
	}
	in := inOf("KAN-5", fold(t, base...), t0)
	in.Policy.Tracker.MergeStatus = "Done"
	got := actionsOf(in)

	// No normal flow rows open on a merged PR.
	require.NotContains(t, got, "gate")
	require.NotContains(t, got, "review")
	require.NotContains(t, got, "branch")

	// F1: tracker_merge emits tracker_write with transition from policy.
	require.Len(t, got["tracker_merge"], 1)
	tj := got["tracker_merge"][0].Job
	require.NotNil(t, tj)
	require.Equal(t, change.EffectTrackerWrite, tj.Kind)
	tp := tj.Payload.(change.TrackerWritePayload)
	require.Equal(t, change.TrackerStageMerge, tp.Stage)
	require.Equal(t, "Done", tp.Transition)
	require.Contains(t, tp.Comment, "acme/pay#7")
	require.Contains(t, tp.Comment, "Approved by bob")
	require.NoError(t, tp.Validate())

	// F2: close emits one room_post summary and one room_close job.
	require.Len(t, got["close"], 2)
	postJob := got["close"][0].Job
	closeJob := got["close"][1].Job
	require.Equal(t, change.EffectRoomPost, postJob.Kind)
	require.Equal(t, "switch:r1", postJob.Target)
	pp := postJob.Payload.(change.RoomPostPayload)
	require.Equal(t, change.RoomID("switch:r1"), pp.RoomID)
	require.Contains(t, pp.Text, "KAN-5")
	require.Contains(t, pp.Text, "merged")
	require.NoError(t, pp.Validate())

	require.Equal(t, change.EffectRoomClose, closeJob.Kind)
	require.Equal(t, "switch:r1", closeJob.Target)
	cp := closeJob.Payload.(change.RoomClosePayload)
	require.Equal(t, change.RoomID("switch:r1"), cp.RoomID)
	require.NoError(t, cp.Validate())

	// Once tracker.updated(merge) and room.closed arrive, no rows remain open.
	finished := append(append([]change.Event{}, base...),
		ev(t, 7, change.EventTrackerUpdated, change.TrackerUpdated{Ticket: "KAN-5", Stage: change.TrackerStageMerge, Transition: "Done"}),
		ev(t, 8, change.EventRoomClosed, change.RoomClosed{RoomID: "switch:r1"}))
	inDone := inOf("KAN-5", fold(t, finished...), t0)
	inDone.Policy.Tracker.MergeStatus = "Done"
	require.Empty(t, change.Next(inDone))
}

func TestClosedUnmergedPRHasNoMergeRows(t *testing.T) {
	t.Parallel()
	v := fold(t, triggered(t),
		ev(t, 2, change.EventRoomOpened, change.RoomOpened{RoomID: "switch:r1"}),
		opened(t),
		ev(t, 4, change.EventPRClosed, change.PRClosed{Repo: "acme/pay", Number: 7, HeadSHA: fHead, Merged: false}))
	require.Empty(t, change.Next(inOf("KAN-5", v, t0)))
}
