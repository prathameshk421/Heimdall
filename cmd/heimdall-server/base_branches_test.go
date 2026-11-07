package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
)

// Each project's configured base branch reaches the GitHub adapter, keyed by its repository. Without it every change
// was cut from the default branch and the base branch's policy was never read.
func TestBaseBranchesFollowTheProjectMappings(t *testing.T) {
	cfg := config.Config{Projects: []config.Project{
		{TrackerProject: "KAN", Repo: "aarush323/heimdall-fixture-pay", BaseBranch: "heimdall-test/demo-base"},
		{TrackerProject: "PAY", Repo: "acme/pay", BaseBranch: "release"},
	}}
	require.Equal(t, map[change.Repo]string{
		"aarush323/heimdall-fixture-pay": "heimdall-test/demo-base",
		"acme/pay":                       "release",
	}, baseBranchesOf(cfg))
}
