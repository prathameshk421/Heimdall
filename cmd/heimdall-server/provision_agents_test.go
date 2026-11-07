package main

import (
	"testing"

	"github.com/stretchr/testify/require"

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
