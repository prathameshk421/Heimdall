//go:build component

package postgres

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

func jobFx(payload string) change.Effect {
	return change.Effect{Key: "PAY-1/room/r0", Kind: change.EffectRoomPost, Target: roomTarget, Payload: json.RawMessage(payload)}
}

func TestEnsureEffects(t *testing.T) {
	ctx := context.Background()
	srv := startServer(t)
	cid := id.ChangeID("PAY-1")

	t.Run("attaches the effect to the newest event, and the same key and content is a no-op", func(t *testing.T) {
		s := NewStore(srv.database(ctx, t).pool)
		_, err := s.Append(ctx, cid, 0, roomEvents(cid, 0, "r1", "r2"), nil)
		require.NoError(t, err)

		fx := jobFx(`{"text":"a"}`)
		require.NoError(t, s.EnsureEffects(ctx, cid, []change.Effect{fx}))
		require.NoError(t, s.EnsureEffects(ctx, cid, []change.Effect{fx}))
		require.EqualValues(t, 1, countRows(ctx, t, s.pool, "SELECT count(*) FROM outbox WHERE effect_key = $1", "PAY-1/room/r0"))
		require.EqualValues(t, 1, countRows(ctx, t, s.pool,
			`SELECT count(*) FROM outbox o JOIN event e ON e.id = o.event_id WHERE e.change_id = $1 AND e.version = 2`, string(cid)),
			"the row hangs on the change's newest event")
	})

	t.Run("the same key with other content is refused loudly", func(t *testing.T) {
		s := NewStore(srv.database(ctx, t).pool)
		_, err := s.Append(ctx, cid, 0, roomEvents(cid, 0, "r1"), nil)
		require.NoError(t, err)
		require.NoError(t, s.EnsureEffects(ctx, cid, []change.Effect{jobFx(`{"text":"a"}`)}))
		require.Error(t, s.EnsureEffects(ctx, cid, []change.Effect{jobFx(`{"text":"b"}`)}))
	})

	t.Run("a change with no events is an error and nothing is written", func(t *testing.T) {
		s := NewStore(srv.database(ctx, t).pool)
		require.Error(t, s.EnsureEffects(ctx, cid, []change.Effect{jobFx(`{"text":"a"}`)}))
		require.EqualValues(t, 0, countRows(ctx, t, s.pool, "SELECT count(*) FROM outbox"))
	})
}

func TestOpenChangesMatchesView_Over(t *testing.T) {
	ctx := context.Background()
	s := NewStore(startServer(t).database(ctx, t).pool)
	pr := func(n int) change.PROpened {
		return change.PROpened{Repo: "acme/pay", Number: n, HeadSHA: "1111111111111111111111111111111111111111", Author: "dev-alice"}
	}
	closed := func(n int) change.PRClosed {
		return change.PRClosed{Repo: "acme/pay", Number: n, HeadSHA: "1111111111111111111111111111111111111111"}
	}
	app := func(cid id.ChangeID, evs ...change.Event) {
		_, err := s.Append(ctx, cid, 0, evs, nil)
		require.NoError(t, err)
	}
	app("PAY-1", prEvent(t, "PAY-1", 1, change.EventPROpened, pr(1)))
	app("PAY-2", prEvent(t, "PAY-2", 1, change.EventPROpened, pr(2)), prEvent(t, "PAY-2", 2, change.EventPRClosed, closed(2)))
	app("PAY-3", roomEvent("PAY-3", 1, "room-3"))
	app("PAY-4", prEvent(t, "PAY-4", 1, change.EventChangeRejected, change.Rejected{Reason: "no"}))
	app("PAY-5", prEvent(t, "PAY-5", 1, change.EventDecisionRecorded,
		change.DecisionRecorded{Kind: change.DecisionReject, Target: "change", Login: "dev-alice", Reason: "no", SourceRef: "sw_1"}))
	app("PAY-6", prEvent(t, "PAY-6", 1, change.EventDecisionRecorded,
		change.DecisionRecorded{Kind: change.DecisionUphold, Target: "F1", Login: "dev-alice", Reason: "ok", SourceRef: "sw_2"}))
	reopened := func(n int) change.PRReopened {
		return change.PRReopened{Repo: "acme/pay", Number: n, HeadSHA: "1111111111111111111111111111111111111111"}
	}
	merged := closed(8)
	merged.Merged = true
	app("PAY-7", prEvent(t, "PAY-7", 1, change.EventPROpened, pr(7)), prEvent(t, "PAY-7", 2, change.EventPRClosed, closed(7)),
		prEvent(t, "PAY-7", 3, change.EventPRReopened, reopened(7)))
	app("PAY-8", prEvent(t, "PAY-8", 1, change.EventPROpened, pr(8)), prEvent(t, "PAY-8", 2, change.EventPRClosed, merged),
		prEvent(t, "PAY-8", 3, change.EventPRReopened, reopened(8)))
	app("PAY-9", prEvent(t, "PAY-9", 1, change.EventPROpened, pr(9)), prEvent(t, "PAY-9", 2, change.EventPRClosed, closed(9)),
		prEvent(t, "PAY-9", 3, change.EventPRReopened, reopened(9)), prEvent(t, "PAY-9", 4, change.EventPRClosed, closed(9)))

	got, err := s.OpenChanges(ctx)
	require.NoError(t, err)

	var want []id.ChangeID
	for _, cid := range []id.ChangeID{"PAY-1", "PAY-2", "PAY-3", "PAY-4", "PAY-5", "PAY-6", "PAY-7", "PAY-8", "PAY-9"} {
		evs, err := s.Load(ctx, cid)
		require.NoError(t, err)
		v, err := change.Fold(evs)
		require.NoError(t, err)
		if !v.Over() {
			want = append(want, cid)
		}
	}
	require.Equal(t, want, got, "the SQL and the fold agree on which changes are over")
	require.Equal(t, []id.ChangeID{"PAY-1", "PAY-3", "PAY-6", "PAY-7"}, got, "a reopen undoes an unmerged close only")
}

func TestDeadEffects(t *testing.T) {
	ctx := context.Background()
	s := NewStore(startServer(t).database(ctx, t).pool)
	cid := id.ChangeID("PAY-142") // the change appendOne writes to
	ok := appendOne(ctx, t, s, 1, "ok")
	dead := appendOne(ctx, t, s, 2, "dead")
	doubt := appendOne(ctx, t, s, 3, "doubt")
	other := id.ChangeID("PAY-2")
	_, err := s.Append(ctx, other, 0, roomEvents(other, 0, "r"), []change.Effect{effect(t, other, 1, "x", `{"text":"x"}`)})
	require.NoError(t, err)

	require.NoError(t, s.Done(ctx, ok.Key))
	for i := 0; i < maxAttempts; i++ {
		require.NoError(t, s.Fail(ctx, dead.Key, "switch is down"))
	}
	_, err = s.Claim(ctx, 10, claimLong)
	require.NoError(t, err)
	require.NoError(t, s.Start(ctx, doubt.Key))
	expireLease(ctx, t, s.pool, doubt.Key)

	got, err := s.DeadEffects(ctx, cid)
	require.NoError(t, err)
	require.Equal(t, []change.DeadJob{
		{Key: dead.Key, Kind: change.EffectRoomPost, Attempts: maxAttempts, LastError: "switch is down"},
		{Key: doubt.Key, Kind: change.EffectRoomPost, InDoubt: true},
	}, got, "done rows, live rows and other changes' rows are not listed")
}
