package postgres

import (
	"context"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

var _ ports.OpsReader = (*Store)(nil)

// OpsSnapshot implements ports.OpsReader (O6): queue depth and oldest age per effect kind, dead and in-doubt counts,
// dead chat messages from the cursor table, and open change count.
func (s *Store) OpsSnapshot(ctx context.Context) (ports.OpsSnapshot, error) {
	snap := ports.OpsSnapshot{
		QueuedByKind:        map[change.EffectKind]int{},
		OldestQueuedSeconds: map[change.EffectKind]int64{},
		DeadByKind:          map[change.EffectKind]int{},
	}
	if err := s.readOutboxOps(ctx, &snap); err != nil {
		return ports.OpsSnapshot{}, err
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM cursor WHERE source LIKE 'switch:msg-dead:%'`).Scan(&snap.DeadMessages); err != nil {
		return ports.OpsSnapshot{}, fmt.Errorf("ops snapshot: dead messages: %w", err)
	}
	open, err := s.OpenChanges(ctx)
	if err != nil {
		return ports.OpsSnapshot{}, fmt.Errorf("ops snapshot: open changes: %w", err)
	}
	snap.OpenChanges = len(open)
	return snap, nil
}

func (s *Store) readOutboxOps(ctx context.Context, snap *ports.OpsSnapshot) error {
	rows, err := s.pool.Query(ctx, `
SELECT kind,
       count(*) FILTER (WHERE attempts < $1 AND (started_at IS NULL OR claimed_until >= now())) AS queued,
       COALESCE(MAX(EXTRACT(EPOCH FROM (now() - created_at))::bigint) FILTER (WHERE attempts < $1 AND (started_at IS NULL OR claimed_until >= now())), 0) AS oldest_sec,
       count(*) FILTER (WHERE attempts >= $1) AS dead,
       count(*) FILTER (WHERE started_at IS NOT NULL AND claimed_until < now()) AS in_doubt
FROM outbox
WHERE done_at IS NULL
GROUP BY kind`, maxAttempts)
	if err != nil {
		return fmt.Errorf("ops snapshot: outbox: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var queued, dead, inDoubt int
		var oldest int64
		if err := rows.Scan(&kind, &queued, &oldest, &dead, &inDoubt); err != nil {
			return fmt.Errorf("ops snapshot: scan outbox: %w", err)
		}
		k := change.EffectKind(kind)
		if queued > 0 {
			snap.QueuedByKind[k] = queued
			snap.OldestQueuedSeconds[k] = oldest
		}
		if dead > 0 {
			snap.DeadByKind[k] = dead
			snap.DeadCount += dead
		}
		snap.InDoubtCount += inDoubt
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("ops snapshot: outbox rows: %w", err)
	}
	return nil
}
