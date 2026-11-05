package ports

import "context"

// Sandbox runs one job on untrusted code (TECH_SPEC 12.2). Consumer: the red-first and repro flows. Adapter:
// internal/adapters/docker, which talks to heimdall-runner over its Unix socket (task T15; jobs T29).
type Sandbox interface {
	// Run returns the parsed JUnit summary; the full log is stored as a blob and referenced by LogRef.
	Run(ctx context.Context, j SandboxJob) (SandboxResult, error)
}
