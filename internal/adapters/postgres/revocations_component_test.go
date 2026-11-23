//go:build component

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRevocationStorePersistsAcrossRestartAndPrunesExpired(t *testing.T) {
	ctx := context.Background()
	srv := startServer(t)
	db := srv.database(ctx, t)
	t0 := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

	first := NewStore(db.pool)
	require.NoError(t, first.Revoke(ctx, "sid-1", t0.Add(time.Hour)))
	require.NoError(t, first.Revoke(ctx, "sid-1", t0.Add(30*time.Minute)), "re-revoking is idempotent")
	require.NoError(t, first.Revoke(ctx, "sid-short", t0.Add(5*time.Minute)))

	// A fresh Store models a server restart against the same database.
	restarted := NewStore(db.pool)
	revoked, err := restarted.IsRevoked(ctx, "sid-1", t0.Add(45*time.Minute))
	require.NoError(t, err)
	require.True(t, revoked, "earlier re-revoke must not shorten expiry")

	revoked, err = restarted.IsRevoked(ctx, "sid-short", t0.Add(5*time.Minute))
	require.NoError(t, err)
	require.False(t, revoked, "expired revocation is not active")

	require.NoError(t, restarted.Prune(ctx, t0.Add(10*time.Minute)))
	require.Equal(t, int64(1), countRows(ctx, t, db.pool, "SELECT count(*) FROM revoked_sessions"))

	require.Error(t, restarted.Revoke(ctx, "", t0.Add(time.Hour)))
	_, err = restarted.IsRevoked(ctx, "", t0)
	require.Error(t, err)
}
