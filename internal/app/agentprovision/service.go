// Package agentprovision reconciles role agents with Switch and emits the host setup
// contract. Switch remains responsible for process launch and supervision.
package agentprovision

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// SecretReader reads one configured agent token without exposing it through error text.
type SecretReader interface {
	Secret(name string) (string, error)
}

// Agent is one role-bound Switch identity.
type Agent struct {
	Name         id.AgentName
	Role         workflow.Role
	Description  string
	Instructions string
}

// Config describes the stable enrollment and MCP endpoint inputs shared by all role agents.
type Config struct {
	Provisioner ports.AgentProvisioner
	Receipts    ports.AgentReceiptLedger
	Secrets     SecretReader
	Machine     string
	MCPEndpoint string
	Model       string
	SecretsDir  string
	// AgentsDir is the controller's per-agent directory root; empty leaves each agent's directory to Switch.
	AgentsDir string
	Agents    []Agent
}

// Service reconciles Switch managed agents and creates setup handoffs for the host installer.
type Service struct{ cfg Config }

// New validates dependencies and the role map before any Switch request can be made.
func New(cfg Config) (*Service, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}
	return &Service{cfg: cfg}, nil
}

func validateConfig(cfg Config) error {
	if cfg.Provisioner == nil || cfg.Secrets == nil || cfg.Receipts == nil {
		return errors.New("agent provision: provisioner, receipt ledger and secrets are required")
	}
	if cfg.Machine == "" || cfg.MCPEndpoint == "" || cfg.Model == "" || cfg.SecretsDir == "" {
		return errors.New("agent provision: machine, MCP endpoint, model and secrets directory are required")
	}
	if len(cfg.Agents) == 0 {
		return errors.New("agent provision: at least one role agent is required")
	}
	return validateAgents(cfg.Agents)
}

func validateAgents(agents []Agent) error {
	seen := make(map[id.AgentName]workflow.Role, len(agents))
	for _, a := range agents {
		ref := ports.AgentRef{Name: a.Name, Role: a.Role}
		if err := ref.Validate(); err != nil {
			return fmt.Errorf("agent provision: %w", err)
		}
		if previous, ok := seen[a.Name]; ok {
			return fmt.Errorf("agent provision: %q has roles %s and %s", a.Name, previous, a.Role)
		}
		if strings.TrimSpace(a.Description) == "" {
			return fmt.Errorf("agent provision: description is required for %q", a.Name)
		}
		seen[a.Name] = a.Role
	}
	return nil
}

// Handoff is the non-secret role-specific config consumed by host setup. The bearer
// remains in the named secret file; the installer resolves it into a mode-0600 file.
type Handoff struct {
	Agent        id.AgentName  `json:"agent"`
	Role         workflow.Role `json:"role"`
	SwitchAgent  string        `json:"switch_agent_id"`
	Machine      string        `json:"machine"`
	MCP          MCPConfig     `json:"mcp"`
	TokenFile    string        `json:"token_file"`
	TokenSHA256  string        `json:"token_sha256"`
	DesiredState string        `json:"desired_state"`
}

// MCPConfig is the per-agent OpenCode MCP entry written by host setup.
type MCPConfig struct {
	Type string            `json:"type"`
	URL  string            `json:"url"`
	Auth map[string]string `json:"headers"`
}

// Receipt records that host setup installed the exact endpoint and token represented by a handoff.
type Receipt struct {
	Agent                id.AgentName  `json:"agent"`
	Role                 workflow.Role `json:"role"`
	MCPEndpoint          string        `json:"mcp_endpoint"`
	TokenSHA256          string        `json:"token_sha256"`
	CredentialsInstalled bool          `json:"credentials_installed"`
}

// Prepare idempotently ensures every managed definition exists stopped, then returns
// role-bound config handoffs. Replaying it yields the same contract and creates no duplicate.
func (s *Service) Prepare(ctx context.Context) ([]Handoff, error) {
	agents := slices.Clone(s.cfg.Agents)
	slices.SortFunc(agents, func(a, b Agent) int { return strings.Compare(string(a.Name), string(b.Name)) })
	type preparedAgent struct {
		agent      Agent
		secretName string
		tokenHash  [32]byte
	}
	seenTokens := map[[32]byte]id.AgentName{}
	prepared := make([]preparedAgent, 0, len(agents))
	for _, a := range agents {
		secretName := "agent_token_" + string(a.Name)
		token, err := s.cfg.Secrets.Secret(secretName)
		if err != nil {
			return nil, fmt.Errorf("agent provision %q: read token file %s: %w", a.Name, secretName, err)
		}
		if token == "" || strings.ContainsAny(token, "\r\n") {
			return nil, fmt.Errorf("agent provision %q: token must be a non-empty single line", a.Name)
		}
		hash := sha256.Sum256([]byte(token))
		if other, ok := seenTokens[hash]; ok {
			return nil, fmt.Errorf("agent provision: %q and %q use the same token", other, a.Name)
		}
		seenTokens[hash] = a.Name
		prepared = append(prepared, preparedAgent{agent: a, secretName: secretName, tokenHash: hash})
	}
	out := make([]Handoff, 0, len(prepared))
	for _, preparedAgent := range prepared {
		a := preparedAgent.agent
		spec := ports.ManagedAgentSpec{
			Agent: ports.AgentRef{Name: a.Name, Role: a.Role}, Description: a.Description,
			Machine: s.cfg.Machine, Provider: "opencode", Model: s.cfg.Model,
			Instructions: a.Instructions, Directory: s.directoryOf(a.Name),
		}
		managed, err := s.cfg.Provisioner.EnsureManagedAgent(ctx, spec)
		if err != nil {
			return nil, fmt.Errorf("agent provision %q: reconcile Switch definition: %w", a.Name, err)
		}
		if managed.ID == "" || managed.Name != string(a.Name) {
			return nil, fmt.Errorf("agent provision %q: Switch returned a mismatched identity", a.Name)
		}
		if err := s.cfg.Provisioner.SetManagedAgentState(ctx, managed.ID, ports.ManagedAgentStopped); err != nil {
			return nil, fmt.Errorf("agent provision %q: request Switch stop before configuration: %w", a.Name, err)
		}
		out = append(out, Handoff{
			Agent: a.Name, Role: a.Role, SwitchAgent: managed.ID, Machine: s.cfg.Machine,
			MCP: MCPConfig{Type: "remote", URL: s.cfg.MCPEndpoint,
				Auth: map[string]string{"Authorization": "Bearer {{file:secrets/" + preparedAgent.secretName + "}}"}},
			TokenFile: filepath.Join(s.cfg.SecretsDir, preparedAgent.secretName), TokenSHA256: hex.EncodeToString(preparedAgent.tokenHash[:]),
			DesiredState: string(ports.ManagedAgentStopped),
		})
	}
	return out, nil
}

// Activate accepts only a host receipt for the exact configured role, endpoint and
// token. Switch performs the actual start; repeated receipts safely replay desired state.
func (s *Service) Activate(ctx context.Context, receipt Receipt) error {
	if !receipt.CredentialsInstalled {
		return fmt.Errorf("agent provision: host has not confirmed credential installation: %w", ports.ErrInvalidRequest)
	}
	var agent *Agent
	for i := range s.cfg.Agents {
		if s.cfg.Agents[i].Name == receipt.Agent {
			agent = &s.cfg.Agents[i]
			break
		}
	}
	if agent == nil || agent.Role != receipt.Role {
		return fmt.Errorf("agent provision: setup receipt does not match configured role agent %q: %w", receipt.Agent, ports.ErrInvalidRequest)
	}
	if receipt.MCPEndpoint != s.cfg.MCPEndpoint {
		return fmt.Errorf("agent provision: setup receipt MCP endpoint does not match configured endpoint: %w", ports.ErrInvalidRequest)
	}
	token, err := s.cfg.Secrets.Secret("agent_token_" + string(agent.Name))
	if err != nil {
		return fmt.Errorf("agent provision %q: read token for setup receipt: %w", agent.Name, err)
	}
	hash := sha256.Sum256([]byte(token))
	if receipt.TokenSHA256 != hex.EncodeToString(hash[:]) {
		return fmt.Errorf("agent provision %q: setup receipt token fingerprint mismatch: %w", agent.Name, ports.ErrInvalidRequest)
	}
	fingerprint := receiptFingerprint(receipt)
	managed, err := s.cfg.Provisioner.EnsureManagedAgent(ctx, ports.ManagedAgentSpec{
		Agent: ports.AgentRef{Name: agent.Name, Role: agent.Role}, Description: agent.Description,
		Machine: s.cfg.Machine, Provider: "opencode", Model: s.cfg.Model, Instructions: agent.Instructions,
		Directory: s.directoryOf(agent.Name),
	})
	if err != nil {
		return fmt.Errorf("agent provision %q: reconcile before activation: %w", agent.Name, err)
	}
	if err := s.cfg.Provisioner.SetManagedAgentState(ctx, managed.ID, ports.ManagedAgentRunning); err != nil {
		return fmt.Errorf("agent provision %q: request Switch start: %w", agent.Name, err)
	}
	if _, err := s.cfg.Receipts.RecordAgentSetupReceipt(ctx, agent.Name, fingerprint); err != nil {
		return fmt.Errorf("agent provision %q: persist setup receipt fingerprint: %w", agent.Name, err)
	}
	return nil
}

// directoryOf is the directory the host installer fills for the agent, or empty when Switch picks it.
func (s *Service) directoryOf(name id.AgentName) string {
	if s.cfg.AgentsDir == "" {
		return ""
	}
	return filepath.Join(s.cfg.AgentsDir, string(name))
}

func receiptFingerprint(receipt Receipt) string {
	canonical := string(receipt.Agent) + "\x00" + string(receipt.Role) + "\x00" + receipt.MCPEndpoint + "\x00" + receipt.TokenSHA256 + "\x00true"
	hash := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(hash[:])
}
