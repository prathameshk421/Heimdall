package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The host job writes today's agent tokens to a file; the reconciler reads it with the cap (P4).
func TestUsageFromFileReadsTodaysTokens(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.json")
	now := func() time.Time { return time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC) }
	usage := usageFromFile(path, 1_500_000, now)

	_, _, err := usage(context.Background())
	require.Error(t, err, "no file yet: unreadable, which pauses new work")

	require.NoError(t, os.WriteFile(path, []byte(`{"date":"2026-11-22","tokens":1234567}`), 0o600))
	used, capTokens, err := usage(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(1_234_567), used)
	require.Equal(t, int64(1_500_000), capTokens)

	require.NoError(t, os.WriteFile(path, []byte(`{"date":"2026-11-21","tokens":9999999}`), 0o600))
	used, _, err = usage(context.Background())
	require.NoError(t, err)
	require.Zero(t, used, "yesterday's count is not today's usage")

	require.NoError(t, os.WriteFile(path, []byte(`{"date":"2026-11-22","tokens":-5}`), 0o600))
	_, _, err = usage(context.Background())
	require.Error(t, err, "a negative count is refused")
}
