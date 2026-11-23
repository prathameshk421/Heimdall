package prsync_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

func types(t *testing.T, r *rig, cid id.ChangeID) []change.EventType {
	t.Helper()
	evs, err := r.events.Load(context.Background(), cid)
	require.NoError(t, err)
	var out []change.EventType
	for _, e := range evs {
		out = append(out, e.Type)
	}
	return out
}

// Found live in H11: PR #16 was closed, then reopened on GitHub, and the sync appended nothing, so the change stayed
// over. A reopen is a fact: pr.reopened is appended once, and a later close is recorded again.
func TestAReopenedPullRequestIsRecordedOnceAndCanCloseAgain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := newRig(t)
	cid := id.ChangeID("PAY-16")
	r.intake.changeID[16] = cid
	r.seedInitialPROpened(cid, 16, head1)

	closed := samplePR(16, head1, ports.PRStateClosed)
	r.prs.prs[testRepo] = []ports.PR{closed}
	_, err := r.syncer.SyncOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, []change.EventType{change.EventPROpened, change.EventPRClosed}, types(t, r, cid))

	r.prs.prs[testRepo] = []ports.PR{samplePR(16, head1, ports.PRStateOpen)}
	res, err := r.syncer.SyncOnce(ctx)
	require.NoError(t, err)
	require.Empty(t, res.Failed)
	require.Equal(t, []change.EventType{change.EventPROpened, change.EventPRClosed, change.EventPRReopened}, types(t, r, cid))
	evs, err := r.events.Load(ctx, cid)
	require.NoError(t, err)
	var p change.PRReopened
	require.NoError(t, json.Unmarshal(evs[2].Payload, &p))
	require.Equal(t, change.PRReopened{Repo: testRepo, Number: 16, HeadSHA: head1}, p)

	_, err = r.restart(t).SyncOnce(ctx)
	require.NoError(t, err)
	require.Len(t, types(t, r, cid), 3, "a second sync, even after a restart, appends no second reopen")

	r.prs.prs[testRepo] = []ports.PR{closed}
	_, err = r.syncer.SyncOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, change.EventPRClosed, types(t, r, cid)[3], "a close after the reopen is recorded")
}

func TestAMergedPullRequestIsNeverReopened(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := newRig(t)
	cid := id.ChangeID("PAY-17")
	r.intake.changeID[17] = cid
	r.seedInitialPROpened(cid, 17, head1)
	merged := samplePR(17, head1, ports.PRStateClosed)
	merged.Merged = true
	r.prs.prs[testRepo] = []ports.PR{merged}
	_, err := r.syncer.SyncOnce(ctx)
	require.NoError(t, err)

	r.prs.prs[testRepo] = []ports.PR{samplePR(17, head1, ports.PRStateOpen)}
	_, err = r.syncer.SyncOnce(ctx)
	require.NoError(t, err)
	require.NotContains(t, types(t, r, cid), change.EventPRReopened)
}
