//go:build component

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/stretchr/testify/require"
)

func TestReviewPackCatalogPersistsVersionsAndRevokesCiphertextReferences(t *testing.T) {
	ctx := context.Background()
	srv := startServer(t)
	db := srv.database(ctx, t)
	store := NewStore(db.pool)
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	one := workflow.BlobRef("blob:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	two := workflow.BlobRef("blob:sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	require.NoError(t, store.SavePack(ctx, "alice", 1, one, []byte(`{"person":"alice","version":1}`), now))
	require.NoError(t, store.SavePack(ctx, "alice", 2, two, []byte(`{"person":"alice","version":2}`), now.Add(time.Hour)))
	require.NoError(t, store.SavePack(ctx, "bob", 1, one, []byte(`{"person":"bob","version":1}`), now))
	require.NoError(t, store.SavePack(ctx, "alice", 2, two, []byte(`{"person":"alice","version":2}`), now.Add(time.Hour)), "same version replay is idempotent")
	require.Error(t, store.SavePack(ctx, "alice", 2, one, []byte(`{"person":"alice","version":2}`), now.Add(time.Hour)), "version content is immutable")

	latest, err := store.LatestPack(ctx, "alice")
	require.NoError(t, err)
	require.Equal(t, 2, latest.Version)
	require.Equal(t, two, latest.Blob)
	_, err = store.LatestPack(ctx, "carol")
	require.ErrorIs(t, err, ports.ErrNotFound)

	refs, err := store.DeletePacks(ctx, "alice")
	require.NoError(t, err)
	require.Equal(t, []workflow.BlobRef{two}, refs, "blob still referenced by Bob must not be eligible for deletion")
	refs, err = store.DeletePacks(ctx, "bob")
	require.NoError(t, err)
	require.Equal(t, []workflow.BlobRef{one}, refs)
}
