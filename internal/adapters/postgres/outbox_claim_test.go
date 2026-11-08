//go:build component

package postgres

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// appendOne commits one event at version and one effect keyed to it, in its own transaction, so each effect gets its
// own created_at.
func appendOne(ctx context.Context, t *testing.T, s *Store, version int64, disc string) change.Effect {
	t.Helper()
	cid := id.ChangeID("PAY-142")
	fx := effect(t, cid, version, disc, `{"text":"`+disc+`"}`)
	_, err := s.Append(ctx, cid, version-1, roomEvents(cid, version-1, "room-"+disc), []change.Effect{fx})
	require.NoError(t, err)
	return fx
}

// claimLong is a lease that no test in this file waits out.
const claimLong = time.Minute

func TestOutboxClaim(t *testing.T) {
	ctx := context.Background()
	srv := startServer(t)

	t.Run("claims oldest first, honours the limit, and a live lease hides the row", func(t *testing.T) {
		s := NewStore(srv.database(ctx, t).pool)
		e1 := appendOne(ctx, t, s, 1, "a")
		e2 := appendOne(ctx, t, s, 2, "b")
		e3 := appendOne(ctx, t, s, 3, "c")

		first, err := s.Claim(ctx, 2, claimLong)
		require.NoError(t, err)
		require.Equal(t, []change.EffectKey{e1.Key, e2.Key}, keysOf(first))

		rest, err := s.Claim(ctx, 10, claimLong)
		require.NoError(t, err)
		require.Equal(t, []change.EffectKey{e3.Key}, keysOf(rest), "the two leased rows are not offered again")

		none, err := s.Claim(ctx, 10, claimLong)
		require.NoError(t, err)
		require.Empty(t, none)
		require.Len(t, pendingOf(ctx, t, s), 3, "Pending still shows every undone effect for inspection")
	})

	t.Run("a claimed effect comes back with the same key, kind, target and payload as Pending", func(t *testing.T) {
		s := NewStore(srv.database(ctx, t).pool)
		fx := appendOne(ctx, t, s, 1, "same")

		want := pendingOf(ctx, t, s)
		got, err := s.Claim(ctx, 1, claimLong)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, want, got)
		require.Equal(t, fx.Key, got[0].Key)
	})

	t.Run("a non-positive limit or lease is refused before any row is touched", func(t *testing.T) {
		db := srv.database(ctx, t)
		s := NewStore(db.pool)
		fx := appendOne(ctx, t, s, 1, "bad")

		_, err := s.Claim(ctx, 0, claimLong)
		require.Error(t, err)
		_, err = s.Claim(ctx, 1, 0)
		require.Error(t, err)
		_, err = s.Claim(ctx, 1, 500*time.Nanosecond)
		require.Error(t, err, "a lease under one microsecond would expire at once")
		require.Equal(t, int64(0), countRows(ctx, t, db.pool, "SELECT count(*) FROM outbox WHERE claimed_until IS NOT NULL"))
		require.Len(t, pendingOf(ctx, t, s), 1)
		require.Equal(t, fx.Key, pendingOf(ctx, t, s)[0].Key)
	})
}

func TestOutboxLease(t *testing.T) {
	ctx := context.Background()
	srv := startServer(t)
	cid := id.ChangeID("PAY-142")

	t.Run("concurrent claims never return the same effect while its lease is live", func(t *testing.T) {
		s := NewStore(srv.database(ctx, t).pool)
		const n = 40
		rooms := make([]string, n)
		for i := range rooms {
			rooms[i] = fmt.Sprintf("bulk-room-%02d", i)
		}
		fx := make([]change.Effect, 0, n)
		for i := range n {
			fx = append(fx, effect(t, cid, n, fmt.Sprintf("bulk%02d", i), `{"n":`+strconv.Itoa(i)+`}`))
		}
		_, err := s.Append(ctx, cid, 0, roomEvents(cid, 0, rooms...), fx)
		require.NoError(t, err)

		const claimers = 8
		var mu sync.Mutex
		seen := map[change.EffectKey]int{}
		var wg sync.WaitGroup
		errs := make(chan error, claimers)
		for range claimers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					got, err := s.Claim(ctx, 5, claimLong)
					if err != nil {
						errs <- err
						return
					}
					if len(got) == 0 {
						return
					}
					mu.Lock()
					for _, g := range got {
						seen[g.Key]++
					}
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}
		require.Len(t, seen, n, "every effect was claimed")
		for key, count := range seen {
			require.Equal(t, 1, count, "effect %s was claimed %d times", key, count)
		}
	})

	t.Run("an expired lease makes the effect claimable again", func(t *testing.T) {
		s := NewStore(srv.database(ctx, t).pool)
		fx := appendOne(ctx, t, s, 1, "lease")

		first, err := s.Claim(ctx, 1, 200*time.Millisecond)
		require.NoError(t, err)
		require.Equal(t, []change.EffectKey{fx.Key}, keysOf(first))

		again, err := s.Claim(ctx, 1, 200*time.Millisecond)
		require.NoError(t, err)
		require.Empty(t, again, "the lease is live, so a second claim gets nothing")

		require.Eventually(t, func() bool {
			got, err := s.Claim(ctx, 1, 200*time.Millisecond)
			return err == nil && len(got) == 1 && got[0].Key == fx.Key
		}, 10*time.Second, 20*time.Millisecond, "the expired lease must hand the effect out again")
	})

	t.Run("done clears the lease and the row is never claimed again", func(t *testing.T) {
		db := srv.database(ctx, t)
		s := NewStore(db.pool)
		fx := appendOne(ctx, t, s, 1, "done")

		_, err := s.Claim(ctx, 1, claimLong)
		require.NoError(t, err)
		require.NoError(t, s.Done(ctx, fx.Key))
		require.Equal(t, int64(1), countRows(ctx, t, db.pool,
			"SELECT count(*) FROM outbox WHERE effect_key = $1 AND claimed_until IS NULL", string(fx.Key)))

		got, err := s.Claim(ctx, 10, time.Millisecond)
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("fail keeps the row pending and holds it back about 5 s, then it is claimable", func(t *testing.T) {
		db := srv.database(ctx, t)
		s := NewStore(db.pool)
		fx := appendOne(ctx, t, s, 1, "fail")

		_, err := s.Claim(ctx, 1, claimLong)
		require.NoError(t, err)
		require.NoError(t, s.Fail(ctx, fx.Key, "upstream 503"))
		require.Equal(t, int64(1), countRows(ctx, t, db.pool,
			`SELECT count(*) FROM outbox WHERE effect_key = $1
				AND claimed_until BETWEEN now() + interval '3900 milliseconds' AND now() + interval '6100 milliseconds'`,
			string(fx.Key)), "the first retry waits 5 s, plus or minus 20 percent")

		early, err := s.Claim(ctx, 1, claimLong)
		require.NoError(t, err)
		require.Empty(t, early, "a failed effect is not retried before its delay")

		expireLease(ctx, t, db.pool, fx.Key)
		got, err := s.Claim(ctx, 1, claimLong)
		require.NoError(t, err)
		require.Equal(t, []change.EffectKey{fx.Key}, keysOf(got), "a failed effect is retried once its delay has passed")
		require.Equal(t, int64(1), countRows(ctx, t, db.pool, "SELECT attempts FROM outbox WHERE effect_key = $1", string(fx.Key)))
	})

	t.Run("rows locked by another transaction are skipped, not waited for", func(t *testing.T) {
		db := srv.database(ctx, t)
		s := NewStore(db.pool)
		locked := appendOne(ctx, t, s, 1, "locked")
		free := appendOne(ctx, t, s, 2, "free")

		holder, err := db.pool.Begin(ctx)
		require.NoError(t, err)
		t.Cleanup(func() { _ = holder.Rollback(context.WithoutCancel(ctx)) })
		_, err = holder.Exec(ctx, "SELECT 1 FROM outbox WHERE effect_key = $1 FOR UPDATE", string(locked.Key))
		require.NoError(t, err)

		// A claim that waited on the locked row would run out of this deadline instead of returning the other row.
		bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		got, err := s.Claim(bounded, 10, claimLong)
		require.NoError(t, err, "the claim waited on the locked row")
		require.Equal(t, []change.EffectKey{free.Key}, keysOf(got))
	})

	t.Run("the retry delay doubles and never passes 10 minutes", func(t *testing.T) {
		db := srv.database(ctx, t)
		s := NewStore(db.pool)
		fx := appendOne(ctx, t, s, 1, "cap")

		_, err := db.pool.Exec(ctx, "UPDATE outbox SET attempts = 2 WHERE effect_key = $1", string(fx.Key))
		require.NoError(t, err)
		require.NoError(t, s.Fail(ctx, fx.Key, "third failure"))
		require.Equal(t, int64(1), countRows(ctx, t, db.pool,
			`SELECT count(*) FROM outbox WHERE effect_key = $1
				AND claimed_until BETWEEN now() + interval '15900 milliseconds' AND now() + interval '24100 milliseconds'`,
			string(fx.Key)), "after two earlier failures the delay is 20 s, plus or minus 20 percent")

		_, err = db.pool.Exec(ctx, "UPDATE outbox SET attempts = 6 WHERE effect_key = $1", string(fx.Key))
		require.NoError(t, err)
		require.NoError(t, s.Fail(ctx, fx.Key, "seventh failure"))
		require.Equal(t, int64(1), countRows(ctx, t, db.pool,
			`SELECT count(*) FROM outbox WHERE effect_key = $1
				AND claimed_until BETWEEN now() + interval '256 seconds' AND now() + interval '10 minutes'`,
			string(fx.Key)), "5 s doubled six times is 320 s, plus or minus 20 percent, under the cap")

		_, err = db.pool.Exec(ctx, "UPDATE outbox SET attempts = 20 WHERE effect_key = $1", string(fx.Key))
		require.NoError(t, err)
		require.NoError(t, s.Fail(ctx, fx.Key, "late failure"))
		require.Equal(t, int64(1), countRows(ctx, t, db.pool,
			`SELECT count(*) FROM outbox WHERE effect_key = $1
				AND claimed_until BETWEEN now() + interval '599 seconds' AND now() + interval '10 minutes'`,
			string(fx.Key)), "the delay stops at 10 minutes")
	})

	t.Run("a failure for an unknown effect is not found", func(t *testing.T) {
		s := NewStore(srv.database(ctx, t).pool)
		require.ErrorIs(t, s.Fail(ctx, change.EffectKey("room_post:PAY-142:9:none"), "x"), ports.ErrNotFound)
	})
}

// expireLease moves the lease of key into the past, as if its drainer had died. It uses the database clock, as Claim
// does, so the test does not wait for a lease to run out.
func expireLease(ctx context.Context, t *testing.T, pool *pgxpool.Pool, key change.EffectKey) {
	t.Helper()
	tag, err := pool.Exec(ctx, "UPDATE outbox SET claimed_until = now() - interval '1 second' WHERE effect_key = $1", string(key))
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected())
}

func TestOutboxStart(t *testing.T) {
	ctx := context.Background()
	srv := startServer(t)

	t.Run("a started row is not claimed again after its lease runs out, and is listed as in doubt", func(t *testing.T) {
		db := srv.database(ctx, t)
		s := NewStore(db.pool)
		fx := appendOne(ctx, t, s, 1, "started")

		_, err := s.Claim(ctx, 1, claimLong)
		require.NoError(t, err)
		require.NoError(t, s.Start(ctx, fx.Key))
		require.Equal(t, int64(0), countRows(ctx, t, db.pool, "SELECT count(*) FROM outbox_in_doubt"), "a live lease is not in doubt")

		expireLease(ctx, t, db.pool, fx.Key)
		require.Equal(t, int64(1), countRows(ctx, t, db.pool, "SELECT count(*) FROM outbox_in_doubt"))
		got, err := s.Claim(ctx, 10, claimLong)
		require.NoError(t, err)
		require.Empty(t, got, "an effect that may have run is never handed out again")
	})

	t.Run("start needs a live lease, so a drainer that lost its lease cannot start the row", func(t *testing.T) {
		db := srv.database(ctx, t)
		s := NewStore(db.pool)
		fx := appendOne(ctx, t, s, 1, "lost")

		_, err := s.Claim(ctx, 1, claimLong)
		require.NoError(t, err)
		expireLease(ctx, t, db.pool, fx.Key)
		require.Error(t, s.Start(ctx, fx.Key))
		require.Equal(t, int64(0), countRows(ctx, t, db.pool,
			"SELECT count(*) FROM outbox WHERE effect_key = $1 AND started_at IS NOT NULL", string(fx.Key)))
	})

	t.Run("start refuses an unclaimed effect and a second start", func(t *testing.T) {
		db := srv.database(ctx, t)
		s := NewStore(db.pool)
		fx := appendOne(ctx, t, s, 1, "twice")

		require.Error(t, s.Start(ctx, fx.Key), "an effect nobody claimed cannot start")
		_, err := s.Claim(ctx, 1, claimLong)
		require.NoError(t, err)
		require.NoError(t, s.Start(ctx, fx.Key))
		require.Error(t, s.Start(ctx, fx.Key), "a row starts once")
	})

	t.Run("start refuses a done effect and an unknown key", func(t *testing.T) {
		db := srv.database(ctx, t)
		s := NewStore(db.pool)
		fx := appendOne(ctx, t, s, 1, "done")

		_, err := s.Claim(ctx, 1, claimLong)
		require.NoError(t, err)
		require.NoError(t, s.Done(ctx, fx.Key))
		require.Error(t, s.Start(ctx, fx.Key))
		require.ErrorIs(t, s.Start(ctx, "no:such:effect"), ports.ErrNotFound)
	})

	t.Run("fail after start clears the mark, so the effect is claimable once its delay passes", func(t *testing.T) {
		db := srv.database(ctx, t)
		s := NewStore(db.pool)
		fx := appendOne(ctx, t, s, 1, "retry")

		_, err := s.Claim(ctx, 1, claimLong)
		require.NoError(t, err)
		require.NoError(t, s.Start(ctx, fx.Key))
		require.NoError(t, s.Fail(ctx, fx.Key, "upstream 503"))
		require.Equal(t, int64(0), countRows(ctx, t, db.pool, "SELECT count(*) FROM outbox_in_doubt"),
			"a reported failure is not in doubt")

		expireLease(ctx, t, db.pool, fx.Key)
		got, err := s.Claim(ctx, 1, claimLong)
		require.NoError(t, err)
		require.Equal(t, []change.EffectKey{fx.Key}, keysOf(got))
	})

	t.Run("ClaimEffect never returns an in-doubt row", func(t *testing.T) {
		db := srv.database(ctx, t)
		s := NewStore(db.pool)
		fx := appendOne(ctx, t, s, 1, "keyed")

		_, err := s.Claim(ctx, 1, claimLong)
		require.NoError(t, err)
		require.NoError(t, s.Start(ctx, fx.Key))
		expireLease(ctx, t, db.pool, fx.Key)

		got, err := s.ClaimEffect(ctx, fx.Key, claimLong)
		require.NoError(t, err)
		require.Nil(t, got, "a started effect whose lease ran out is in doubt and is not claimed by key either")
	})
}

func TestOutboxDeadLetter(t *testing.T) {
	ctx := context.Background()
	srv := startServer(t)
	db := srv.database(ctx, t)
	s := NewStore(db.pool)
	fx := appendOne(ctx, t, s, 1, "dead")

	// Seven failed claims: each attempt is claimable once its delay passes, and the effect stays pending.
	for i := 1; i <= 7; i++ {
		if i > 1 {
			expireLease(ctx, t, db.pool, fx.Key)
		}
		got, err := s.Claim(ctx, 1, claimLong)
		require.NoError(t, err)
		require.Equal(t, []change.EffectKey{fx.Key}, keysOf(got), "attempt %d is still claimable", i)
		require.NoError(t, s.Fail(ctx, fx.Key, "boom"))
	}
	require.Len(t, pendingOf(ctx, t, s), 1, "seven failures leave the effect pending")

	// The eighth failure dead-letters it, and Claim never returns it again.
	expireLease(ctx, t, db.pool, fx.Key)
	got, err := s.Claim(ctx, 1, claimLong)
	require.NoError(t, err)
	require.Equal(t, []change.EffectKey{fx.Key}, keysOf(got))
	require.NoError(t, s.Fail(ctx, fx.Key, "boom 8"))

	again, err := s.Claim(ctx, 10, claimLong)
	require.NoError(t, err)
	require.Empty(t, again, "eight failures take the effect out of Claim")
	require.Equal(t, int64(1), countRows(ctx, t, db.pool, "SELECT count(*) FROM outbox_dead"))
}
