package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

var _ ports.ToolCalls = (*Store)(nil)

// RecordCall appends one tool call to the record.
func (s *Store) RecordCall(ctx context.Context, c ports.ToolCall) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO tool_call (at, agent, ticket, tool, args, outcome, refusal, result_bytes, duration_ns)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		c.At, string(c.Agent), string(c.Ticket), c.Tool, c.Args, c.Outcome, c.Refusal, c.ResultBytes, int64(c.Duration))
	if err != nil {
		return fmt.Errorf("record tool call %s: %w", c.Tool, err)
	}
	return nil
}

// RecentCalls returns an agent's latest calls, newest first.
func (s *Store) RecentCalls(ctx context.Context, agent id.AgentName, limit int) ([]ports.ToolCall, error) {
	rows, err := s.pool.Query(ctx, `SELECT at, agent, ticket, tool, args, outcome, refusal, result_bytes, duration_ns
		FROM tool_call WHERE agent = $1 ORDER BY at DESC, id DESC LIMIT $2`, string(agent), limit)
	if err != nil {
		return nil, fmt.Errorf("read calls of %s: %w", agent, err)
	}
	return scanCalls(rows)
}

// TicketCalls returns the calls about one ticket, oldest first.
func (s *Store) TicketCalls(ctx context.Context, ticket id.ChangeID, limit int) ([]ports.ToolCall, error) {
	rows, err := s.pool.Query(ctx, `SELECT at, agent, ticket, tool, args, outcome, refusal, result_bytes, duration_ns
		FROM tool_call WHERE ticket = $1 ORDER BY at, id LIMIT $2`, string(ticket), limit)
	if err != nil {
		return nil, fmt.Errorf("read calls of %s: %w", ticket, err)
	}
	return scanCalls(rows)
}

func scanCalls(rows pgx.Rows) ([]ports.ToolCall, error) {
	defer rows.Close()
	var out []ports.ToolCall
	for rows.Next() {
		var c ports.ToolCall
		var agent, ticket string
		var dur int64
		if err := rows.Scan(&c.At, &agent, &ticket, &c.Tool, &c.Args, &c.Outcome, &c.Refusal, &c.ResultBytes, &dur); err != nil {
			return nil, fmt.Errorf("scan tool call: %w", err)
		}
		c.Agent, c.Ticket, c.Duration, c.At = id.AgentName(agent), id.ChangeID(ticket), time.Duration(dur), c.At.UTC()
		out = append(out, c)
	}
	return out, rows.Err()
}
