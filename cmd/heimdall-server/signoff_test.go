package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

func signoffEvents(t *testing.T, verifiedTree string) []change.Event {
	t.Helper()
	raw := json.RawMessage(`{"run_ids":["run_01ARZ3NDEKTSV4RRFFQ69G5FAV"],"sha":"` + string(testSignoffHead) + `","tree_hash":"` + verifiedTree + `","needs_signoff":true}`)
	return []change.Event{{Type: change.EventTestsVerified, Payload: raw}}
}

const testSignoffHead = "0123456789abcdef0123456789abcdef01234567"

// Approving tests binds the approval to the tests tree that was verified, and only while the change waits for it
// (A4, D33). Otherwise the command is refused, so an approval cannot land on tests nobody reviewed.
func TestSignoffApprovalBindsToTheVerifiedTree(t *testing.T) {
	tree := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	evs := signoffEvents(t, tree)
	approved, err := signoffPayload(change.StateAwaitingTestSignoff, evs, "ana")
	require.NoError(t, err)
	require.Equal(t, id.SHA(tree), approved.TreeHash)
	require.Equal(t, change.ActorID("ana"), approved.Approver)
}

func TestSignoffRefusesWhenNothingWaits(t *testing.T) {
	// A verified run exists, but the change is not waiting for sign-off, so the state alone refuses it.
	_, err := signoffPayload(change.StateTestsReady, signoffEvents(t, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), "ana")
	require.ErrorContains(t, err, "nothing is waiting for test sign-off")
}
