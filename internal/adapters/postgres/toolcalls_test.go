//go:build component

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// The record round-trips on real Postgres: an agent's latest calls newest first, a ticket's calls oldest first.
func TestToolCallsAreRecordedAndReadBack(t *testing.T) {
	ctx := context.Background()
	srv := startServer(t)
	s := NewStore(srv.database(ctx, t).pool)
	t0 := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	calls := []ports.ToolCall{
		{At: t0, Agent: "reviewer-1", Ticket: "KAN-22", Tool: "get_task", Args: "{}", Outcome: "ok", ResultBytes: 900, Duration: time.Second},
		{At: t0.Add(time.Minute), Agent: "reviewer-1", Ticket: "KAN-22", Tool: "submit_review", Args: `{"clean":true}`, Outcome: "refused", Refusal: "rule_checks: missing"},
		{At: t0.Add(2 * time.Minute), Agent: "reviewer-2", Ticket: "KAN-23", Tool: "get_task", Outcome: "ok"},
	}
	for _, c := range calls {
		require.NoError(t, s.RecordCall(ctx, c))
	}

	recent, err := s.RecentCalls(ctx, "reviewer-1", 10)
	require.NoError(t, err)
	require.Equal(t, []ports.ToolCall{calls[1], calls[0]}, recent)

	ticket, err := s.TicketCalls(ctx, "KAN-22", 10)
	require.NoError(t, err)
	require.Equal(t, calls[:2], ticket)

	require.Error(t, s.RecordCall(ctx, ports.ToolCall{At: t0, Agent: "x", Tool: "y", Outcome: "maybe"}), "the outcome is one of three")
}
