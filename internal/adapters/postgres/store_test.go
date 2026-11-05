//go:build component

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

func TestAppend(t *testing.T) {
	ctx := context.Background()
	srv := startServer(t)
	cid := id.ChangeID("PAY-142")

	t.Run("happy path writes events, a hash chain and outbox rows", func(t *testing.T) {
		s := NewStore(srv.database(ctx, t).pool)
		evs := roomEvents(cid, 0, "room-1", "room-2")
		fx := effect(t, cid, 2, "msg-1", `{"text":"hi"}`)

		n, err := s.Append(ctx, cid, 0, evs, []change.Effect{fx})
		require.NoError(t, err)
		require.Equal(t, int64(2), n)
		require.Equal(t, change.Hash{}, evs[0].Hash, "Append must not write into the caller's events")

		loaded, err := s.Load(ctx, cid)
		require.NoError(t, err)
		require.Len(t, loaded, 2)
		require.Equal(t, int64(1), loaded[0].Version)
		require.Equal(t, int64(2), loaded[1].Version)
		require.Nil(t, loaded[0].PrevHash, "the genesis event has no previous hash")
		require.NotNil(t, loaded[1].PrevHash)
		require.Equal(t, loaded[0].Hash, *loaded[1].PrevHash)
		require.NoError(t, change.VerifyChain(loaded))

		pending, err := s.Pending(ctx, 10)
		require.NoError(t, err)
		require.Len(t, pending, 1)
		require.Equal(t, fx.Key, pending[0].Key)
		require.Equal(t, fx.Kind, pending[0].Kind)
		require.Equal(t, fx.Target, pending[0].Target)
		require.JSONEq(t, string(fx.Payload), string(pending[0].Payload))
	})

	t.Run("a stale expected version is a conflict and writes nothing", func(t *testing.T) {
		s := NewStore(srv.database(ctx, t).pool)
		_, err := s.Append(ctx, cid, 0, roomEvents(cid, 0, "room-1"), nil)
		require.NoError(t, err)

		stale := effect(t, cid, 1, "msg-2", `{"text":"late"}`)
		_, err = s.Append(ctx, cid, 0, roomEvents(cid, 0, "room-2"), []change.Effect{stale})
		require.ErrorIs(t, err, ports.ErrVersionConflict)

		loaded, err := s.Load(ctx, cid)
		require.NoError(t, err)
		require.Len(t, loaded, 1)
		require.Empty(t, pendingOf(ctx, t, s), "a conflicting append commits no outbox row")
	})

	t.Run("an expected version ahead of the change is a conflict", func(t *testing.T) {
		s := NewStore(srv.database(ctx, t).pool)
		_, err := s.Append(ctx, cid, 0, roomEvents(cid, 0, "room-1"), nil)
		require.NoError(t, err)

		_, err = s.Append(ctx, cid, 3, roomEvents(cid, 3, "room-2"), nil)
		require.ErrorIs(t, err, ports.ErrVersionConflict)
		loaded, err := s.Load(ctx, cid)
		require.NoError(t, err)
		require.Len(t, loaded, 1, "a gap must not be written")
	})

	t.Run("concurrent appends at the same expected version: exactly one wins", func(t *testing.T) {
		s := NewStore(srv.database(ctx, t).pool)
		const writers = 8
		errs := make(chan error, writers)
		var wg sync.WaitGroup
		for i := range writers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := s.Append(ctx, cid, 0, roomEvents(cid, 0, fmt.Sprintf("room-%d", i)), nil)
				errs <- err
			}()
		}
		wg.Wait()
		close(errs)

		var won, conflicts int
		for err := range errs {
			switch {
			case err == nil:
				won++
			case errors.Is(err, ports.ErrVersionConflict):
				conflicts++
			default:
				t.Errorf("unexpected error from a racing append: %v", err)
			}
		}
		require.Equal(t, 1, won)
		require.Equal(t, writers-1, conflicts)
		loaded, err := s.Load(ctx, cid)
		require.NoError(t, err)
		require.Len(t, loaded, 1)
		require.NoError(t, change.VerifyChain(loaded))
	})

	t.Run("a failure in the middle of the batch leaves nothing", func(t *testing.T) {
		s := NewStore(srv.database(ctx, t).pool)
		good := roomEvent(cid, 1, "room-1")
		second := roomEvent(cid, 2, "room-2")
		second.ID = good.ID // the first insert succeeds inside the transaction, the second hits event_id_key
		fx := effect(t, cid, 2, "msg-3", `{"text":"half"}`)

		_, err := s.Append(ctx, cid, 0, []change.Event{good, second}, []change.Effect{fx})
		require.Error(t, err)
		require.NotErrorIs(t, err, ports.ErrVersionConflict)

		loaded, err := s.Load(ctx, cid)
		require.NoError(t, err)
		require.Empty(t, loaded)
		require.Empty(t, pendingOf(ctx, t, s))
		_, err = s.Append(ctx, cid, 0, roomEvents(cid, 0, "room-3"), nil)
		require.NoError(t, err, "the change is still at version 0 after the rollback")
	})

	t.Run("an append whose backend is killed while it waits for the change lock leaves nothing", func(t *testing.T) {
		db := srv.database(ctx, t)
		s := NewStore(db.pool)

		// A superuser holds the change's lock, so the append blocks on it. Then its backend is terminated, which is
		// what a crash of the writer looks like to the database.
		holder := connectSuperuser(ctx, t, db.dsn)
		tx, err := holder.Begin(ctx)
		require.NoError(t, err)
		t.Cleanup(func() { _ = tx.Rollback(context.WithoutCancel(ctx)) })
		_, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", string(cid))
		require.NoError(t, err)

		evs := roomEvents(cid, 0, "room-1")
		fx := []change.Effect{effect(t, cid, 1, "killed", `{"text":"never"}`)}
		done := make(chan error, 1)
		go func() {
			_, err := s.Append(ctx, cid, 0, evs, fx)
			done <- err
		}()

		var pid int
		require.Eventually(t, func() bool {
			return holder.QueryRow(ctx, `SELECT pid FROM pg_stat_activity
				WHERE wait_event_type = 'Lock' AND query LIKE 'SELECT pg_advisory_xact_lock%'`).Scan(&pid) == nil
		}, 10*time.Second, 10*time.Millisecond, "the append never reached the change lock")
		_, err = holder.Exec(ctx, "SELECT pg_terminate_backend($1)", pid)
		require.NoError(t, err)
		require.Error(t, <-done, "the killed append must report failure")
		require.NoError(t, tx.Rollback(ctx))

		loaded, err := s.Load(ctx, cid)
		require.NoError(t, err)
		require.Empty(t, loaded)
		require.Empty(t, pendingOf(ctx, t, s))
		_, err = s.Append(ctx, cid, 0, roomEvents(cid, 0, "room-2"), nil)
		require.NoError(t, err, "the change is still at version 0 after the kill")
	})

	t.Run("an invalid event is refused before anything is written", func(t *testing.T) {
		s := NewStore(srv.database(ctx, t).pool)
		bad := roomEvents(cid, 0, "room-1", "room-2")
		bad[1].Type = change.EventType("not.a.type")

		_, err := s.Append(ctx, cid, 0, bad, nil)
		require.Error(t, err)
		loaded, err := s.Load(ctx, cid)
		require.NoError(t, err)
		require.Empty(t, loaded)
	})

	t.Run("versions must run consecutively from expected plus one", func(t *testing.T) {
		s := NewStore(srv.database(ctx, t).pool)
		gap := roomEvents(cid, 0, "room-1", "room-2")
		gap[1].Version = 3

		_, err := s.Append(ctx, cid, 0, gap, nil)
		require.Error(t, err)
		require.NotErrorIs(t, err, ports.ErrVersionConflict)
		loaded, err := s.Load(ctx, cid)
		require.NoError(t, err)
		require.Empty(t, loaded)
	})

	t.Run("an effect needs a causing event and an empty append is refused", func(t *testing.T) {
		s := NewStore(srv.database(ctx, t).pool)
		fx := effect(t, cid, 1, "msg-4", `{"text":"orphan"}`)

		_, err := s.Append(ctx, cid, 0, nil, []change.Effect{fx})
		require.Error(t, err)
		_, err = s.Append(ctx, cid, 0, nil, nil)
		require.Error(t, err)
		require.Empty(t, pendingOf(ctx, t, s))
	})

	t.Run("the same effect key with the same content is a no-op, with other content it is refused", func(t *testing.T) {
		s := NewStore(srv.database(ctx, t).pool)
		fx := effect(t, cid, 1, "msg-5", `{"text":"hi"}`)
		_, err := s.Append(ctx, cid, 0, roomEvents(cid, 0, "room-1"), []change.Effect{fx})
		require.NoError(t, err)

		n, err := s.Append(ctx, cid, 1, roomEvents(cid, 1, "room-2"), []change.Effect{fx})
		require.NoError(t, err)
		require.Equal(t, int64(2), n)
		require.Len(t, pendingOf(ctx, t, s), 1, "re-appending the same effect key adds no row")

		other := fx
		other.Payload = json.RawMessage(`{"text":"changed"}`)
		_, err = s.Append(ctx, cid, 2, roomEvents(cid, 2, "room-3"), []change.Effect{other})
		require.Error(t, err)
		require.NotErrorIs(t, err, ports.ErrVersionConflict)
		loaded, err := s.Load(ctx, cid)
		require.NoError(t, err)
		require.Len(t, loaded, 2, "the refused append wrote no event")
	})

	t.Run("loaded events verify although JSONB reorders keys, and keep microseconds", func(t *testing.T) {
		s := NewStore(srv.database(ctx, t).pool)
		ev := change.Event{
			ID:            newEventID(),
			ChangeID:      cid,
			Version:       1,
			Type:          change.EventChangeTriggered,
			Actor:         change.Actor{Kind: change.ActorJira, ID: "jira"},
			OccurredAt:    base.Add(123456789), // nanoseconds: the store keeps microseconds, and hashes what it keeps
			CorrelationID: "corr-1",
			Payload:       json.RawMessage(`{ "ticket": "PAY-142", "repo": "acme/pay-demo", "assignee": "bob", "source": "jira" }`),
		}
		_, err := s.Append(ctx, cid, 0, []change.Event{ev}, nil)
		require.NoError(t, err)

		loaded, err := s.Load(ctx, cid)
		require.NoError(t, err)
		require.Len(t, loaded, 1)
		require.NoError(t, change.VerifyChain(loaded))
		require.JSONEq(t, string(ev.Payload), string(loaded[0].Payload))
		require.True(t, base.Add(123456000).Equal(loaded[0].OccurredAt), "got %s", loaded[0].OccurredAt)
		require.Equal(t, ev.ID, loaded[0].ID)
		require.Equal(t, ev.Actor, loaded[0].Actor)
	})

	t.Run("event rows cannot be updated, deleted or truncated", func(t *testing.T) {
		db := srv.database(ctx, t)
		s := NewStore(db.pool)
		_, err := s.Append(ctx, cid, 0, roomEvents(cid, 0, "room-1"), nil)
		require.NoError(t, err)

		// The message is the trigger's own. Postgres refuses TRUNCATE event for its foreign key from outbox before
		// any trigger runs, so a check on "any error" would pass with the trigger removed.
		for sql, want := range map[string]string{
			"UPDATE event SET type = 'room.opened' WHERE change_id = 'PAY-142'": "UPDATE on event is refused",
			"UPDATE event SET payload = '{}' WHERE change_id = 'PAY-142'":       "UPDATE on event is refused",
			"DELETE FROM event WHERE change_id = 'PAY-142'":                     "DELETE on event is refused",
		} {
			_, err := db.pool.Exec(ctx, sql)
			require.ErrorContains(t, err, want, sql)
		}
		// Postgres refuses TRUNCATE event alone because outbox references it, before any trigger runs. To test the
		// event trigger by itself, a superuser disables the outbox trigger for this database and both tables are
		// truncated together.
		su := connectSuperuser(ctx, t, db.dsn)
		_, err = su.Exec(ctx, "ALTER TABLE outbox DISABLE TRIGGER outbox_refuse_truncate")
		require.NoError(t, err)
		_, err = db.pool.Exec(ctx, "TRUNCATE event, outbox")
		require.ErrorContains(t, err, "TRUNCATE on event is refused")
		loaded, err := s.Load(ctx, cid)
		require.NoError(t, err)
		require.Len(t, loaded, 1, "the rejected statements changed nothing")
	})
}

// pendingOf returns the effects that are not done yet.
func pendingOf(ctx context.Context, t *testing.T, s *Store) []change.Effect {
	t.Helper()
	fx, err := s.Pending(ctx, 1000)
	require.NoError(t, err)
	return fx
}

// connectSuperuser opens a session of the container's superuser, which may bypass triggers.
func connectSuperuser(ctx context.Context, t *testing.T, dsn string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close(ctx) })
	return conn
}
