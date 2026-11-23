package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInMemoryRevocationsAreBoundedAndPruneExpired(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	r := newRevocations(func() time.Time { return now }, 3)

	require.NoError(t, r.Revoke(ctx, "s1", now.Add(5*time.Minute)))
	require.NoError(t, r.Revoke(ctx, "s2", now.Add(10*time.Minute)))
	require.NoError(t, r.Revoke(ctx, "s3", now.Add(15*time.Minute)))

	// Adding a 4th while all 3 are unexpired evicts the oldest ("s1").
	require.NoError(t, r.Revoke(ctx, "s4", now.Add(20*time.Minute)))
	require.Len(t, r.entries, 3)

	got, err := r.IsRevoked(ctx, "s1", now)
	require.NoError(t, err)
	require.False(t, got, "oldest entry evicted at cap")
	for _, id := range []string{"s2", "s3", "s4"} {
		got, err = r.IsRevoked(ctx, id, now)
		require.NoError(t, err)
		require.True(t, got, id)
	}

	// Advancing time past s2 and s3 prunes them on IsRevoked.
	now = now.Add(16 * time.Minute)
	got, err = r.IsRevoked(ctx, "s2", now)
	require.NoError(t, err)
	require.False(t, got)
	require.Len(t, r.entries, 1)

	got, err = r.IsRevoked(ctx, "s4", now)
	require.NoError(t, err)
	require.True(t, got)
}
