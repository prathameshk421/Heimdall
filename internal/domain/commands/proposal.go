package commands

import (
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// ProposalTTL is how long a held proposal waits for its sender's yes (AGENT_INTERACTION_ARCH 5.3: "not too old").
const ProposalTTL = 30 * time.Minute

// OpenProposal returns the proposal a yes from sender would confirm: the newest command.proposed for that sender with
// no command.confirmed, proposed on the head the change has now, and younger than ProposalTTL. A held command is a
// fact; whether it is still open is computed from facts, never stored.
func OpenProposal(events []change.Event, sender change.SenderID, head id.SHA, now time.Time) (change.CommandProposed, bool) {
	confirmed := map[change.MessageID]bool{}
	var open []struct {
		p  change.CommandProposed
		at time.Time
	}
	for _, e := range events {
		p, err := change.Decode(e)
		if err != nil {
			continue
		}
		switch p := p.(type) {
		case change.CommandProposed:
			open = append(open, struct {
				p  change.CommandProposed
				at time.Time
			}{p, e.OccurredAt})
		case change.CommandConfirmed:
			confirmed[p.Trigger] = true
		}
	}
	for i := len(open) - 1; i >= 0; i-- {
		c := open[i]
		if c.p.Sender == sender && !confirmed[c.p.Trigger] && c.p.HeadSHA == head && now.Sub(c.at) <= ProposalTTL {
			return c.p, true
		}
	}
	return change.CommandProposed{}, false
}

// IsYes reports whether a message is the one word that confirms a proposal.
func IsYes(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	return t == "yes" || t == "yes." || t == "yes!"
}
