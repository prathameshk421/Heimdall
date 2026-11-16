package ports

import (
	"context"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// ToolCall is one Heimdall tool call as the record keeps it (AGENT_INTERACTION_ARCH 9): for audit and debugging,
// never a chat feature. Args is a redacted, bounded summary; the full arguments are never stored.
type ToolCall struct {
	At          time.Time     `json:"at"`
	Agent       id.AgentName  `json:"agent"`
	Ticket      id.ChangeID   `json:"ticket,omitempty"`
	Tool        string        `json:"tool"`
	Args        string        `json:"args"`
	Outcome     string        `json:"outcome"` // ok, refused or failed
	Refusal     string        `json:"refusal,omitempty"`
	ResultBytes int           `json:"result_bytes"`
	Duration    time.Duration `json:"duration_ns"`
}

// ToolCalls is the record of tool calls. Adapter: postgres (migration 0013).
type ToolCalls interface {
	RecordCall(ctx context.Context, c ToolCall) error
	// RecentCalls returns an agent's latest calls, newest first.
	RecentCalls(ctx context.Context, agent id.AgentName, limit int) ([]ToolCall, error)
	// TicketCalls returns the calls about one ticket, oldest first.
	TicketCalls(ctx context.Context, ticket id.ChangeID, limit int) ([]ToolCall, error)
}
