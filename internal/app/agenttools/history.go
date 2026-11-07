package agenttools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

// maxHistoryLines bounds how much of a ticket's story one task carries.
const maxHistoryLines = 25

// ticketHistory tells one ticket's story in plain lines, oldest first: what was submitted, why it was rejected, what the
// agent reported, what a person decided. It belongs to this ticket only, so an agent that works on several tickets sees
// each one's own story and never another's.
func ticketHistory(events []change.Event) []string {
	var lines []string
	for _, ev := range events {
		if line, ok := historyLine(ev); ok {
			lines = append(lines, line)
		}
	}
	if len(lines) > maxHistoryLines {
		lines = lines[len(lines)-maxHistoryLines:]
	}
	return lines
}

func historyLine(ev change.Event) (string, bool) {
	var p struct {
		Reasons []string `json:"reasons"`
		Text    string   `json:"text"`
		Kind    string   `json:"kind"`
		Reason  string   `json:"reason"`
		Attempt int      `json:"attempt"`
		Refs    []string `json:"refs"`
	}
	if json.Unmarshal(ev.Payload, &p) != nil {
		return "", false
	}
	lines := map[change.EventType]string{
		change.EventTestsSubmitted:   fmt.Sprintf("Tests were submitted (%d file(s)).", len(p.Refs)),
		change.EventTestsRejected:    "The tests were rejected: " + strings.Join(p.Reasons, "; ") + ".",
		change.EventStepRetried:      fmt.Sprintf("The task was given again (attempt %d).", p.Attempt),
		change.EventTestsVerified:    "The tests were verified: they fail on the current code, as they should.",
		change.EventTestsCommitted:   "The tests were committed.",
		change.EventTaskProgress:     "The agent reported: " + p.Text,
		change.EventDecisionRecorded: "A person decided: " + p.Kind + " " + p.Reason,
	}
	if line, ok := lines[ev.Type]; ok {
		return line, true
	}
	return "", false
}
