package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// GetCursor returns the value of a poller or stream cursor (TECH_SPEC 2.1, table cursor). An unknown source is
// ports.ErrNotFound, so the caller can start from the beginning rather than read an empty value.
func (s *Store) GetCursor(ctx context.Context, source string) (string, error) {
	var value string
	err := s.pool.QueryRow(ctx, "SELECT value FROM cursor WHERE source = $1", source).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("cursor %s: %w", source, ports.ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("cursor %s: %w", source, err)
	}
	return value, nil
}

// SetCursor stores the value of a cursor, creating the row on first use.
func (s *Store) SetCursor(ctx context.Context, source, value string) error {
	if source == "" {
		return errors.New("cursor source must not be empty")
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO cursor (source, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (source) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, source, value)
	if err != nil {
		return fmt.Errorf("set cursor %s: %w", source, err)
	}
	return nil
}

// SeenDelivery records a webhook delivery and reports whether it was already recorded. The insert is ON CONFLICT
// DO NOTHING, so the first caller sees false and every later one sees true.
func (s *Store) SeenDelivery(ctx context.Context, provider, deliveryID string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `INSERT INTO webhook_delivery (provider, delivery_id) VALUES ($1, $2)
		ON CONFLICT (provider, delivery_id) DO NOTHING`, provider, deliveryID)
	if err != nil {
		return false, fmt.Errorf("record delivery %s/%s: %w", provider, deliveryID, err)
	}
	return tag.RowsAffected() == 0, nil
}
