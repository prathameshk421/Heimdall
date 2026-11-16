package commands_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/commands"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

func proposalEvent(t *testing.T, typ change.EventType, at time.Time, p any) change.Event {
	t.Helper()
	raw, err := json.Marshal(p)
	require.NoError(t, err)
	return change.Event{Type: typ, OccurredAt: at, Payload: raw, Actor: change.Actor{Kind: change.ActorSystem, ID: "board"}}
}

// The open-proposal predicate (5.3, H10): only the sender's own yes, on the same head, while it is young, confirms.
func TestOpenProposal(t *testing.T) {
	t0 := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	head := "59effae547d768799972d6e54eff32e82dbb7912"
	proposed := change.CommandProposed{Trigger: "msg_0050", Sender: "sw-88", Person: "prathamesh", Text: "dismiss F2 duplicate of F1, same lines",
		Verb: "dismiss", ProposedBy: "lead", HeadSHA: "59effae547d768799972d6e54eff32e82dbb7912"}
	held := []change.Event{proposalEvent(t, change.EventCommandProposed, t0, proposed)}
	confirmed := append(append([]change.Event{}, held...), proposalEvent(t, change.EventCommandConfirmed, t0.Add(time.Minute),
		change.CommandConfirmed{Trigger: "msg_0050", Sender: "sw-88", MessageID: "msg_0051"}))

	tests := []struct {
		name   string
		events []change.Event
		sender change.SenderID
		head   string
		now    time.Time
		open   bool
	}{
		{"the sender, same head, in time", held, "sw-88", head, t0.Add(time.Minute), true},
		{"another person", held, "sw-99", head, t0.Add(time.Minute), false},
		{"after a push", held, "sw-88", "1111111111111111111111111111111111111111", t0.Add(time.Minute), false},
		{"too old", held, "sw-88", head, t0.Add(commands.ProposalTTL + time.Second), false},
		{"already confirmed", confirmed, "sw-88", head, t0.Add(2 * time.Minute), false},
		{"nothing proposed", nil, "sw-88", head, t0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := commands.OpenProposal(tc.events, tc.sender, id.SHA(tc.head), tc.now)
			require.Equal(t, tc.open, ok)
			if ok {
				require.Equal(t, proposed, got)
			}
		})
	}
	require.True(t, commands.IsYes(" Yes "))
	require.False(t, commands.IsYes("yes, but dismiss F3 too"))
}
