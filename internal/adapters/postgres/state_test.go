//go:build component

package postgres

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

func TestCursorAndWebhookDelivery(t *testing.T) {
	ctx := context.Background()
	srv := startServer(t)

	t.Run("a missing cursor is not found, and SetCursor upserts", func(t *testing.T) {
		s := NewStore(srv.database(ctx, t).pool)
		_, err := s.GetCursor(ctx, "jira:PAY")
		require.ErrorIs(t, err, ports.ErrNotFound)

		require.NoError(t, s.SetCursor(ctx, "jira:PAY", "2026-11-04T09:00:00Z"))
		got, err := s.GetCursor(ctx, "jira:PAY")
		require.NoError(t, err)
		require.Equal(t, "2026-11-04T09:00:00Z", got)

		require.NoError(t, s.SetCursor(ctx, "jira:PAY", "2026-11-04T09:30:00Z"))
		got, err = s.GetCursor(ctx, "jira:PAY")
		require.NoError(t, err)
		require.Equal(t, "2026-11-04T09:30:00Z", got)
	})

	t.Run("cursors are independent by source", func(t *testing.T) {
		s := NewStore(srv.database(ctx, t).pool)
		require.NoError(t, s.SetCursor(ctx, "github:acme/pay-demo:pulls", "a"))
		require.NoError(t, s.SetCursor(ctx, "switch:coordinator", "b"))
		got, err := s.GetCursor(ctx, "github:acme/pay-demo:pulls")
		require.NoError(t, err)
		require.Equal(t, "a", got)
	})

	t.Run("a webhook delivery is seen once per provider", func(t *testing.T) {
		s := NewStore(srv.database(ctx, t).pool)
		seen, err := s.SeenDelivery(ctx, "github", "d-1")
		require.NoError(t, err)
		require.False(t, seen, "the first delivery is new")

		seen, err = s.SeenDelivery(ctx, "github", "d-1")
		require.NoError(t, err)
		require.True(t, seen, "the same delivery again is a duplicate")

		seen, err = s.SeenDelivery(ctx, "jira", "d-1")
		require.NoError(t, err)
		require.False(t, seen, "the same id from another provider is a different delivery")
	})
}
