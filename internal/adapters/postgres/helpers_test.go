//go:build component

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/testkit/postgres"
)

// server is one throwaway PostgreSQL container. A top-level test starts one server and gives each of its subtests a
// fresh, migrated database, so the container start is paid once per test function.
type server struct {
	dsn     string
	created int
}

// database is one migrated database of a server.
type database struct {
	dsn  string
	pool *pgxpool.Pool
}

func startServer(t *testing.T) *server {
	t.Helper()
	return &server{dsn: postgres.Start(t)}
}

// database creates a new database, migrates it and returns a pool for it. The pool closes when t ends.
func (s *server) database(ctx context.Context, t *testing.T) database {
	t.Helper()
	db := s.emptyDatabase(ctx, t)
	require.NoError(t, Migrate(ctx, db.pool))
	return db
}

// emptyDatabase creates a new database that has no tables yet, with a pool for it that closes when t ends.
func (s *server) emptyDatabase(ctx context.Context, t *testing.T) database {
	t.Helper()
	s.created++
	name := fmt.Sprintf("heimdall_%d", s.created)

	admin, err := pgx.Connect(ctx, s.dsn)
	require.NoError(t, err)
	_, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize())
	require.NoError(t, err)
	require.NoError(t, admin.Close(ctx))

	u, err := url.Parse(s.dsn)
	require.NoError(t, err)
	u.Path = "/" + name
	dsn := u.String()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return database{dsn: dsn, pool: pool}
}

// eventSeq makes every event id unique across the test binary.
var eventSeq atomic.Int64

func newEventID() change.EventID {
	return change.EventID(fmt.Sprintf("00000000-0000-4000-8000-%012d", eventSeq.Add(1)))
}

var base = time.Date(2026, 11, 4, 9, 30, 0, 0, time.UTC)

// roomEvent is a valid room.opened event at version. Its payload names the room.
func roomEvent(cid id.ChangeID, version int64, room string) change.Event {
	return change.Event{
		ID:            newEventID(),
		ChangeID:      cid,
		Version:       version,
		Type:          change.EventRoomOpened,
		Actor:         change.Actor{Kind: change.ActorSystem, ID: "engine"},
		OccurredAt:    base.Add(time.Duration(version) * time.Second),
		CorrelationID: "corr-1",
		Payload:       json.RawMessage(`{"room_id":"` + room + `"}`),
	}
}

// roomEvents builds one room.opened event per room, at the versions after expected.
func roomEvents(cid id.ChangeID, expected int64, rooms ...string) []change.Event {
	out := make([]change.Event, 0, len(rooms))
	for i, room := range rooms {
		out = append(out, roomEvent(cid, expected+int64(i)+1, room))
	}
	return out
}

// roomTarget is the Switch room the test effects are posted to.
const roomTarget = "room-42"

// effect is an outbox row keyed by the deterministic key of change.IdempotencyKey, posted to roomTarget.
func effect(t *testing.T, cid id.ChangeID, version int64, disc, payload string) change.Effect {
	t.Helper()
	key, err := change.IdempotencyKey(cid, version, change.EffectRoomPost, disc)
	require.NoError(t, err)
	return change.Effect{Key: key, Kind: change.EffectRoomPost, Target: roomTarget, Payload: json.RawMessage(payload)}
}

// countRows returns the single integer a query yields.
func countRows(ctx context.Context, t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int64 {
	t.Helper()
	var n int64
	require.NoError(t, pool.QueryRow(ctx, sql, args...).Scan(&n))
	return n
}
