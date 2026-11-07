package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/ids"
)

// The demo slice workflow has 11 implemented steps from workflow.Default.
func TestSliceWorkflowHasImplementedSteps(t *testing.T) {
	agents := config.Agents{
		TestAuthor: "test-author",
		Reviewers:  []string{"reviewer-1", "reviewer-2"},
		Resolver:   "resolver",
	}
	gen := ids.New()
	clk := clock.System{}
	wf, steps, err := sliceSteps(agents, gen, clk)
	require.NoError(t, err)
	require.Len(t, wf.Steps, 11)
	require.Len(t, steps, 11)
	require.Equal(t, "review", wf.Steps[8].ID)
	require.Equal(t, "verify", wf.Steps[9].ID)
	require.Equal(t, []string{"review"}, wf.Steps[9].After)
	require.Equal(t, "deliberate", wf.Steps[10].ID)
}

// Every step of the slice has an engine implementation, and every implementation is used by a step of the slice.
func TestSliceWorkflowRunsExactlyTheRegisteredStepTypes(t *testing.T) {
	agents := config.Agents{
		TestAuthor: "test-author",
		Reviewers:  []string{"reviewer-1", "reviewer-2"},
		Resolver:   "resolver",
	}
	gen := ids.New()
	clk := clock.System{}
	wf, steps, err := sliceSteps(agents, gen, clk)
	require.NoError(t, err)
	registered := map[string]bool{}
	for _, s := range steps {
		registered[s.Type()] = true
	}
	used := map[string]bool{}
	for _, s := range wf.Steps {
		used[s.Do] = true
	}
	require.Equal(t, used, registered)
}
