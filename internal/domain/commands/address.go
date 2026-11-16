package commands

import (
	"regexp"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

// Message is one chat message as the board sees it (AGENT_INTERACTION_ARCH F5, H4).
type Message struct {
	Room             string `json:"room"`
	ThreadRootTicket string `json:"thread_root_ticket"` // the change whose status message roots the thread; "" at the top level
	Sender           string `json:"sender"`
	MentionsBoard    bool   `json:"mentions_board"` // the chat port says the board itself was mentioned
	Text             string `json:"text"`
}

// Address is who a message is for and which change it is about.
type Address struct {
	Ticket       string     `json:"ticket"`
	TicketSource string     `json:"ticket_source"` // thread_root, text or none
	For          string     `json:"for"`           // board, board_relay, agent or nobody
	Agent        string     `json:"agent"`         // lead, an agent name or a seat; "" when no agent is addressed
	Command      *VerbClass `json:"command"`
}

// VerbClass is a grammar command's verb and class. A typed command never needs a confirmation (5.1): the "reply yes"
// step is only for a command an agent proposes for a person (5.3).
type VerbClass struct {
	Verb              string `json:"verb"`
	Class             string `json:"class"`
	NeedsConfirmation bool   `json:"needs_confirmation"`
}

// Verb classes (5.3): a light verb runs at once; a deciding verb an agent proposes waits for the sender's yes.
const (
	ClassLight    = "light"
	ClassDeciding = "deciding"
)

// Classes is the verb class table.
func Classes() map[string]string {
	return map[string]string{
		"start": ClassLight, "review": ClassLight, "remember": ClassLight, "incident": ClassLight,
		"challenge": ClassLight, "answer": ClassLight,
		"uphold": ClassDeciding, "dismiss": ClassDeciding, "approve-tests": ClassDeciding,
		"waive-reviewer": ClassDeciding, "spec-change": ClassDeciding, "resume": ClassDeciding,
		"forget": ClassDeciding, "override": ClassDeciding, "reject": ClassDeciding,
	}
}

var (
	ticketKey = regexp.MustCompile(`\b[A-Z][A-Z0-9]*-[1-9][0-9]*\b`)
	mention   = regexp.MustCompile(`(?:^|\s)@([A-Za-z][A-Za-z0-9_-]*)`)
)

// Route says which change a message is about and who it is for (F5): a thread message takes its root's change, a
// top-level one the key in its text; grammar goes to the board, a board mention that is not grammar is relayed to the
// lead, an @ reaches the agent or the seat holding that role, and anything else reaches nobody.
func Route(m Message, seats map[string]policy.Seat) Address {
	var a Address
	switch key := ticketKey.FindString(m.Text); {
	case m.ThreadRootTicket != "":
		a.Ticket, a.TicketSource = m.ThreadRootTicket, "thread_root"
	case key != "":
		a.Ticket, a.TicketSource = key, "text"
	default:
		a.TicketSource = "none"
	}
	if verb, ok := grammarVerb(m.Text); ok {
		a.For = "board"
		if class, known := Classes()[verb]; known {
			a.Command = &VerbClass{Verb: verb, Class: class}
		}
		return a
	}
	if m.MentionsBoard {
		a.For, a.Agent = "board_relay", "lead"
		return a
	}
	if name := firstMention(m.Text); name != "" {
		a.For, a.Agent = "agent", addressee(name, seats)
		return a
	}
	a.For = "nobody"
	return a
}

// grammarVerb reads `heimdall <verb> ...`: the plain word, then the verb.
func grammarVerb(text string) (string, bool) {
	fields := strings.Fields(text)
	if len(fields) < 2 || !strings.EqualFold(fields[0], "heimdall") {
		return "", false
	}
	return strings.ToLower(fields[1]), true
}

func firstMention(text string) string {
	if m := mention.FindStringSubmatch(text); m != nil {
		return m[1]
	}
	return ""
}

// addressee is the agent an @name reaches: the lead for heimdall, the seat that holds a role, or the name itself.
func addressee(name string, seats map[string]policy.Seat) string {
	if strings.EqualFold(name, "heimdall") {
		return "lead"
	}
	for seat, s := range seats {
		if s.Role == name {
			return seat
		}
	}
	return name
}
