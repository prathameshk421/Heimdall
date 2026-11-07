package prsync_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/prsync"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// GitHub does not add a review when an approval is dismissed: the SAME review changes from APPROVED to DISMISSED.
// The sync must notice a state change on a review it has already recorded, or a dismissed approval stays valid.
func TestLeadDismissingTheSameReviewInvalidatesTheApproval(t *testing.T) {
	r := newRig(t)
	pr := samplePR(10, head1, ports.PRStateOpen)
	r.prs.prs[testRepo] = []ports.PR{pr}
	changeID := id.ChangeID("PAY-10")
	r.intake.changeID[10] = changeID
	r.seedInitialPROpened(changeID, 10, head1)

	review := prsync.PRReview{ID: "rev-1", Login: reviewerBob, State: prsync.ReviewApproved, CommitSHA: head1, SubmittedAt: testNow}
	r.reviews.reviews[10] = []prsync.PRReview{review}
	_, err := r.syncer.SyncOnce(context.Background())
	require.NoError(t, err)

	review.State = prsync.ReviewDismissed // same id, new state
	r.reviews.reviews[10] = []prsync.PRReview{review}
	res, err := r.syncer.SyncOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, res.Appended, "the dismissal must be recorded")

	evs, err := r.events.Load(context.Background(), changeID)
	require.NoError(t, err)
	require.Equal(t, change.EventApprovalInvalidated, evs[len(evs)-1].Type)
	var inv change.ApprovalInvalidated
	require.NoError(t, json.Unmarshal(evs[len(evs)-1].Payload, &inv))
	require.Equal(t, reviewerBob, inv.Login)
	require.Equal(t, head1, inv.CommitSHA)

	// A third sync with nothing new appends nothing.
	res, err = r.syncer.SyncOnce(context.Background())
	require.NoError(t, err)
	require.Zero(t, res.Appended)
}

// A fresh syncer (a restart) must reach the same decision from the event log alone.
func TestLeadDismissalIsFoundAfterARestart(t *testing.T) {
	r := newRig(t)
	pr := samplePR(10, head1, ports.PRStateOpen)
	r.prs.prs[testRepo] = []ports.PR{pr}
	changeID := id.ChangeID("PAY-10")
	r.intake.changeID[10] = changeID
	r.seedInitialPROpened(changeID, 10, head1)

	review := prsync.PRReview{ID: "rev-1", Login: reviewerBob, State: prsync.ReviewApproved, CommitSHA: head1, SubmittedAt: testNow}
	r.reviews.reviews[10] = []prsync.PRReview{review}
	_, err := r.syncer.SyncOnce(context.Background())
	require.NoError(t, err)

	// A new Syncer on the same event log and the same GitHub fakes: it has no memory of the first one.
	fresh, err := prsync.New(prsync.Config{
		Repos: r.repos, PRs: r.prs, Streams: r.events, Intake: r.intake, Reviews: r.reviews, Checks: r.checks,
		Gate: r.gate, Events: r.events, Clock: r.clock, IDs: r.ids,
	})
	require.NoError(t, err)
	review.State = prsync.ReviewDismissed
	r.reviews.reviews[10] = []prsync.PRReview{review}
	res, err := fresh.SyncOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, res.Appended)
}
