package postgres

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

var _ ports.AgentReceiptLedger = (*Store)(nil)

// RecordAgentSetupReceipt stores only the fingerprint of the accepted host receipt.
// Replays replace the fingerprint for that agent without creating duplicate rows.
func (s *Store) RecordAgentSetupReceipt(ctx context.Context, agent id.AgentName, fingerprint string) (bool, error) {
	if !agent.Valid() || len(fingerprint) != 64 || strings.ToLower(fingerprint) != fingerprint {
		return false, fmt.Errorf("record agent setup receipt: invalid agent or fingerprint")
	}
	if _, err := hex.DecodeString(fingerprint); err != nil {
		return false, fmt.Errorf("record agent setup receipt: invalid fingerprint")
	}
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO agent_setup_receipts (agent_name, fingerprint)
		VALUES ($1, $2)
		ON CONFLICT (agent_name, fingerprint) DO NOTHING
	`, string(agent), fingerprint)
	if err != nil {
		return false, fmt.Errorf("record agent setup receipt: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// LoadAgentSetupReceipt returns the newest receipt accepted for agent, if any.
func (s *Store) LoadAgentSetupReceipt(ctx context.Context, agent id.AgentName) (ports.AgentSetupReceipt, bool, error) {
	if !agent.Valid() {
		return ports.AgentSetupReceipt{}, false, fmt.Errorf("load agent setup receipt: invalid agent")
	}
	var fingerprint string
	err := s.pool.QueryRow(ctx, `SELECT fingerprint FROM agent_setup_receipts WHERE agent_name = $1 ORDER BY accepted_at DESC, fingerprint LIMIT 1`, string(agent)).Scan(&fingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.AgentSetupReceipt{}, false, nil
	}
	if err != nil {
		return ports.AgentSetupReceipt{}, false, fmt.Errorf("load agent setup receipt fingerprint: %w", err)
	}
	return ports.AgentSetupReceipt{Agent: agent, Fingerprint: fingerprint}, true, nil
}
