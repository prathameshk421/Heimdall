package main

import (
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/engine"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// Step ids of the slice. They name the first two steps of workflow.Default.
const (
	stepOpenRoom = "open_room"
	stepBranch   = "branch"
)

// sliceSteps returns the workflow of the slice and the engine steps that run it. The workflow is the first two steps
// of workflow.Default, copied with their with objects. It grows as each step lands: a step joins it when its engine
// implementation is registered here. engine.New refuses a workflow with a step type that has no implementation, so
// the list cannot run ahead of the code.
func sliceSteps() (workflow.Workflow, []engine.Step, error) {
	full := workflow.Default()
	openRoom, err := stepByID(full, stepOpenRoom)
	if err != nil {
		return workflow.Workflow{}, nil, err
	}
	branch, err := stepByID(full, stepBranch)
	if err != nil {
		return workflow.Workflow{}, nil, err
	}
	wf := workflow.Workflow{Version: full.Version, Steps: []workflow.Step{openRoom, branch}}

	room, err := engine.NewOpenRoom(openRoom.With)
	if err != nil {
		return workflow.Workflow{}, nil, fmt.Errorf("step %s: %w", stepOpenRoom, err)
	}
	ensure, err := engine.NewEnsureBranch(branch.With)
	if err != nil {
		return workflow.Workflow{}, nil, fmt.Errorf("step %s: %w", stepBranch, err)
	}
	return wf, []engine.Step{room, ensure}, nil
}

// stepByID returns the step of w with the id, copied. A missing id is an error, so a change to the default workflow
// fails loudly here rather than running a different slice.
func stepByID(w workflow.Workflow, id string) (workflow.Step, error) {
	for _, s := range w.Steps {
		if s.ID == id {
			return s, nil
		}
	}
	return workflow.Step{}, fmt.Errorf("workflow.Default has no step %q", id)
}
