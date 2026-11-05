package ports

import (
	"context"

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
