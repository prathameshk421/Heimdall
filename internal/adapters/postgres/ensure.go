package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

var _ ports.EffectEnsurer = (*Store)(nil)

// EnsureEffects implements ports.EffectEnsurer. Under the change's advisory lock it finds the newest event and inserts
// the effects with insertEffect, so a repeated key is a no-op and a key with other content is refused, exactly as in
// Append.
func (s *Store) EnsureEffects(ctx context.Context, changeID id.ChangeID, fx []change.Effect) error {
	if _, err := id.ParseChangeID(string(changeID)); err != nil {
		return fmt.Errorf("ensure effects of %s: %w", changeID, err)
	}
	for _, f := range fx {
		if err := f.Validate(); err != nil {
			return fmt.Errorf("ensure effects of %s: effect %s: %w", changeID, f.Key, err)
		}
	}
	if len(fx) == 0 {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("ensure effects of %s: begin: %w", changeID, err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", string(changeID)); err != nil {
		return fmt.Errorf("ensure effects of %s: lock: %w", changeID, err)
	}
	var causing string
	err = tx.QueryRow(ctx, "SELECT id FROM event WHERE change_id = $1 ORDER BY version DESC LIMIT 1", string(changeID)).Scan(&causing)
	if err != nil {
		return fmt.Errorf("ensure effects of %s: read newest event: %w", changeID, noEventsErr(err))
	}
	if err := s.insertEffects(ctx, tx, change.EventID(causing), fx); err != nil {
		return fmt.Errorf("ensure effects of %s: %w", changeID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("ensure effects of %s: commit: %w", changeID, err)
	}
	return nil
}

func noEventsErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("the change has no events")
	}
	return err
}
