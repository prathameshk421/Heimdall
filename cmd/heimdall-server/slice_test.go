package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// The slice workflow is the first two steps of the default workflow, with the same with objects.
func TestSliceWorkflowIsTheFirstTwoStepsOfTheDefault(t *testing.T) {
	wf, _, err := sliceSteps()
	require.NoError(t, err)
	full := workflow.Default()
	require.Len(t, wf.Steps, 2)
	for i, s := range wf.Steps {
		require.Equal(t, full.Steps[i].ID, s.ID)
		require.Equal(t, full.Steps[i].Do, s.Do)
		require.JSONEq(t, string(full.Steps[i].With), string(s.With), "step %s keeps its with object", s.ID)
	}
}

// Every step of the slice has an engine implementation, and every implementation is used by a step of the slice.
func TestSliceWorkflowRunsExactlyTheRegisteredStepTypes(t *testing.T) {
	wf, steps, err := sliceSteps()
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
	require.Equal(t, map[string]bool{"switch.open_room": true, "github.ensure_branch": true}, used)
}
