package people

import (
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

// ChatLink is a chat sender id that proved it belongs to a GitHub login: the login commented a one-time code that the
// sender was given in chat (D194).
type ChatLink struct {
	Sender string
	GitHub string
}

// Link maps each linked chat sender to the person who holds its GitHub login, so one person can use several chat
// accounts without anyone editing the policy. The policy stays the authority: a sender the policy already maps, or one
// that is an agent name, is not moved; a login no person holds maps nobody; a sender id that does not parse is skipped.
// Roles still come from the policy, so a link grants exactly what the person already has.
func (d *Directory) Link(links []ChatLink) {
	for _, l := range links {
		sid, err := change.ParseSenderID(l.Sender)
		if err != nil {
			continue
		}
		sender := string(sid)
		if _, mapped := d.chat[sender]; mapped {
			continue
		}
		if _, agent := d.agents[sender]; agent {
			continue
		}
		name, ok := d.logins[strings.ToLower(l.GitHub)]
		if !ok {
			continue
		}
		d.chat[sender] = name
	}
}
