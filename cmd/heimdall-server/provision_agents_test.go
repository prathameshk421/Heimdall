package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
)

// Each role agent gets a description naming it, so two reviewers are two definitions in Switch and a definition that
// already exists under that name matches.
func TestProvisionAgentsDescribeEachAgentByName(t *testing.T) {
	got := provisionAgents(config.Agents{TestAuthor: "test-author", Reviewers: []string{"reviewer-1", "reviewer-2"}, Resolver: "resolver"})
	described := map[string]string{}
	for _, a := range got {
		described[string(a.Name)] = a.Description
	}
	require.Equal(t, map[string]string{
		"test-author": "Heimdall test-author",
		"reviewer-1":  "Heimdall reviewer-1",
		"reviewer-2":  "Heimdall reviewer-2",
		"resolver":    "Heimdall resolver",
	}, described)
}

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
