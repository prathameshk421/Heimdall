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
	models   []ports.ManagedAgentSpec
	modelErr error
	ensures  []ports.ManagedAgentSpec
	states   []struct {
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

func (f *fakeProvisioner) SetManagedAgentModel(_ context.Context, spec ports.ManagedAgentSpec) error {
	f.models = append(f.models, spec)
	return f.modelErr
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

type fakeReceiptLedger struct {
	records map[string]string
	count   int
}

func (f *fakeReceiptLedger) RecordAgentSetupReceipt(_ context.Context, agent id.AgentName, fingerprint string) (bool, error) {
	if f.records == nil {
		f.records = make(map[string]string)
	}
	key := string(agent)
	if existing, exists := f.records[key]; exists {
		if existing != fingerprint {
			return false, errors.New("receipt fingerprint conflict")
		}
		return false, nil
	}
	f.records[key] = fingerprint
	f.count++
	return true, nil
}

func (f *fakeReceiptLedger) LoadAgentSetupReceipt(_ context.Context, agent id.AgentName) (ports.AgentSetupReceipt, bool, error) {
	fingerprint, ok := f.records[string(agent)]
	return ports.AgentSetupReceipt{Agent: agent, Fingerprint: fingerprint}, ok, nil
}

func newService(t *testing.T, p *fakeProvisioner) *Service {
	t.Helper()
	svc, err := New(Config{
		Provisioner: p,
		Receipts:    &fakeReceiptLedger{},
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

// Each definition asks Switch for the directory the host installer fills for that agent, so an agent that already
// exists there is adopted instead of refused as a conflict.
func TestPrepareAsksForEachAgentsOwnDirectory(t *testing.T) {
	p := &fakeProvisioner{}
	svc, err := New(Config{
		Provisioner: p, Receipts: &fakeReceiptLedger{},
		Secrets: fakeSecrets{"agent_token_test-author": "a", "agent_token_reviewer-1": "b"},
		Machine: "heimdall-vm", MCPEndpoint: "http://127.0.0.1:8080/mcp", Model: "openrouter/deepseek/deepseek-v4-flash",
		SecretsDir: "/etc/heimdall/secrets", AgentsDir: "/var/lib/switch-agents/1000/agent",
		Agents: []Agent{
			{Name: id.AgentName("reviewer-1"), Role: workflow.RoleReviewer, Description: "Heimdall reviewer-1"},
			{Name: id.AgentName("test-author"), Role: workflow.RoleTestAuthor, Description: "Heimdall test-author"},
		},
	})
	require.NoError(t, err)
	_, err = svc.Prepare(context.Background())
	require.NoError(t, err)
	require.Len(t, p.ensures, 2)
	require.Equal(t, "/var/lib/switch-agents/1000/agent/reviewer-1", p.ensures[0].Directory)
	require.Equal(t, "/var/lib/switch-agents/1000/agent/test-author", p.ensures[1].Directory)
}

func testTokenHash() string {
	sum := sha256.Sum256([]byte("author-token"))
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
		Provisioner: p, Receipts: &fakeReceiptLedger{}, Secrets: fakeSecrets{"agent_token_test-author": "same", "agent_token_reviewer-1": "same"},
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
		TokenSHA256: testTokenHash(), CredentialsInstalled: true}
	err := svc.Activate(context.Background(), receipt)
	require.Error(t, err)
	require.Empty(t, p.states)
}

func TestActivationRetryAfterControllerFailureReconcilesDesiredState(t *testing.T) {
	p := &fakeProvisioner{err: errors.New("controller unavailable")}
	svc := newService(t, p)
	receipt := Receipt{Agent: "test-author", Role: workflow.RoleTestAuthor, MCPEndpoint: "http://127.0.0.1:8080/mcp",
		TokenSHA256: testTokenHash(), CredentialsInstalled: true}
	require.Error(t, svc.Activate(context.Background(), receipt))
	p.err = nil
	require.NoError(t, svc.Activate(context.Background(), receipt))
	require.NoError(t, svc.Activate(context.Background(), receipt))
	require.Len(t, p.ensures, 3, "each retry must reconcile the managed definition before requesting state")
	require.Len(t, p.states, 2)
	for _, state := range p.states {
		require.Equal(t, "switch-test-author", state.id)
		require.Equal(t, ports.ManagedAgentRunning, state.state)
	}
}

func TestActivationRecordsDurableReceiptOnlyAfterSwitchAcceptsStart(t *testing.T) {
	p := &fakeProvisioner{err: errors.New("controller unavailable")}
	svc := newService(t, p)
	ledger := svc.cfg.Receipts.(*fakeReceiptLedger)
	receipt := Receipt{Agent: "test-author", Role: workflow.RoleTestAuthor, MCPEndpoint: "http://127.0.0.1:8080/mcp",
		TokenSHA256: testTokenHash(), CredentialsInstalled: true}

	err := svc.Activate(context.Background(), receipt)
	require.Error(t, err)
	require.Empty(t, ledger.records, "a failed controller activation must not be recorded as installed")

	p.err = nil
	require.NoError(t, svc.Activate(context.Background(), receipt))
	stored, found, err := ledger.LoadAgentSetupReceipt(context.Background(), receipt.Agent)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, receipt.Agent, stored.Agent)
	require.Equal(t, receiptFingerprint(receipt), stored.Fingerprint)
	require.NoError(t, svc.Activate(context.Background(), receipt), "the same receipt can safely reconcile after a restart")
	require.Equal(t, 1, ledger.count, "an exact replay adds no second local receipt")
}

func TestSyncDefinitionsMovesEveryAgentToTheConfiguredModelAndCreatesNone(t *testing.T) {
	p := &fakeProvisioner{}
	svc := newService(t, p)
	require.NoError(t, svc.SyncDefinitions(context.Background()))
	require.Len(t, p.models, len(svc.cfg.Agents))
	for _, spec := range p.models {
		require.Equal(t, svc.cfg.Model, spec.Model)
	}
	require.Empty(t, p.ensures, "a sync never creates an agent")
	require.Empty(t, p.states, "a sync never starts an agent")
}

func TestSyncDefinitionsLeavesAnAgentThatIsNotInSwitchYet(t *testing.T) {
	p := &fakeProvisioner{modelErr: ports.ErrNotFound}
	require.NoError(t, newService(t, p).SyncDefinitions(context.Background()))
}
