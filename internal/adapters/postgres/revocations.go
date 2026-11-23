package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

var _ ports.RevocationStore = (*Store)(nil)

// Revoke records a signed-out board session identifier until expiresAt. Repeating the same sid keeps the later expiry.
func (s *Store) Revoke(ctx context.Context, sid string, expiresAt time.Time) error {
	if sid == "" {
		return errors.New("revoke session: sid is empty")
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO revoked_sessions (sid, expires_at)
		VALUES ($1, $2)
		ON CONFLICT (sid) DO UPDATE SET expires_at = GREATEST(revoked_sessions.expires_at, EXCLUDED.expires_at)
	`, sid, expiresAt.UTC())
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

// IsRevoked reports whether sid is revoked and has not yet expired at now.
func (s *Store) IsRevoked(ctx context.Context, sid string, now time.Time) (bool, error) {
	if sid == "" {
		return false, errors.New("check revoked session: sid is empty")
	}
	var revoked bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM revoked_sessions WHERE sid = $1 AND expires_at > $2
		)
	`, sid, now.UTC()).Scan(&revoked)
	if err != nil {
		return false, fmt.Errorf("check revoked session: %w", err)
	}
	return revoked, nil
}

// Prune deletes revoked session rows whose original expiry is at or before now.
func (s *Store) Prune(ctx context.Context, now time.Time) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM revoked_sessions WHERE expires_at <= $1`, now.UTC())
	if err != nil {
		return fmt.Errorf("prune revoked sessions: %w", err)
	}
	return nil
}
