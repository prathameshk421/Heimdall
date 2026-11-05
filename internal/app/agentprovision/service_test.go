package agentprovision

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

type fakeProvisioner struct {
	ensures []ports.ManagedAgentSpec
	states  []struct {
		id    string
		state ports.ManagedAgentState
	}
	err error
}

func (f *fakeProvisioner) EnsureManagedAgent(_ context.Context, spec ports.ManagedAgentSpec) (ports.ManagedAgent, error) {
	f.ensures = append(f.ensures, spec)
	if f.err != nil {
		return ports.ManagedAgent{}, f.err
	}
	return ports.ManagedAgent{ID: "switch-" + string(spec.Agent.Name), Name: string(spec.Agent.Name)}, nil
}

func (f *fakeProvisioner) SetManagedAgentState(_ context.Context, agentID string, state ports.ManagedAgentState) error {
	f.states = append(f.states, struct {
		id    string
		state ports.ManagedAgentState
	}{agentID, state})
	return f.err
}

type fakeSecrets map[string]string

func (s fakeSecrets) Secret(name string) (string, error) {
	v, ok := s[name]
	if !ok {
		return "", fmt.Errorf("secret %s missing", name)
	}
	return v, nil
}

func newService(t *testing.T, p *fakeProvisioner) *Service {
	t.Helper()
	svc, err := New(Config{
		Provisioner: p,
		Secrets:     fakeSecrets{"agent_token_test-author": "author-token", "agent_token_reviewer-1": "review-token"},
		Machine:     "heimdall-vm", MCPEndpoint: "http://127.0.0.1:8080/mcp", Model: "openrouter/deepseek/deepseek-v4-flash",
		SecretsDir: "/etc/heimdall/secrets",
		Agents: []Agent{
			{Name: id.AgentName("reviewer-1"), Role: workflow.RoleReviewer, Description: "Heimdall reviewer"},
			{Name: id.AgentName("test-author"), Role: workflow.RoleTestAuthor, Description: "Heimdall test author"},
		},
	})
	require.NoError(t, err)
	return svc
}

func testTokenHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func TestPrepareBuildsRoleBoundHandoffsAndLeavesAgentsStopped(t *testing.T) {
	p := &fakeProvisioner{}
	svc := newService(t, p)
	got, err := svc.Prepare(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, id.AgentName("reviewer-1"), got[0].Agent)
	require.Equal(t, workflow.RoleReviewer, got[0].Role)
	require.Equal(t, "/etc/heimdall/secrets/agent_token_reviewer-1", got[0].TokenFile)
	require.Equal(t, "http://127.0.0.1:8080/mcp", got[0].MCP.URL)
	require.Equal(t, "Bearer {{file:secrets/agent_token_reviewer-1}}", got[0].MCP.Auth["Authorization"])
	require.NotEmpty(t, got[0].TokenSHA256)
	require.Len(t, p.states, 2)
	for _, state := range p.states {
		require.Equal(t, ports.ManagedAgentStopped, state.state, "configuration must be installed while the controller has the agent stopped")
	}
}

func TestActivateRequiresInstalledReceiptForExactRoleEndpointAndToken(t *testing.T) {
	p := &fakeProvisioner{}
	svc := newService(t, p)
	bad := Receipt{
		Agent: id.AgentName("test-author"), Role: workflow.RoleReviewer,
		MCPEndpoint: "http://127.0.0.1:8080/mcp", CredentialsInstalled: true,
	}
	require.Error(t, svc.Activate(context.Background(), bad))
	require.Empty(t, p.states)

	receipt := Receipt{
		Agent: id.AgentName("test-author"), Role: workflow.RoleTestAuthor,
		MCPEndpoint: "http://127.0.0.1:8080/mcp", TokenSHA256: "d076b35f1f72d725ff9047f5c55d637f" + "9b1c5393d6c4c5b29406025f1e3c7c62",
		CredentialsInstalled: true,
	}
	// The host receipt is checked against the configured secret; it cannot activate on a wrong fingerprint.
	require.Error(t, svc.Activate(context.Background(), receipt))
	require.Empty(t, p.states)
}

func TestActivateRequestsSwitchStartOnlyAfterCredentialReceiptMatches(t *testing.T) {
	p := &fakeProvisioner{}
	svc := newService(t, p)
	handoffs, err := svc.Prepare(context.Background())
	require.NoError(t, err)
	var handoff Handoff
	for _, candidate := range handoffs {
		if candidate.Agent == "test-author" {
			handoff = candidate
		}
	}
	receipt := Receipt{
		Agent: handoff.Agent, Role: handoff.Role, MCPEndpoint: handoff.MCP.URL,
		TokenSHA256: handoff.TokenSHA256, CredentialsInstalled: true,
	}
	require.NoError(t, svc.Activate(context.Background(), receipt))
	require.Len(t, p.states, 3)
	require.Equal(t, "switch-test-author", p.states[2].id)
	require.Equal(t, ports.ManagedAgentRunning, p.states[2].state)
}

func TestPrepareRejectsDuplicateMCPTokenValues(t *testing.T) {
	p := &fakeProvisioner{}
	cfg := Config{
		Provisioner: p, Secrets: fakeSecrets{"agent_token_test-author": "same", "agent_token_reviewer-1": "same"},
		Machine: "heimdall-vm", MCPEndpoint: "http://127.0.0.1:8080/mcp", Model: "model", SecretsDir: "/secrets",
		Agents: []Agent{
			{Name: "test-author", Role: workflow.RoleTestAuthor, Description: "author"},
			{Name: "reviewer-1", Role: workflow.RoleReviewer, Description: "reviewer"},
		},
	}
	svc, err := New(cfg)
	require.NoError(t, err)
	_, err = svc.Prepare(context.Background())
	require.Error(t, err)
	require.Empty(t, p.ensures)
}

func TestPreparePropagatesUnavailableWithoutStartingOrMaskingError(t *testing.T) {
	p := &fakeProvisioner{err: ports.ErrUnavailable}
	svc := newService(t, p)
	_, err := svc.Prepare(context.Background())
	require.ErrorIs(t, err, ports.ErrUnavailable)
	require.Empty(t, p.states)
}

func TestActivationErrorRemainsRetryable(t *testing.T) {
	p := &fakeProvisioner{err: errors.New("controller down")}
	svc := newService(t, p)
	receipt := Receipt{Agent: "test-author", Role: workflow.RoleTestAuthor, MCPEndpoint: "http://127.0.0.1:8080/mcp",
		TokenSHA256: testTokenHash("author-token"), CredentialsInstalled: true}
	err := svc.Activate(context.Background(), receipt)
	require.Error(t, err)
	require.Empty(t, p.states)
}
