package trigger_test

import (
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/trigger"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// An app account (an automation) assigned to a ticket is not a person, so the change does not start (TECH_SPEC 13).
// The rule is checked here, in the trigger, not only in the adapter, because a ticket can name an app account from any
// tracker.
func TestIgnoresATicketAssignedToAnAppAccount(t *testing.T) {
	r := newRig(t)
	r.tickets.changes = []ports.TicketChange{statusMove("KAN-1", "10001", statusInPrg)}
	r.tickets.next = "c1"
	tk := ticket("KAN-1", statusInPrg, ana)
	tk.AssigneeIsApp = true
	r.tickets.tickets["KAN-1"] = tk

	assertIgnored(t, r, r.poll(t), trigger.ReasonAppAccount, "KAN-1")
}
