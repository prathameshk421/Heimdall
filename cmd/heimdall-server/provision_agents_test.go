package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
)

// A configured profiler is provisioned with its role and holds the profile tasks (D175); without one, no agent does.
func TestTheProfilerIsProvisionedAndHoldsProfileTasks(t *testing.T) {
	agents := config.Agents{TestAuthor: "test-author", Reviewers: []string{"reviewer-1"}, Resolver: "resolver", Profiler: "profiler"}
	roles := map[string]workflow.Role{}
	for _, a := range provisionAgents(agents) {
		roles[string(a.Name)] = a.Role
	}
	require.Equal(t, workflow.RoleProfiler, roles["profiler"])

	name, ok := agentFor(agents)("profile", "aarush323/heimdall-fixture-pay")
	require.True(t, ok)
	require.Equal(t, id.AgentName("profiler"), name)

	agents.Profiler = ""
	require.Len(t, provisionAgents(agents), 3, "no profiler, no profiler agent")
	_, ok = agentFor(agents)("profile", "aarush323/heimdall-fixture-pay")
	require.False(t, ok)
}
