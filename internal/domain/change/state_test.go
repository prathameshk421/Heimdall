package change_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

// specStates is TECH_SPEC 3.3, spelled exactly.
var specStates = []string{
	"TRIGGERED", "ROOM_OPEN", "WRITING_TESTS", "VERIFYING_TESTS", "AWAITING_TEST_SIGNOFF", "TESTS_READY",
	"BUILDING", "PR_OPEN", "SLOP_REJECTED", "UNLINKED_PR", "PICKING_REVIEWERS", "IN_REVIEW", "DELIBERATING",
	"ESCALATED", "FIXING", "VERIFYING_FIX", "READY_FOR_APPROVAL", "APPROVED", "MERGED", "REJECTED",
}

func TestAllStatesMatchTheSpecInOrder(t *testing.T) {
	var got []string
	for _, s := range change.AllStates() {
		require.True(t, s.Valid(), s)
		got = append(got, string(s))
	}
	require.Equal(t, specStates, got)
}

func TestStateValidRefusesUnknownAndLowerCaseNames(t *testing.T) {
	for _, bad := range []change.State{"", "merged", "IN REVIEW", "PR OPEN", "BOGUS"} {
		require.False(t, bad.Valid(), bad)
	}
}

func TestOnlyMergedAndRejectedAreTerminal(t *testing.T) {
	for _, s := range change.AllStates() {
		want := s == change.StateMerged || s == change.StateRejected
		require.Equal(t, want, s.IsTerminal(), s)
	}
}

func TestAllStatesReturnsAFreshSlice(t *testing.T) {
	a := change.AllStates()
	a[0] = "TAMPERED"
	require.Equal(t, change.StateTriggered, change.AllStates()[0])
}
