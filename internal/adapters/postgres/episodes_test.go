//go:build component

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/stretchr/testify/require"
)

func TestMemoryEpisodeQueuePersistsTrustMetadataAndRetriesUntilDelivered(t *testing.T) {
	ctx := context.Background()
	srv := startServer(t)
	db := srv.database(ctx, t)
	store := NewStore(db.pool)
	episode := ports.Episode{Key: change.EffectKey("episode:decision:PAY-142:1"), Source: "decision", Status: ports.EpisodeConfirmed, GroupID: "payments", Body: `{"claim":"refunds are idempotent"}`}
	require.NoError(t, store.EnqueueEpisode(ctx, episode))
	require.NoError(t, store.EnqueueEpisode(ctx, episode), "replayed source events are idempotent")
	changed := episode
	changed.Status = ports.EpisodeObserved
	require.Error(t, store.EnqueueEpisode(ctx, changed), "source status is immutable for an episode key")

	pending, err := store.PendingEpisodes(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, []ports.Episode{episode}, pending)
	at := time.Date(2026, 11, 6, 1, 0, 0, 0, time.UTC)
	require.NoError(t, store.MarkEpisodeDelivered(ctx, episode.Key, at))
	require.NoError(t, store.MarkEpisodeDelivered(ctx, episode.Key, at.Add(time.Hour)), "delivery acknowledgement is idempotent")
	pending, err = store.PendingEpisodes(ctx, 10)
	require.NoError(t, err)
	require.Empty(t, pending)
	delivered, err := store.EpisodeDeliveryTime(ctx, episode.Key)
	require.NoError(t, err)
	require.True(t, at.Equal(delivered), "first successful delivery timestamp is preserved")
	require.NoError(t, store.InvalidateEpisode(ctx, episode.Key, at.Add(2*time.Hour)))
	require.NoError(t, store.InvalidateEpisode(ctx, episode.Key, at.Add(3*time.Hour)), "invalidation is idempotent")
	require.Error(t, store.EnqueueEpisode(ctx, episode), "a tombstoned source key cannot be replayed")
}
