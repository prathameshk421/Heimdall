package ports

import (
	"context"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// Sandbox runs one job on untrusted code (TECH_SPEC 12.2). Consumer: the red-first and repro flows. Adapter:
// internal/adapters/docker, which talks to heimdall-runner over its Unix socket (task T15; jobs T29).
type Sandbox interface {
	// Run returns the parsed JUnit summary; the full log is stored as a blob and referenced by LogRef.
	Run(ctx context.Context, j SandboxJob) (SandboxResult, error)
}

// SandboxSuperseder tells the runner that a change has a new head. The runner cancels the queued and running
// background jobs of that change that served an older head and leaves every other job alone (TEST_AGENT_ARCH 8).
// Adapter: the runner client.
type SandboxSuperseder interface {
	Supersede(ctx context.Context, repo change.Repo, ch id.ChangeID, head id.SHA) error
}
