//go:build component

package postgres

import (
	"context"
	"io/fs"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// migrateOnly applies the first migration alone, so a test can write rows in the schema that existed before T22.
func migrateOnly(ctx context.Context, pool *pgxpool.Pool, name string) error {
	sub, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		return err
	}
	data, err := fs.ReadFile(sub, name)
	if err != nil {
		return err
	}
	only := fstest.MapFS{name: &fstest.MapFile{Data: data}}
	db := stdlib.OpenDBFromPool(pool)
	defer func() { _ = db.Close() }()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, only)
	if err != nil {
		return err
	}
	_, err = provider.Up(ctx)
	return err
}

func TestMigrateOutboxLease(t *testing.T) {
	ctx := context.Background()
	srv := startServer(t)

	t.Run("migration 0002 applies on top of rows written under 0001 and keeps them", func(t *testing.T) {
		db := srv.emptyDatabase(ctx, t)
		require.NoError(t, migrateOnly(ctx, db.pool, "0001_init.sql"))
		require.Equal(t, int64(0), countRows(ctx, t, db.pool,
			"SELECT count(*) FROM information_schema.columns WHERE table_name = 'outbox' AND column_name = 'claimed_until'"),
			"the column is not there before 0002")

		cid := id.ChangeID("PAY-142")
		s := NewStore(db.pool)
		fx := effect(t, cid, 1, "pre-0002", `{"text":"written before the lease"}`)
		_, err := s.Append(ctx, cid, 0, roomEvents(cid, 0, "room-old"), []change.Effect{fx})
		require.NoError(t, err)

		require.NoError(t, Migrate(ctx, db.pool))
		require.Equal(t, int64(1), countRows(ctx, t, db.pool,
			"SELECT count(*) FROM information_schema.columns WHERE table_name = 'outbox' AND column_name = 'claimed_until'"))
		require.Equal(t, int64(1), countRows(ctx, t, db.pool, "SELECT count(*) FROM outbox WHERE claimed_until IS NULL"),
			"the old row survives with no lease")

		got, err := s.Claim(ctx, 10, time.Second)
		require.NoError(t, err)
		require.Equal(t, []change.EffectKey{fx.Key}, keysOf(got), "the old effect is claimable under the new schema")
	})

	t.Run("the guard refuses changes to outbox columns that are not on its list, even after 0002", func(t *testing.T) {
		db := srv.database(ctx, t)
		s := NewStore(db.pool)
		fx := effect(t, id.ChangeID("PAY-142"), 1, "guard", `{"text":"guard"}`)
		_, err := s.Append(ctx, id.ChangeID("PAY-142"), 0, roomEvents(id.ChangeID("PAY-142"), 0, "room-g"), []change.Effect{fx})
		require.NoError(t, err)

		_, err = db.pool.Exec(ctx, "UPDATE outbox SET claimed_until = now() + interval '1 minute'")
		require.NoError(t, err, "claimed_until is the one new column the guard allows")
		_, err = db.pool.Exec(ctx, "UPDATE outbox SET payload = '{}'")
		require.ErrorContains(t, err, "may change only done_at, attempts, last_error and claimed_until")
	})
}
