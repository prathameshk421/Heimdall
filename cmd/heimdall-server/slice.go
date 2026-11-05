package main

import (
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/engine"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Step ids of the demo workflow: the nine steps from workflow.Default that have implementations.
const (
	stepOpenRoom   = "open_room"
	stepBranch     = "branch"
	stepWriteTests = "write_tests"
	stepTestQA     = "test_qa"
	stepCommit     = "commit"
	stepWaitPR     = "wait_pr"
	stepSlop       = "slop"
	stepPick       = "pick"
	stepReview     = "review"
)

// sliceSteps returns the demo workflow and the engine steps that run it. The workflow is built from
// workflow.Default(), keeping the 9 implemented steps and trimming dependency edges of removed steps.
func sliceSteps(agents config.Agents, gen ports.IDs, clk ports.Clock) (workflow.Workflow, []engine.Step, error) {
	full := workflow.Default()

	openRoom, err := stepByID(full, stepOpenRoom)
	if err != nil {
		return workflow.Workflow{}, nil, err
	}
	branch, err := stepByID(full, stepBranch)
	if err != nil {
		return workflow.Workflow{}, nil, err
	}
	writeTests, err := stepByID(full, stepWriteTests)
	if err != nil {
		return workflow.Workflow{}, nil, err
	}
	testQA, err := stepByID(full, stepTestQA)
	if err != nil {
		return workflow.Workflow{}, nil, err
	}
	commit, err := stepByID(full, stepCommit)
	if err != nil {
		return workflow.Workflow{}, nil, err
	}
	// Trim after chain: signoff is not implemented, so commit only depends on test_qa
	commit.After = []string{stepTestQA}

	waitPR, err := stepByID(full, stepWaitPR)
	if err != nil {
		return workflow.Workflow{}, nil, err
	}
	slop, err := stepByID(full, stepSlop)
	if err != nil {
		return workflow.Workflow{}, nil, err
	}
	pick, err := stepByID(full, stepPick)
	if err != nil {
		return workflow.Workflow{}, nil, err
	}
	review, err := stepByID(full, stepReview)
	if err != nil {
		return workflow.Workflow{}, nil, err
	}

	room, err := engine.NewOpenRoom(openRoom.With)
	if err != nil {
		return workflow.Workflow{}, nil, fmt.Errorf("step %s: %w", stepOpenRoom, err)
	}
	ensure, err := engine.NewEnsureBranch(branch.With)
	if err != nil {
		return workflow.Workflow{}, nil, fmt.Errorf("step %s: %w", stepBranch, err)
	}
	agentTask, err := engine.NewAgentTask(engine.AgentTaskSpec{
		ID:      writeTests.ID,
		Role:    writeTests.Role,
		Timeout: writeTests.Timeout,
		Output:  change.EventTestsSubmitted,
		AgentFor: func(role workflow.Role) (id.AgentName, bool) {
			switch role {
			case workflow.RoleTestAuthor:
				return id.AgentName(agents.TestAuthor), true
			case workflow.RoleResolver:
				return id.AgentName(agents.Resolver), true
			default:
				return "", false
			}
		},
		Clock: clk,
		IDs:   gen,
	})
	if err != nil {
		return workflow.Workflow{}, nil, fmt.Errorf("step %s: %w", stepWriteTests, err)
	}
	tqa, err := engine.NewTestQA(testQA.With)
	if err != nil {
		return workflow.Workflow{}, nil, fmt.Errorf("step %s: %w", stepTestQA, err)
	}
	comm, err := engine.NewCommitTests(commit.With)
	if err != nil {
		return workflow.Workflow{}, nil, fmt.Errorf("step %s: %w", stepCommit, err)
	}
	wait, err := engine.NewWait(waitPR.With)
	if err != nil {
		return workflow.Workflow{}, nil, fmt.Errorf("step %s: %w", stepWaitPR, err)
	}
	sl := engine.NewSlop()
	pk, err := engine.NewPickReviewers(engine.PickReviewersSpec{
		ID:      pick.ID,
		Timeout: pick.Timeout,
		Clock:   clk,
		IDs:     gen,
		DefaultReviewers: func(_ engine.ChangeView) []change.ActorID {
			var out []change.ActorID
			for _, r := range agents.Reviewers {
				out = append(out, change.ActorID(r))
			}
			return out
		},
	})
	if err != nil {
		return workflow.Workflow{}, nil, fmt.Errorf("step %s: %w", stepPick, err)
	}
	rev, err := engine.NewAgentFanout(engine.AgentFanoutSpec{
		ID:      review.ID,
		Timeout: review.Timeout,
		Clock:   clk,
		IDs:     gen,
		AgentFor: func(person change.ActorID) (id.AgentName, bool) {
			for _, r := range agents.Reviewers {
				if string(person) == r {
					return id.AgentName(r), true
				}
			}
			return id.ParseAgentName(string(person))
		},
	})
	if err != nil {
		return workflow.Workflow{}, nil, fmt.Errorf("step %s: %w", stepReview, err)
	}

	wf := workflow.Workflow{
		Version: full.Version,
		Steps: []workflow.Step{
			openRoom, branch, writeTests, testQA, commit, waitPR, slop, pick, review,
		},
	}
	steps := []engine.Step{
		room, ensure, agentTask, tqa, comm, wait, sl, pk, rev,
	}
	return wf, steps, nil
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
