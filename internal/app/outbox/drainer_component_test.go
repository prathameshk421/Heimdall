//go:build component

package outbox_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	pgstore "github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/postgres"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/outbox"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	testpg "github.com/prathameshk421/flip_the_switch_hackathon/internal/testkit/postgres"
)

// dedupingSystem stands for an outside system that treats an idempotency key as the identity of one action. Execute
// is what the drainer calls: calls counts every execution, actions counts the distinct actions performed.
type dedupingSystem struct {
	mu      sync.Mutex
	calls   map[change.EffectKey]int
	actions map[change.EffectKey]int
}

var _ outbox.Executor = (*dedupingSystem)(nil)

func newDedupingSystem() *dedupingSystem {
	return &dedupingSystem{calls: map[change.EffectKey]int{}, actions: map[change.EffectKey]int{}}
}

func (s *dedupingSystem) Execute(_ context.Context, fx change.Effect) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls[fx.Key]++
	if s.actions[fx.Key] == 0 {
		s.actions[fx.Key] = 1
	}
	return nil
}

// crashOnce wraps the real outbox and fails the first Done, as a process that dies after Execute would look to the
// drainer: the action happened, the database never heard about it, and no Fail was recorded.
type crashOnce struct {
	ports.Outbox
	mu      sync.Mutex
	crashed bool
}

func (c *crashOnce) Done(ctx context.Context, key change.EffectKey) error {
	c.mu.Lock()
	first := !c.crashed
	c.crashed = true
	c.mu.Unlock()
	if first {
		return errors.New("simulated crash between Execute and Done")
	}
	return c.Outbox.Done(ctx, key)
}

// newPool starts a PostgreSQL container and returns a pool on its migrated database. The pool closes when t ends.
func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := testpg.Start(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	require.NoError(t, pgstore.Migrate(ctx, pool))
	return pool
}

// appendEffects commits n events of one change in one transaction, with one room post per event, each caused by the
// last event. Every effect key is distinct; every key names the version of the last event.
func appendEffects(ctx context.Context, t *testing.T, store *pgstore.Store, cid id.ChangeID, n int) []change.Effect {
	t.Helper()
	events := make([]change.Event, 0, n)
	for v := 1; v <= n; v++ {
		events = append(events, change.Event{
			ID:            change.EventID(fmt.Sprintf("00000000-0000-4000-8000-%012d", v)),
			ChangeID:      cid,
			Version:       int64(v),
			Type:          change.EventRoomOpened,
			Actor:         change.Actor{Kind: change.ActorSystem, ID: "engine"},
			OccurredAt:    time.Date(2026, 11, 4, 9, 30, v, 0, time.UTC),
			CorrelationID: "corr-1",
			Payload:       json.RawMessage(`{"room_id":"room-` + strconv.Itoa(v) + `"}`),
		})
	}
	fx := make([]change.Effect, 0, n)
	for i := range n {
		key, err := change.IdempotencyKey(cid, int64(n), change.EffectRoomPost, fmt.Sprintf("post-%02d", i))
		require.NoError(t, err)
		fx = append(fx, change.Effect{Key: key, Kind: change.EffectRoomPost, Target: "room-42",
			Payload: json.RawMessage(`{"n":` + strconv.Itoa(i) + `}`)})
	}
	_, err := store.Append(ctx, cid, 0, events, fx)
	require.NoError(t, err)
	return fx
}

func discardLog() *slog.Logger { return slog.New(slog.DiscardHandler) }

func TestDrainerWithPostgres(t *testing.T) {
	t.Run("two concurrent drainers execute each of 50 effects once when nothing crashes", func(t *testing.T) {
		pool := newPool(t)
		ctx := context.Background()
		store := pgstore.NewStore(pool)
		fx := appendEffects(ctx, t, store, id.ChangeID("PAY-142"), 50)
		sys := newDedupingSystem()

		cfg := outbox.Config{
			Outbox:        store,
			Executors:     map[change.EffectKind]outbox.Executor{change.EffectRoomPost: sys},
			Logger:        discardLog(),
			Batch:         5,
			Lease:         30 * time.Second,
			EffectTimeout: time.Second,
		}
		d1, err := outbox.New(cfg)
		require.NoError(t, err)
		d2, err := outbox.New(cfg)
		require.NoError(t, err)

		var mu sync.Mutex
		doneTotal := 0
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		for _, d := range []*outbox.Drainer{d1, d2} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					res, err := d.RunOnce(ctx)
					if err != nil {
						errs <- err
						return
					}
					if res.Claimed == 0 {
						return
					}
					mu.Lock()
					doneTotal += res.Done
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}

		require.Equal(t, 50, doneTotal, "every effect was marked done by exactly one drainer")
		require.Len(t, sys.calls, len(fx))
		for _, f := range fx {
			require.Equal(t, 1, sys.calls[f.Key], "effect %s executed once", f.Key)
		}
		require.Len(t, sys.actions, len(fx))
		pending, err := store.Pending(ctx, 100)
		require.NoError(t, err)
		require.Empty(t, pending)
	})

	t.Run("a crash after Execute and before Done never runs the effect again, and lists it as in doubt", func(t *testing.T) {
		pool := newPool(t)
		ctx := context.Background()
		store := pgstore.NewStore(pool)
		fx := appendEffects(ctx, t, store, id.ChangeID("PAY-143"), 1)[0]
		sys := newDedupingSystem()

		crashing := &crashOnce{Outbox: store}
		d, err := outbox.New(outbox.Config{
			Outbox:        crashing,
			Executors:     map[change.EffectKind]outbox.Executor{change.EffectRoomPost: sys},
			Logger:        discardLog(),
			Batch:         1,
			Lease:         200 * time.Millisecond,
			EffectTimeout: 100 * time.Millisecond,
		})
		require.NoError(t, err)

		res, err := d.RunOnce(ctx)
		require.ErrorContains(t, err, "simulated crash")
		require.Equal(t, outbox.DrainResult{Claimed: 1}, res)
		require.Equal(t, 1, sys.calls[fx.Key], "the action was performed before the crash")

		again, err := d.RunOnce(ctx)
		require.NoError(t, err)
		require.Equal(t, outbox.DrainResult{}, again, "while the lease is live the effect is not claimed again")

		require.Eventually(t, func() bool {
			var expired bool
			err := pool.QueryRow(ctx, "SELECT claimed_until <= now() FROM outbox WHERE effect_key = $1", string(fx.Key)).Scan(&expired)
			return err == nil && expired
		}, 10*time.Second, 20*time.Millisecond, "the lease on the effect runs out")

		restarted, err := d.RunOnce(ctx)
		require.NoError(t, err)
		require.Equal(t, outbox.DrainResult{}, restarted, "after the lease runs out, a started effect is in doubt and not claimed")

		sys.mu.Lock()
		defer sys.mu.Unlock()
		require.Equal(t, 1, sys.calls[fx.Key], "the effect ran once, not twice")
		require.Equal(t, 1, sys.actions[fx.Key], "the outside system performed one action")
		var inDoubt int
		require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM outbox_in_doubt").Scan(&inDoubt))
		require.Equal(t, 1, inDoubt, "the effect is listed as in doubt for reconciliation")
	})
}
