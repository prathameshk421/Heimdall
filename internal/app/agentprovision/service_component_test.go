//go:build component

package agentprovision

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/postgres"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	testpg "github.com/prathameshk421/flip_the_switch_hackathon/internal/testkit/postgres"
)

func TestActivationReceiptSurvivesServiceAndDatabasePoolRestart(t *testing.T) {
	ctx := context.Background()
	dsn := testpg.Start(t)
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	require.NoError(t, postgres.Migrate(ctx, pool))
	store := postgres.NewStore(pool)
	provisioner := &fakeProvisioner{}
	secrets := fakeSecrets{"agent_token_test-author": "author-token", "agent_token_reviewer-1": "review-token"}
	cfg := Config{
		Provisioner: provisioner, Receipts: store, Secrets: secrets,
		Machine: "heimdall-vm", MCPEndpoint: "http://127.0.0.1:8080/mcp", Model: "openrouter/deepseek/deepseek-v4-flash",
		SecretsDir: "/etc/heimdall/secrets",
		Agents: []Agent{
			{Name: id.AgentName("test-author"), Role: workflow.RoleTestAuthor, Description: "Heimdall test author"},
			{Name: id.AgentName("reviewer-1"), Role: workflow.RoleReviewer, Description: "Heimdall reviewer"},
		},
	}
	first, err := New(cfg)
	require.NoError(t, err)
	receipt := Receipt{Agent: "test-author", Role: workflow.RoleTestAuthor, MCPEndpoint: cfg.MCPEndpoint,
		TokenSHA256: testTokenHash(), CredentialsInstalled: true}
	require.NoError(t, first.Activate(ctx, receipt))

	// Reopen the pool and rebuild both the adapter and service, matching a process restart.
	pool.Close()
	restartedPool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(func() { restartedPool.Close() })
	restarted := postgres.NewStore(restartedPool)
	cfg.Provisioner = &fakeProvisioner{}
	cfg.Receipts = restarted
	second, err := New(cfg)
	require.NoError(t, err)
	require.NoError(t, second.Activate(ctx, receipt), "a repeated valid host receipt reconciles the desired Switch state after restart")

	loaded, found, err := restarted.LoadAgentSetupReceipt(ctx, receipt.Agent)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, receipt.Agent, loaded.Agent)
	require.Equal(t, receiptFingerprint(receipt), loaded.Fingerprint)
	var rows int
	require.NoError(t, restartedPool.QueryRow(ctx, `SELECT count(*) FROM agent_setup_receipts WHERE agent_name=$1`, string(receipt.Agent)).Scan(&rows))
	require.Equal(t, 1, rows, "restart replay does not duplicate a receipt")
}
