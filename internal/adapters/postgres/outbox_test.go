//go:build component

package postgres

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

func TestOutbox(t *testing.T) {
	ctx := context.Background()
	srv := startServer(t)
	cid := id.ChangeID("PAY-142")

	// appendWithEffect commits one event at version and one effect keyed to it, in its own transaction, so each
	// effect gets its own created_at.
	appendWithEffect := func(t *testing.T, s *Store, version int64, disc string) change.Effect {
		t.Helper()
		fx := effect(t, cid, version, disc, `{"text":"`+disc+`"}`)
		_, err := s.Append(ctx, cid, version-1, roomEvents(cid, version-1, "room-"+disc), []change.Effect{fx})
		require.NoError(t, err)
		return fx
	}

	t.Run("pending is oldest first, honours the limit and skips done rows", func(t *testing.T) {
		s := NewStore(srv.database(ctx, t).pool)
		e1 := appendWithEffect(t, s, 1, "a")
		e2 := appendWithEffect(t, s, 2, "b")
		e3 := appendWithEffect(t, s, 3, "c")

		first, err := s.Pending(ctx, 2)
		require.NoError(t, err)
		require.Equal(t, []change.EffectKey{e1.Key, e2.Key}, keysOf(first))

		require.NoError(t, s.Done(ctx, e1.Key))
		rest, err := s.Pending(ctx, 10)
		require.NoError(t, err)
		require.Equal(t, []change.EffectKey{e2.Key, e3.Key}, keysOf(rest))
	})

	t.Run("done twice is not an error, and an unknown key is not found", func(t *testing.T) {
		s := NewStore(srv.database(ctx, t).pool)
		fx := appendWithEffect(t, s, 1, "d")

		require.NoError(t, s.Done(ctx, fx.Key))
		require.NoError(t, s.Done(ctx, fx.Key))
		require.Empty(t, pendingOf(ctx, t, s))
		err := s.Done(ctx, change.EffectKey("room_post:PAY-142:9:nope"))
		require.ErrorIs(t, err, ports.ErrNotFound)
	})

	t.Run("failures are counted; the eighth moves the effect to the dead letter", func(t *testing.T) {
		db := srv.database(ctx, t)
		s := NewStore(db.pool)
		fx := appendWithEffect(t, s, 1, "e")

		for i := 1; i <= 7; i++ {
			require.NoError(t, s.Fail(ctx, fx.Key, "boom"))
		}
		require.Len(t, pendingOf(ctx, t, s), 1, "seven failures leave the effect pending")
		require.Equal(t, int64(0), countRows(ctx, t, db.pool, "SELECT count(*) FROM outbox_dead"))

		require.NoError(t, s.Fail(ctx, fx.Key, "boom 8"))
		require.Empty(t, pendingOf(ctx, t, s), "eight failures take the effect out of Pending")
		require.Equal(t, int64(1), countRows(ctx, t, db.pool, "SELECT count(*) FROM outbox_dead"))
		require.Equal(t, int64(8), countRows(ctx, t, db.pool, "SELECT attempts FROM outbox_dead"))

		var lastErr string
		require.NoError(t, db.pool.QueryRow(ctx, "SELECT last_error FROM outbox_dead").Scan(&lastErr))
		require.Equal(t, "boom 8", lastErr)
	})

	t.Run("a failure for an unknown key is not found, and a late failure after done is refused", func(t *testing.T) {
		s := NewStore(srv.database(ctx, t).pool)
		fx := appendWithEffect(t, s, 1, "f")

		require.ErrorIs(t, s.Fail(ctx, change.EffectKey("room_post:PAY-142:9:nope"), "x"), ports.ErrNotFound)
		require.NoError(t, s.Done(ctx, fx.Key))
		err := s.Fail(ctx, fx.Key, "too late")
		require.Error(t, err)
		require.NotErrorIs(t, err, ports.ErrNotFound)
	})

	t.Run("outbox rows cannot be deleted, truncated or changed beyond done_at, attempts and last_error", func(t *testing.T) {
		db := srv.database(ctx, t)
		s := NewStore(db.pool)
		fx := appendWithEffect(t, s, 1, "g")

		// Each message is the guard's own, so a removed guard cannot pass the check through another error.
		const guard = "UPDATE on outbox may change only done_at, attempts and last_error"
		for sql, want := range map[string]string{
			"UPDATE outbox SET payload = '{}'":                        guard,
			"UPDATE outbox SET kind = 'github_check'":                 guard,
			"UPDATE outbox SET target = 'elsewhere'":                  guard,
			"UPDATE outbox SET effect_key = 'other'":                  guard,
			"UPDATE outbox SET created_at = now() + interval '1 day'": guard,
			"DELETE FROM outbox":                                      "DELETE on outbox is refused",
			"TRUNCATE outbox":                                         "TRUNCATE on outbox is refused",
		} {
			_, err := db.pool.Exec(ctx, sql)
			require.ErrorContains(t, err, want, sql)
		}
		require.Len(t, pendingOf(ctx, t, s), 1)
		require.Equal(t, fx.Key, pendingOf(ctx, t, s)[0].Key, "the rejected statements changed nothing")
	})
}

func keysOf(fx []change.Effect) []change.EffectKey {
	out := make([]change.EffectKey, 0, len(fx))
	for _, f := range fx {
		out = append(out, f.Key)
	}
	return out
}
