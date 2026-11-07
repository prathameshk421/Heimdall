package prsync_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/prsync"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// A boot after the server was down reconciles the facts it missed: a push, then the CI result for it, appear once.
// A second boot adds nothing (A5, D56).
func TestBootAfterDowntimeAppendsMissedPushAndCIOnce(t *testing.T) {
	r := newRig(t)
	pr := samplePR(10, head1, ports.PRStateOpen)
	r.prs.prs[testRepo] = []ports.PR{pr}
	changeID := id.ChangeID("PAY-10")
	r.intake.changeID[10] = changeID
	r.seedInitialPROpened(changeID, 10, head1)
	_, err := r.syncer.SyncOnce(context.Background())
	require.NoError(t, err)

	// Down time: the author pushed and CI reported on the new head.
	pushed := samplePR(10, head2, ports.PRStateOpen)
	r.prs.prs[testRepo] = []ports.PR{pushed}
	r.checks.checks[head2] = []prsync.CheckRun{{ID: "check-2", Name: "build", Status: prsync.CheckCompleted, Conclusion: change.CISuccess, SHA: head2}}

	fresh, err := prsync.New(prsync.Config{
		Repos: r.repos, PRs: r.prs, Streams: r.events, Intake: r.intake, Reviews: r.reviews, Checks: r.checks,
		Gate: r.gate, Events: r.events, Clock: r.clock, IDs: r.ids,
	})
	require.NoError(t, err)
	first, err := fresh.SyncOnce(context.Background())
	require.NoError(t, err)
	require.Positive(t, first.Appended, "the missed push and CI result must be recorded at boot")

	again, err := fresh.SyncOnce(context.Background())
	require.NoError(t, err)
	require.Zero(t, again.Appended, "a second boot adds nothing")
}
