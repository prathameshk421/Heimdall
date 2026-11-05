//go:build component

package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

type testEffectJobArgs struct {
	EffectKey string `json:"effect_key"`
}

func (testEffectJobArgs) Kind() string { return "outbox_effect" }

type testRiverEnqueuer struct {
	client  *river.Client[pgx.Tx]
	failKey change.EffectKey
}

func (e testRiverEnqueuer) EnqueueEffectTx(ctx context.Context, tx pgx.Tx, fx change.Effect) error {
	_, err := e.client.InsertTx(ctx, tx, testEffectJobArgs{EffectKey: string(fx.Key)}, &river.InsertOpts{
		Queue: "integrations", UniqueOpts: river.UniqueOpts{ByArgs: true},
	})
	if err == nil && fx.Key == e.failKey {
		return errors.New("injected queue failure after insertion")
	}
	return err
}

func TestAppendInsertsRiverJobInOutboxTransaction(t *testing.T) {
	ctx := context.Background()
	srv := startServer(t)
	db := srv.database(ctx, t)
	driver := riverpgxv5.New(db.pool)
	migrator, err := rivermigrate.New(driver, nil)
	require.NoError(t, err)
	_, err = migrator.Migrate(ctx, rivermigrate.DirectionUp, nil)
	require.NoError(t, err)
	workers := river.NewWorkers()
	river.AddWorker(workers, river.WorkFunc(func(context.Context, *river.Job[testEffectJobArgs]) error { return nil }))
	client, err := river.NewClient(driver, &river.Config{
		Workers: workers,
		Queues:  map[string]river.QueueConfig{"integrations": {MaxWorkers: 1}},
	})
	require.NoError(t, err)

	s := NewStore(db.pool)
	s.SetEffectEnqueuer(testRiverEnqueuer{client: client})
	cid := id.ChangeID("PAY-142")
	fx := effect(t, cid, 1, "river-job", `{"text":"hello"}`)
	_, err = s.Append(ctx, cid, 0, roomEvents(cid, 0, "room-river"), []change.Effect{fx})
	require.NoError(t, err)

	var jobs int
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE kind = 'outbox_effect' AND args->>'effect_key' = $1`, string(fx.Key)).Scan(&jobs))
	require.Equal(t, 1, jobs, "each committed effect must have one River job")
	require.Len(t, pendingOf(ctx, t, s), 1)

	_, err = s.Append(ctx, cid, 1, roomEvents(cid, 1, "room-river-replay"), []change.Effect{fx})
	require.NoError(t, err, "replaying the same effect key preserves the event append")
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE kind = 'outbox_effect' AND args->>'effect_key' = $1`, string(fx.Key)).Scan(&jobs))
	require.Equal(t, 1, jobs, "replaying an effect must not create a second River job")

	rollbackStore := NewStore(db.pool)
	first := effect(t, id.ChangeID("PAY-143"), 2, "first", `{"text":"first"}`)
	second := effect(t, id.ChangeID("PAY-143"), 2, "second", `{"text":"second"}`)
	rollbackStore.SetEffectEnqueuer(testRiverEnqueuer{client: client, failKey: second.Key})
	_, err = rollbackStore.Append(ctx, id.ChangeID("PAY-143"), 0, roomEvents(id.ChangeID("PAY-143"), 0, "room-rollback", "room-rollback-2"), []change.Effect{first, second})
	require.ErrorContains(t, err, "injected queue failure")
	var eventCount, outboxCount, jobCount int
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT count(*) FROM event WHERE change_id = 'PAY-143'`).Scan(&eventCount))
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE effect_key IN ($1, $2)`, string(first.Key), string(second.Key)).Scan(&outboxCount))
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE kind = 'outbox_effect' AND (args->>'effect_key' = $1 OR args->>'effect_key' = $2)`, string(first.Key), string(second.Key)).Scan(&jobCount))
	require.Zero(t, eventCount, "failed append must roll back event rows")
	require.Zero(t, outboxCount, "failed append must roll back outbox rows")
	require.Zero(t, jobCount, "failed append must roll back River jobs")
}
