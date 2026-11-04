package postgres

import (
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

func TestTranslateAppendError(t *testing.T) {
	t.Run("nil stays nil", func(t *testing.T) {
		require.NoError(t, translateAppendErr(nil))
	})

	t.Run("a unique violation on (change_id, version) is a version conflict", func(t *testing.T) {
		pgErr := &pgconn.PgError{Code: "23505", ConstraintName: "event_change_id_version_key"}
		err := translateAppendErr(fmt.Errorf("insert event: %w", pgErr))
		require.ErrorIs(t, err, ports.ErrVersionConflict)
		var kept *pgconn.PgError
		require.ErrorAs(t, err, &kept, "the database error stays in the chain for the logs")
	})

	t.Run("a unique violation on another constraint is not a conflict", func(t *testing.T) {
		err := translateAppendErr(&pgconn.PgError{Code: "23505", ConstraintName: "event_id_key"})
		require.Error(t, err)
		require.NotErrorIs(t, err, ports.ErrVersionConflict)
	})

	t.Run("another database error is passed through unchanged", func(t *testing.T) {
		in := &pgconn.PgError{Code: "23503", ConstraintName: "outbox_event_id_fkey"}
		require.Same(t, in, translateAppendErr(in))
	})
}
