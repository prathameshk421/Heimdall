package postgres

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// maxAttempts is the number of failed attempts after which an effect leaves Pending for the dead-letter view
// (TECH_SPEC 2.4). The outbox_dead view in the migration uses the same number.
const maxAttempts = 8

// Pending returns up to limit effects that are not done and have failed fewer than maxAttempts times, oldest first.
// It is a plain select for inspection: it takes no lease, so the drainer uses Claim instead.
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

// Start records that the executor of a claimed effect is about to run, before it runs (T22, TECH_SPEC 2.4). The
// UPDATE matches only an undone row that has not started and whose lease is still live on the database clock. A
// drainer that lost its lease, or that starts a row twice, is refused, and so it must run nothing. An unknown key is
// ports.ErrNotFound.
func (s *Store) Start(ctx context.Context, key change.EffectKey) error {
	tag, err := s.pool.Exec(ctx, `UPDATE outbox SET started_at = now()
		WHERE effect_key = $1 AND done_at IS NULL AND started_at IS NULL AND claimed_until > now()`, string(key))
	if err != nil {
		return fmt.Errorf("start effect %s: %w", key, err)
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	var found bool
	err = s.pool.QueryRow(ctx, "SELECT true FROM outbox WHERE effect_key = $1", string(key)).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("start effect %s: %w", key, ports.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("start effect %s: %w", key, err)
	}
	return fmt.Errorf("start effect %s: the effect is not under a live lease of an unstarted row", key)
}

// Done marks an effect as performed and releases its lease. A second call changes nothing and is not an error; the
// first done_at stays.
func (s *Store) Done(ctx context.Context, key change.EffectKey) error {
	tag, err := s.pool.Exec(ctx,
		"UPDATE outbox SET done_at = COALESCE(done_at, now()), claimed_until = NULL WHERE effect_key = $1", string(key))
	if err != nil {
		return fmt.Errorf("mark effect %s done: %w", key, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("mark effect %s done: %w", key, ports.ErrNotFound)
	}
	return nil
}

// Fail records one failed attempt, its reason, and releases the lease, so the effect is claimable again at once. It
// also clears the start mark: a reported failure means the action did not happen, so the retry is not in doubt.
// A failure reported after the effect is done is refused, because it would contradict a performed effect; the
// drainer must not report both.
func (s *Store) Fail(ctx context.Context, key change.EffectKey, reason string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE outbox SET attempts = attempts + 1, last_error = $2, claimed_until = NULL, started_at = NULL
		WHERE effect_key = $1 AND done_at IS NULL`,
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

// Claim takes a lease on up to limit effects that are not done, have failed fewer than maxAttempts times, have not
// started, and whose lease is free or expired. A started row whose lease ran out is in doubt and is never returned
// (outbox_in_doubt). The rows are chosen oldest first with FOR UPDATE SKIP LOCKED, so two claims running at once
// never choose the same row, and the UPDATE sets claimed_until = now() + lease in the same statement, so the lease
// starts in the database's clock. The returned effects are in creation order. limit and lease must be positive; a
// lease under one microsecond cannot be stored and is refused.
func (s *Store) Claim(ctx context.Context, limit int, lease time.Duration) ([]change.Effect, error) {
	if limit < 1 {
		return nil, fmt.Errorf("claim effects: limit %d must be 1 or more", limit)
	}
	micros := lease.Microseconds()
	if micros < 1 {
		return nil, fmt.Errorf("claim effects: lease %s must be at least one microsecond", lease)
	}
	rows, err := s.pool.Query(ctx, `UPDATE outbox SET claimed_until = now() + $3::bigint * interval '1 microsecond'
		WHERE id IN (
		  SELECT id FROM outbox
		  WHERE done_at IS NULL AND attempts < $1 AND started_at IS NULL AND (claimed_until IS NULL OR claimed_until <= now())
		  ORDER BY created_at, id
		  LIMIT $2
		  FOR UPDATE SKIP LOCKED)
		RETURNING id, created_at, effect_key, kind, target, payload`, maxAttempts, limit, micros)
	if err != nil {
		return nil, fmt.Errorf("claim effects: %w", err)
	}
	defer rows.Close()
	type claimed struct {
		id        int64
		createdAt time.Time
		effect    change.Effect
	}
	var got []claimed
	for rows.Next() {
		var c claimed
		var key, kind, target string
		var payload []byte
		if err := rows.Scan(&c.id, &c.createdAt, &key, &kind, &target, &payload); err != nil {
			return nil, fmt.Errorf("claim effects: read: %w", err)
		}
		c.effect = change.Effect{Key: change.EffectKey(key), Kind: change.EffectKind(kind), Target: target, Payload: payload}
		got = append(got, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("claim effects: %w", err)
	}
	// UPDATE ... RETURNING makes no order promise, so the claim restores the order of Pending.
	slices.SortFunc(got, func(a, b claimed) int {
		if c := a.createdAt.Compare(b.createdAt); c != 0 {
			return c
		}
		return cmp.Compare(a.id, b.id)
	})
	out := make([]change.Effect, 0, len(got))
	for _, c := range got {
		out = append(out, c.effect)
	}
	return out, nil
}

// ClaimEffect claims only key, preserving River's stable job identity across retries.
func (s *Store) ClaimEffect(ctx context.Context, key change.EffectKey, lease time.Duration) (*change.Effect, error) {
	micros := lease.Microseconds()
	if micros < 1 {
		return nil, fmt.Errorf("claim effect %s: lease must be at least one microsecond", key)
	}
	var fx change.Effect
	var kind, target string
	var payload []byte
	err := s.pool.QueryRow(ctx, `UPDATE outbox SET claimed_until = now() + $2::bigint * interval '1 microsecond'
		WHERE effect_key = $1 AND done_at IS NULL AND attempts < $3 AND started_at IS NULL AND (claimed_until IS NULL OR claimed_until <= now())
		RETURNING effect_key, kind, target, payload`, string(key), micros, maxAttempts).Scan(&fx.Key, &kind, &target, &payload)
	if err == nil {
		fx.Kind, fx.Target, fx.Payload = change.EffectKind(kind), target, payload
		return &fx, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("claim effect %s: %w", key, err)
	}
	var leased bool
	err = s.pool.QueryRow(ctx, "SELECT claimed_until > now() FROM outbox WHERE effect_key = $1 AND done_at IS NULL AND attempts < $2", string(key), maxAttempts).Scan(&leased)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect effect %s lease: %w", key, err)
	}
	if leased {
		return nil, fmt.Errorf("claim effect %s: %w", key, ports.ErrEffectLeased)
	}
	return nil, nil
}
