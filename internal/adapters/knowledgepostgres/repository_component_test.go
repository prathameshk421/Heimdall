//go:build component

package knowledgepostgres

import (
	"context"
	"fmt"
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	pgtest "github.com/prathameshk421/flip_the_switch_hackathon/internal/testkit/postgres"
)

func TestRepositoryDurablePendingReplayAndCompletion(t *testing.T) {
	ctx := context.Background()
	dsn := pgtest.Start(t)
	admin, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	name := "knowledge_component"
	_, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize())
	require.NoError(t, err)
	require.NoError(t, admin.Close(ctx))
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	u.Path = "/" + name
	pool, err := pgxpool.New(ctx, u.String())
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	require.NoError(t, Migrate(ctx, pool))
	repo, err := New(pool)
	require.NoError(t, err)
	record := ports.KnowledgeRecord{Team: "payments", SourcePath: "docs/refunds.md", SourceDigest: fmt.Sprintf("%064d", 1), ContentDigest: fmt.Sprintf("%064d", 2), IdempotencyKey: "delivery-1", ApprovedBy: "owner", Subject: "agent", Content: "redacted", State: ports.KnowledgePending}
	first, err := repo.Begin(ctx, record)
	require.NoError(t, err)
	require.Equal(t, ports.KnowledgePending, first.State)
	second, err := repo.Begin(ctx, record)
	require.NoError(t, err)
	require.Equal(t, first, second, "same replay returns the persisted row")
	got, found, err := repo.Find(ctx, "payments", "delivery-1")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, record.Content, got.Content)
	require.Equal(t, ports.KnowledgePending, got.State, "a failed external delivery can be retried after restart")
	require.NoError(t, repo.Complete(ctx, "payments", "delivery-1"))
	got, found, err = repo.Find(ctx, "payments", "delivery-1")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, ports.KnowledgeComplete, got.State)
}
