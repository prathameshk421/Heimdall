package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

func waitingForSignoff(tree string, needs bool) change.View {
	return change.View{Tests: &change.TestsInfo{Rejected: []string{},
		Verified: &change.TestsVerifiedInfo{TreeHash: id.SHA(tree), NeedsSignoff: needs}}}
}

// Approving tests binds the approval to the tests tree that was verified, and only while the change waits for it
// (A4, D33). Otherwise the command is refused, so an approval cannot land on tests nobody reviewed.
func TestSignoffApprovalBindsToTheVerifiedTree(t *testing.T) {
	tree := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	approved, err := signoffPayload(waitingForSignoff(tree, true), "ana")
	require.NoError(t, err)
	require.Equal(t, id.SHA(tree), approved.TreeHash)
	require.Equal(t, change.ActorID("ana"), approved.Approver)
}

func TestSignoffRefusesWhenNothingWaits(t *testing.T) {
	// A verified run exists, but it needs no sign-off, so the predicate refuses it.
	_, err := signoffPayload(waitingForSignoff("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", false), "ana")
	require.ErrorContains(t, err, "nothing is waiting for test sign-off")
	require.ErrorIs(t, err, change.ErrRefused)
	_, err = signoffPayload(change.View{}, "ana")
	require.ErrorIs(t, err, change.ErrRefused)
}
