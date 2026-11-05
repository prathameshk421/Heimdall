//go:build component

package postgres

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

func TestVerifyAll(t *testing.T) {
	ctx := context.Background()
	srv := startServer(t)
	pay := id.ChangeID("PAY-142")
	pay2 := id.ChangeID("PAY-143")

	t.Run("an empty database verifies with nothing to report", func(t *testing.T) {
		s := NewStore(srv.database(ctx, t).pool)
		report, err := s.VerifyAll(ctx)
		require.NoError(t, err)
		require.Empty(t, report.Verified)
		require.Empty(t, report.Broken)
	})

	t.Run("every intact change reports its last version and head hash", func(t *testing.T) {
		s := NewStore(srv.database(ctx, t).pool)
		_, err := s.Append(ctx, pay, 0, roomEvents(pay, 0, "room-1", "room-2"), nil)
		require.NoError(t, err)
		_, err = s.Append(ctx, pay, 2, roomEvents(pay, 2, "room-3"), nil)
		require.NoError(t, err)
		_, err = s.Append(ctx, pay2, 0, roomEvents(pay2, 0, "room-4"), nil)
		require.NoError(t, err)

		report, err := s.VerifyAll(ctx)
		require.NoError(t, err)
		require.Empty(t, report.Broken)
		require.Len(t, report.Verified, 2)

		heads := map[id.ChangeID]ChangeHead{}
		for _, h := range report.Verified {
			heads[h.ChangeID] = h
		}
		payEvents, err := s.Load(ctx, pay)
		require.NoError(t, err)
		require.Equal(t, int64(3), heads[pay].LastVersion)
		require.Equal(t, payEvents[2].Hash.String(), heads[pay].HeadHash)
		require.Len(t, heads[pay].HeadHash, 64)
		require.Equal(t, int64(1), heads[pay2].LastVersion)
	})

	t.Run("a payload edited under the triggers is caught, and the other changes still verify", func(t *testing.T) {
		db := srv.database(ctx, t)
		s := NewStore(db.pool)
		_, err := s.Append(ctx, pay, 0, roomEvents(pay, 0, "room-1", "room-2"), nil)
		require.NoError(t, err)
		_, err = s.Append(ctx, pay2, 0, roomEvents(pay2, 0, "room-9"), nil)
		require.NoError(t, err)

		// A superuser session with session_replication_role = replica skips the append-only triggers.
		su := connectSuperuser(ctx, t, db.dsn)
		_, err = su.Exec(ctx, "SET session_replication_role = replica")
		require.NoError(t, err)
		tag, err := su.Exec(ctx, "UPDATE event SET payload = payload || '{\"x\":1}' WHERE change_id = 'PAY-142' AND version = 2")
		require.NoError(t, err)
		require.Equal(t, int64(1), tag.RowsAffected(), "the tamper must hit the row")

		report, err := s.VerifyAll(ctx)
		require.NoError(t, err)
		require.Len(t, report.Broken, 1)
		require.Equal(t, pay, report.Broken[0].ChangeID)
		require.ErrorIs(t, report.Broken[0].Err, change.ErrChainBroken)
		require.Len(t, report.Verified, 1)
		require.Equal(t, pay2, report.Verified[0].ChangeID)
	})

	t.Run("a deleted tail is not caught by the chain, only by the published head", func(t *testing.T) {
		db := srv.database(ctx, t)
		s := NewStore(db.pool)
		_, err := s.Append(ctx, pay, 0, roomEvents(pay, 0, "room-1", "room-2"), nil)
		require.NoError(t, err)

		su := connectSuperuser(ctx, t, db.dsn)
		_, err = su.Exec(ctx, "SET session_replication_role = replica")
		require.NoError(t, err)
		_, err = su.Exec(ctx, "DELETE FROM event WHERE change_id = 'PAY-142' AND version = 2")
		require.NoError(t, err)

		report, err := s.VerifyAll(ctx)
		require.NoError(t, err)
		require.Empty(t, report.Broken, "a shorter chain verifies, so the nightly head must be compared")
		require.Len(t, report.Verified, 1)
		require.Equal(t, int64(1), report.Verified[0].LastVersion)
	})
}
