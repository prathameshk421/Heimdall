package ports

import (
	"context"
	"fmt"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// AgentRuntime wakes agents and asks whether they are reachable (TECH_SPEC 6.8, 6.9). Consumer: the task engine.
// Adapter: internal/adapters/switchroom (task T26; the runtime itself is T27).
type AgentRuntime interface {
	// Wake sends the task to the agent. The envelope carries references only, never untrusted text (TECH_SPEC 6.3).
	Wake(ctx context.Context, a AgentRef, t workflow.TaskEnvelope) (WakeResult, error)
	// Liveness reports whether the agent can take work now.
	Liveness(ctx context.Context, a AgentRef) (Liveness, error)
}

// AgentProvisioner creates a managed agent definition on an external controller.
// It does not own or supervise the process; Switch's controller remains authoritative.
type AgentProvisioner interface {
	EnsureManagedAgent(ctx context.Context, spec ManagedAgentSpec) (ManagedAgent, error)
	SetManagedAgentState(ctx context.Context, agentID string, state ManagedAgentState) error
}

// AgentReceiptLedger records accepted host setup receipts by content fingerprint only.
// Implementations must never receive or persist the bearer token value.
type AgentReceiptLedger interface {
	RecordAgentSetupReceipt(ctx context.Context, agent id.AgentName, fingerprint string) (bool, error)
	LoadAgentSetupReceipt(ctx context.Context, agent id.AgentName) (AgentSetupReceipt, bool, error)
}

// AgentSetupReceipt is the non-secret, durable record of a host credential installation receipt.
type AgentSetupReceipt struct {
	Agent       id.AgentName
	Fingerprint string
}

// ManagedAgentSpec is the non-secret definition Switch needs to enroll an agent.
// MCP credentials are installed separately by the host configuration workflow.
type ManagedAgentSpec struct {
	Agent        AgentRef
	Description  string
	Machine      string
	Provider     string
	Model        string
	Instructions string
	Directory    string
}

// Validate refuses incomplete or unsafe Switch managed-agent definitions.
func (s ManagedAgentSpec) Validate() error {
	if err := s.Agent.Validate(); err != nil {
		return fmt.Errorf("agent: %w", err)
	}
	var problems []string
	if strings.TrimSpace(s.Description) == "" || len(s.Description) > 512 {
		problems = append(problems, "description must be 1 to 512 bytes")
	}
	if strings.TrimSpace(s.Machine) == "" || len(s.Machine) > 128 {
		problems = append(problems, "machine must be 1 to 128 bytes")
	}
	if s.Provider != "opencode" {
		problems = append(problems, "provider must be opencode")
	}
	if strings.TrimSpace(s.Model) == "" || len(s.Model) > 256 {
		problems = append(problems, "model must be 1 to 256 bytes")
	}
	if len(s.Instructions) > 32<<10 {
		problems = append(problems, "instructions must not exceed 32768 bytes")
	}
	if len(s.Directory) > 4096 {
		problems = append(problems, "directory must not exceed 4096 bytes")
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

// ManagedAgent is the Switch identity returned after creating a stopped managed agent.
type ManagedAgent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ManagedAgentState is the desired controller state, never an observed process state.
type ManagedAgentState string

// Desired controller states for managed agents.
const (
	ManagedAgentRunning ManagedAgentState = "running"
	ManagedAgentStopped ManagedAgentState = "stopped"
)

// Valid reports whether state is a supported desired state.
func (s ManagedAgentState) Valid() bool {
	return s == ManagedAgentRunning || s == ManagedAgentStopped
}
