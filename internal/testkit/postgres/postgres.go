// Package postgres starts a real, throwaway PostgreSQL in Docker for component tests (L2).
// It is test code only (D121). It fails the test when Docker is not available: a skipped
// database test would let a missing check pass.
package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// image is the server version the product targets; component tests run against exactly this.
const image = "postgres:16-alpine"

// Start runs a fresh PostgreSQL container, returns its connection string, and stops it when the test ends.
func Start(t testing.TB) string {
	t.Helper()
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, image,
		tcpostgres.WithDatabase("heimdall"),
		tcpostgres.WithUsername("heimdall"),
		tcpostgres.WithPassword("heimdall"),
		tcpostgres.BasicWaitStrategies(),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(90*time.Second)),
	)
	testcontainers.CleanupContainer(t, ctr)
	require.NoError(t, err, "start PostgreSQL in Docker (a component test never skips for a missing Docker)")
	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	return dsn
}
