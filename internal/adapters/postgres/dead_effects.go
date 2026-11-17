package postgres

import (
	"context"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

var _ ports.DeadEffects = (*Store)(nil)

// deadEffectsSQL joins the outbox to the events of one change. A row is dead after maxAttempts failures, and in doubt
// when it was started and its lease ran out (the drainer never claims either again).
const deadEffectsSQL = `
SELECT o.effect_key, o.kind, o.attempts, COALESCE(o.last_error, ''),
       (o.started_at IS NOT NULL AND o.claimed_until < now()) AS in_doubt
FROM outbox o
JOIN event e ON e.id = o.event_id
WHERE e.change_id = $1
  AND o.done_at IS NULL
  AND (o.attempts >= $2 OR (o.started_at IS NOT NULL AND o.claimed_until < now()))
ORDER BY o.created_at, o.id`

// DeadEffects implements ports.DeadEffects.
func (s *Store) DeadEffects(ctx context.Context, changeID id.ChangeID) ([]change.DeadJob, error) {
	rows, err := s.pool.Query(ctx, deadEffectsSQL, string(changeID), maxAttempts)
	if err != nil {
		return nil, fmt.Errorf("dead effects of %s: %w", changeID, err)
	}
	defer rows.Close()
	var out []change.DeadJob
	for rows.Next() {
		var d change.DeadJob
		var key, kind string
		if err := rows.Scan(&key, &kind, &d.Attempts, &d.LastError, &d.InDoubt); err != nil {
			return nil, fmt.Errorf("dead effects of %s: %w", changeID, err)
		}
		d.Key, d.Kind = change.EffectKey(key), change.EffectKind(kind)
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dead effects of %s: %w", changeID, err)
	}
	return out, nil
}
