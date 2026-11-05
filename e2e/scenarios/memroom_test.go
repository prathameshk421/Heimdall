package scenarios_test

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/e2e"
)

// The room of the test double: one room, a product that obeys `heimdall uphold` from the approver and refuses it
// from anyone else, as a correct product does. The quirks make it wrong.

const roomName = "kan-1-add-refunds"

func (w *memWorld) Rooms(context.Context) ([]string, error) { return []string{roomName}, nil }

func (w *memWorld) Members(context.Context, string) ([]string, error) {
	return []string{w.story.Author().Chat}, nil
}

func (w *memWorld) Transcript(context.Context, string) ([]e2e.Message, error) {
	return append([]e2e.Message(nil), w.msgs...), nil
}

func (w *memWorld) post(from string, agent bool, kind e2e.MessageKind, text string) {
	w.now = w.now.Add(time.Second)
	w.msgs = append(w.msgs, e2e.Message{
		ID: fmt.Sprintf("m%d", len(w.msgs)+1), Room: roomName, From: from, FromAgent: agent, Kind: kind, Text: text, At: w.now,
	})
}

func (w *memWorld) Say(_ context.Context, _, as, text string) error {
	agent := strings.HasSuffix(as, "-reviewer")
	w.post(as, agent, e2e.KindHuman, text)
	f := strings.Fields(text)
	if len(f) < 3 || f[0] != "heimdall" || f[1] != "uphold" {
		return nil
	}
	w.uphold(as, agent, f[2], strings.Join(f[3:], " "))
	return nil
}

// uphold is the product's handling of the command: only the approver may, and the others are told who may.
func (w *memWorld) uphold(as string, agent bool, id, reason string) {
	approver := w.story.Approver()
	switch {
	case as == approver.Chat, as == w.story.Author().Chat && w.q.authorCanUphold, agent && w.q.agentCanUphold:
		w.markUpheld(id)
		w.comments = append(w.comments, e2e.PRComment{ID: 1, Author: "heimdall[bot]",
			Body: fmt.Sprintf("%s upheld by %s on %s: %s", id, approver.GitHub, w.now.Format("2006-01-02"), reason)})
		w.post("heimdall", true, e2e.KindOther, id+" is upheld by "+approver.Chat+".")
	case agent:
		return // commands from unmapped senders, agents among them, are ignored
	case as == w.story.Author().Chat:
		w.post("heimdall", true, e2e.KindOther, "You cannot uphold a finding on your own change; only @"+approver.Chat+" may.")
	default:
		w.post("heimdall", true, e2e.KindOther, "I do not know you yet, so I cannot take that. Only @"+approver.Chat+" may uphold a finding; ask them to link your account.")
	}
}

func (w *memWorld) markUpheld(id string) {
	for i := range w.findings {
		if w.findings[i].ID == id {
			w.findings[i].Upheld = true
		}
	}
}
