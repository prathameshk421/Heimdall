package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// memCalls is the record in memory.
type memCalls struct{ calls []ports.ToolCall }

func (m *memCalls) RecordCall(_ context.Context, c ports.ToolCall) error {
	m.calls = append(m.calls, c)
	return nil
}

func (m *memCalls) RecentCalls(_ context.Context, agent id.AgentName, limit int) ([]ports.ToolCall, error) {
	var out []ports.ToolCall
	for i := len(m.calls) - 1; i >= 0 && len(out) < limit; i-- {
		if m.calls[i].Agent == agent {
			out = append(out, m.calls[i])
		}
	}
	return out, nil
}

func (m *memCalls) TicketCalls(context.Context, id.ChangeID, int) ([]ports.ToolCall, error) {
	return nil, nil
}

type noProposals struct{}

func (noProposals) ProposeCommand(context.Context, id.AgentName, id.ChangeID, string, change.MessageID) (string, error) {
	return "", nil
}

// Every call is recorded, and the third equal refusal in a row reads differently (6.2, H5).
func TestEveryCallIsRecordedAndTheThirdEqualRefusalChanges(t *testing.T) {
	r := newRig(t, workflow.RoleReviewer)
	calls := &memCalls{}
	srv, err := NewServer(Deps{Logger: slog.New(slog.DiscardHandler), Clock: r.clock, Tokens: r.tokens, Tasks: r.tasks,
		Reviews: r.reviews, Code: r.code, Blobs: r.blobs, Knowledge: r.knowledge, Files: r.files,
		Proposer: noProposals{}, Calls: calls})
	require.NoError(t, err)
	p := Principal{Agent: testAgent, Role: workflow.RoleReviewer}
	params := json.RawMessage(`{"name":"propose_command","arguments":{"command":"dismiss F2 same as F1","trigger":"msg_1"}}`)

	var texts []string
	for range 3 {
		out, rpcErr := srv.callTool(context.Background(), p, params)
		require.Nil(t, rpcErr)
		res, ok := out.(toolResult)
		require.True(t, ok)
		require.True(t, res.IsError)
		texts = append(texts, res.Content[0].Text)
	}
	require.Equal(t, texts[0], texts[1], "the first two refusals are the same")
	require.True(t, strings.HasPrefix(texts[2], "Stop: propose_command was refused the same way three times"), texts[2])

	require.Len(t, calls.calls, 3)
	for _, c := range calls.calls {
		require.Equal(t, "refused", c.Outcome)
		require.Equal(t, "propose_command", c.Tool)
		require.Contains(t, c.Args, "dismiss F2")
		require.Equal(t, texts[0], c.Refusal, "the record keeps the real reason")
	}
}
