package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// maxAttempts is the number of failed attempts after which an effect leaves Pending for the dead-letter view
// (TECH_SPEC 2.4). The outbox_dead view in the migration uses the same number.
const maxAttempts = 8

// Pending returns up to limit effects that are not done and have failed fewer than maxAttempts times, oldest first.
// It is a plain select: a claim with FOR UPDATE SKIP LOCKED belongs to the drainer (T22), not to this port.
func (s *Store) Pending(ctx context.Context, limit int) ([]change.Effect, error) {
	rows, err := s.pool.Query(ctx, `SELECT effect_key, kind, target, payload FROM outbox
		WHERE done_at IS NULL AND attempts < $1
		ORDER BY created_at, id LIMIT $2`, maxAttempts, limit)
	if err != nil {
		return nil, fmt.Errorf("pending effects: %w", err)
	}
	defer rows.Close()
	var out []change.Effect
	for rows.Next() {
		var key, kind, target string
		var payload []byte
		if err := rows.Scan(&key, &kind, &target, &payload); err != nil {
			return nil, fmt.Errorf("pending effects: read: %w", err)
		}
		out = append(out, change.Effect{
			Key:     change.EffectKey(key),
			Kind:    change.EffectKind(kind),
			Target:  target,
			Payload: payload,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pending effects: %w", err)
	}
	return out, nil
}

// Done marks an effect as performed. A second call changes nothing and is not an error; the first done_at stays.
func (s *Store) Done(ctx context.Context, key change.EffectKey) error {
	tag, err := s.pool.Exec(ctx,
		"UPDATE outbox SET done_at = COALESCE(done_at, now()) WHERE effect_key = $1", string(key))
	if err != nil {
		return fmt.Errorf("mark effect %s done: %w", key, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("mark effect %s done: %w", key, ports.ErrNotFound)
	}
	return nil
}

// Fail records one failed attempt and its reason. A failure reported after the effect is done is refused, because
// it would contradict a performed effect; the drainer must not report both.
func (s *Store) Fail(ctx context.Context, key change.EffectKey, reason string) error {
	tag, err := s.pool.Exec(ctx,
		"UPDATE outbox SET attempts = attempts + 1, last_error = $2 WHERE effect_key = $1 AND done_at IS NULL",
		string(key), reason)
	if err != nil {
		return fmt.Errorf("record failure of %s: %w", key, err)
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	var done bool
	err = s.pool.QueryRow(ctx, "SELECT done_at IS NOT NULL FROM outbox WHERE effect_key = $1", string(key)).Scan(&done)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("record failure of %s: %w", key, ports.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("record failure of %s: %w", key, err)
	}
	if done {
		return fmt.Errorf("record failure of %s: the effect is already done", key)
	}
	return fmt.Errorf("record failure of %s: no row changed", key)
}
