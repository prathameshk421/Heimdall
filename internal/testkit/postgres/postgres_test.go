//go:build component

package postgres_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/testkit/postgres"
)

func query(t *testing.T, dsn, sql string) string {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close(ctx)) })
	var out string
	require.NoError(t, conn.QueryRow(ctx, sql).Scan(&out))
	return out
}

func TestStartGivesARealPostgres(t *testing.T) {
	dsn := postgres.Start(t)
	require.Contains(t, query(t, dsn, "select version()"), "PostgreSQL 16")
}

func TestEachStartIsAnIsolatedDatabase(t *testing.T) {
	a, b := postgres.Start(t), postgres.Start(t)
	require.NotEqual(t, a, b)
	conn, err := pgx.Connect(context.Background(), a)
	require.NoError(t, err)
	_, err = conn.Exec(context.Background(), "create table only_in_a (id int)")
	require.NoError(t, err)
	require.NoError(t, conn.Close(context.Background()))
	require.Equal(t, "", query(t, b, "select coalesce(to_regclass('only_in_a')::text, '')"))
}
