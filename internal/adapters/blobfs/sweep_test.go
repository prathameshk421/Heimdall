package blobfs_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/blobfs"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/stretchr/testify/require"
)

func TestSweepExpiredDeletesOldLogsAndKeepsFreshAndEvidence(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := blobfs.New(dir)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	oldLog, err := store.Put(ctx, ports.BlobLog, strings.NewReader("old-log"))
	require.NoError(t, err)
	freshLog, err := store.Put(ctx, ports.BlobLog, strings.NewReader("fresh-log"))
	require.NoError(t, err)
	oldEvidence, err := store.Put(ctx, ports.BlobEvidence, strings.NewReader("old-evidence"))
	require.NoError(t, err)

	now := time.Now().UTC()
	oldTime := now.Add(-100 * 24 * time.Hour)
	for _, item := range []struct {
		class ports.BlobClass
		hash  string
	}{
		{ports.BlobLog, strings.TrimPrefix(string(oldLog), "blob:sha256:")},
		{ports.BlobEvidence, strings.TrimPrefix(string(oldEvidence), "blob:sha256:")},
	} {
		p := filepath.Join(dir, string(item.class), "sha256", item.hash[:2], item.hash[2:4], item.hash)
		require.NoError(t, os.Chtimes(p, oldTime, oldTime))
	}

	deleted, err := store.SweepExpired(ctx, now, nil)
	require.NoError(t, err)
	require.Equal(t, 1, deleted)

	_, err = store.Get(ctx, oldLog)
	require.ErrorIs(t, err, ports.ErrNotFound, "expired log blob must be deleted")

	rc, err := store.Get(ctx, freshLog)
	require.NoError(t, err)
	_ = rc.Close()

	rc, err = store.Get(ctx, oldEvidence)
	require.NoError(t, err)
	_ = rc.Close()
}
