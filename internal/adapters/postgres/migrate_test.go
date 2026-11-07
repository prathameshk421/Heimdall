//go:build component

package postgres

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigrate(t *testing.T) {
	ctx := context.Background()
	srv := startServer(t)

	t.Run("migrations create the event and knowledge tables and dead-letter view", func(t *testing.T) {
		db := srv.database(ctx, t)
		for _, name := range []string{"event", "outbox", "webhook_delivery", "cursor", "knowledge_file", "review_pack", "memory_episode_queue", "outbox_dead"} {
			require.Equal(t, int64(1), countRows(ctx, t, db.pool,
				"SELECT count(*) FROM pg_class WHERE relname = $1", name), name)
		}
	})

	t.Run("the version uniqueness constraint has the name the store translates", func(t *testing.T) {
		db := srv.database(ctx, t)
		require.Equal(t, int64(1), countRows(ctx, t, db.pool,
			"SELECT count(*) FROM pg_constraint WHERE conname = 'event_change_id_version_key' AND contype = 'u'"))
	})

	t.Run("applying the migrations again changes nothing", func(t *testing.T) {
		db := srv.database(ctx, t)
		require.NoError(t, Migrate(ctx, db.pool))
		require.Equal(t, int64(1), countRows(ctx, t, db.pool,
			"SELECT count(*) FROM pg_class WHERE relname = 'event' AND relkind = 'r'"))
	})
}
