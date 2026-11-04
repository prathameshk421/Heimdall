package ports

import (
	"context"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// TicketReader reads tickets. Consumers: the trigger poll and the acceptance-criteria parser (TECH_SPEC 13, D50).
// Adapters: internal/adapters/jira (JQL poll, task T25) and internal/adapters/file (seed/tickets, T25).
type TicketReader interface {
	// Changes returns the ticket changes after since. The Jira adapter re-queries from since minus 10 minutes and
	// dedupes by key and changelog id (TECH_SPEC 13).
	Changes(ctx context.Context, since Cursor) ([]TicketChange, Cursor, error)
	// Ticket returns one ticket with its acceptance criteria.
	Ticket(ctx context.Context, key id.ChangeID) (Ticket, error)
}

// TicketWriter writes to tickets. Consumer: the outbox drainer. Every write is gated by policy (TECH_SPEC 13).
type TicketWriter interface {
	// Comment adds a comment; the effect key makes a repeat a no-op.
	Comment(ctx context.Context, ticket id.ChangeID, key change.EffectKey, text string) error
	// CreateTicket creates a ticket and returns its key.
	CreateTicket(ctx context.Context, d TicketDraft) (id.ChangeID, error)
}
