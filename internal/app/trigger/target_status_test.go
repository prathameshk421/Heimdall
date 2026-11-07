package trigger_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/trigger"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// statusMove is a changelog entry that moved the status to toStatus.
func statusMove(key id.ChangeID, changelog, toStatus string) ports.TicketChange {
	return ports.TicketChange{Key: key, ChangelogID: changelog, Fields: []string{"status"}, ToStatus: toStatus, At: now}
}

// The trigger decides on the status the move ended at (ToStatus), not the status the ticket has when the poll reads
// it. A ticket that moved to In Progress and back before the poll still starts (TECH_SPEC 2.5).
func TestTriggerUsesTheStatusTheMoveEndedAt(t *testing.T) {
	r := newRig(t)
	r.tickets.changes = []ports.TicketChange{statusMove("KAN-1", "10001", statusInPrg)}
	r.tickets.next = "c1"
	r.tickets.tickets["KAN-1"] = ticket("KAN-1", "To Do", ana)

	res := r.poll(t)

	require.Equal(t, 1, res.Triggered, "the move ended at In Progress, so the change triggers")
	require.Empty(t, res.Ignored)
}

// A move that ended away from In Progress does not trigger, even when the ticket is In Progress by the time of the poll.
func TestTriggerIgnoresAMoveThatEndedAwayFromInProgress(t *testing.T) {
	r := newRig(t)
	r.tickets.changes = []ports.TicketChange{statusMove("KAN-1", "10002", "To Do")}
	r.tickets.next = "c1"
	r.tickets.tickets["KAN-1"] = ticket("KAN-1", statusInPrg, ana)

	res := r.poll(t)

	require.Equal(t, 0, res.Triggered)
	require.Equal(t, map[trigger.Reason]int{trigger.ReasonNotInProgress: 1}, res.Ignored)
	require.Empty(t, r.starter.stream("KAN-1"))
}
