package trigger_test

import (
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// statusMove is a changelog entry that moved the status to toStatus.
func statusMove(key id.ChangeID, changelog, toStatus string) ports.TicketChange {
	return ports.TicketChange{Key: key, ChangelogID: changelog, Fields: []string{"status"}, ToStatus: toStatus, At: now}
}
