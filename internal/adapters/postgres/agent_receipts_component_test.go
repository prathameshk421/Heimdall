//go:build component

package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

func TestAgentSetupReceiptLedgerSurvivesRestartAndExactReplay(t *testing.T) {
	ctx := context.Background()
	srv := startServer(t)
	db := srv.database(ctx, t)
	fingerprint := strings.Repeat("a", 64)
	first := NewStore(db.pool)
	inserted, err := first.RecordAgentSetupReceipt(ctx, id.AgentName("test-author"), fingerprint)
	require.NoError(t, err)
	require.True(t, inserted)

	// A new Store is the server's post-restart view of the same durable database.
	restarted := NewStore(db.pool)
	inserted, err = restarted.RecordAgentSetupReceipt(ctx, id.AgentName("test-author"), fingerprint)
	require.NoError(t, err)
	require.False(t, inserted, "replaying the exact installation receipt is idempotent")

	got, found, err := restarted.LoadAgentSetupReceipt(ctx, id.AgentName("test-author"))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, fingerprint, got.Fingerprint)
	require.Equal(t, id.AgentName("test-author"), got.Agent)
	require.Len(t, got.Fingerprint, 64, "the ledger stores a SHA-256 receipt fingerprint, never a token")

	_, err = restarted.RecordAgentSetupReceipt(ctx, id.AgentName("test-author"), "bad")
	require.Error(t, err, "malformed fingerprints are refused")
}
