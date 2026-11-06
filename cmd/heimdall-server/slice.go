package main

import (
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/bundle"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/engine"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Step ids of the demo workflow: the ten steps from workflow.Default that have implementations.
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
	stepVerify     = "verify"
	stepDeliberate = "deliberate"
)

// sliceSteps returns the demo workflow and the engine steps that run it. The workflow is built from
// workflow.Default(), keeping the 10 implemented steps and trimming dependency edges of removed steps.
func sliceSteps(agents config.Agents, gen ports.IDs, clk ports.Clock) (workflow.Workflow, []engine.Step, error) {
	wf, m, err := demoWorkflow()
	if err != nil {
		return workflow.Workflow{}, nil, err
	}
	early, err := buildEarlyEngineSteps(m, agents, gen, clk)
	if err != nil {
		return workflow.Workflow{}, nil, err
	}
	late, err := buildLateEngineSteps(m, agents, gen, clk)
	if err != nil {
		return workflow.Workflow{}, nil, err
	}
	return wf, append(early, late...), nil
}

func demoWorkflow() (workflow.Workflow, map[string]workflow.Step, error) {
	full := workflow.Default()
	stepNames := []string{
		stepOpenRoom, stepBranch, stepWriteTests, stepTestQA, stepCommit,
		stepWaitPR, stepSlop, stepPick, stepReview, stepVerify, stepDeliberate,
	}
	m := make(map[string]workflow.Step, len(stepNames))
	steps := make([]workflow.Step, 0, len(stepNames))
	for _, name := range stepNames {
		s, err := stepByID(full, name)
		if err != nil {
			return workflow.Workflow{}, nil, err
		}
		if name == stepCommit {
			s.After = []string{stepTestQA}
		}
		m[name] = s
		steps = append(steps, s)
	}
	return workflow.Workflow{
		Version: full.Version,
		Steps:   steps,
	}, m, nil
}

func buildEarlyEngineSteps(m map[string]workflow.Step, agents config.Agents, gen ports.IDs, clk ports.Clock) ([]engine.Step, error) {
	room, err := engine.NewOpenRoom(m[stepOpenRoom].With)
	if err != nil {
		return nil, fmt.Errorf("step %s: %w", stepOpenRoom, err)
	}
	ensure, err := engine.NewEnsureBranch(m[stepBranch].With)
	if err != nil {
		return nil, fmt.Errorf("step %s: %w", stepBranch, err)
	}
	agentTask, err := engine.NewAgentTask(engine.AgentTaskSpec{
		ID:      m[stepWriteTests].ID,
		Role:    m[stepWriteTests].Role,
		Timeout: m[stepWriteTests].Timeout,
		Output:  change.EventTestsSubmitted,
		AgentFor: func(role workflow.Role) (id.AgentName, bool) {
			switch role {
			case workflow.RoleTestAuthor:
				return id.AgentName(agents.TestAuthor), true
			case workflow.RoleResolver:
				return id.AgentName(agents.Resolver), true
			case workflow.RoleReviewer, workflow.RoleProfiler:
				return "", false
			default:
				return "", false
			}
		},
		Clock: clk,
		IDs:   gen,
		Bundle: func(role workflow.Role, changeID id.ChangeID, events []change.Event) ([]byte, workflow.BlobRef, error) {
			built, err := bundle.BuildFromEvents(role, changeID, events)
			if err != nil {
				return nil, "", err
			}
			return built.Bytes, bundle.Hash(built), nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("step %s: %w", stepWriteTests, err)
	}
	tqa, err := engine.NewTestQA(m[stepTestQA].With)
	if err != nil {
		return nil, fmt.Errorf("step %s: %w", stepTestQA, err)
	}
	comm, err := engine.NewCommitTests(m[stepCommit].With)
	if err != nil {
		return nil, fmt.Errorf("step %s: %w", stepCommit, err)
	}
	return []engine.Step{room, ensure, agentTask, tqa, comm}, nil
}

func buildLateEngineSteps(m map[string]workflow.Step, agents config.Agents, gen ports.IDs, clk ports.Clock) ([]engine.Step, error) {
	wait, err := engine.NewWait(m[stepWaitPR].With)
	if err != nil {
		return nil, fmt.Errorf("step %s: %w", stepWaitPR, err)
	}
	sl := engine.NewSlop()
	pk, err := newPickReviewersStep(m[stepPick], agents, gen, clk)
	if err != nil {
		return nil, fmt.Errorf("step %s: %w", stepPick, err)
	}
	rev, err := engine.NewAgentFanout(engine.AgentFanoutSpec{
		ID:      m[stepReview].ID,
		Timeout: m[stepReview].Timeout,
		Clock:   clk,
		IDs:     gen,
		Bundle: func(role workflow.Role, changeID id.ChangeID, events []change.Event) ([]byte, workflow.BlobRef, error) {
			built, err := bundle.BuildFromEvents(role, changeID, events)
			if err != nil {
				return nil, "", err
			}
			return built.Bytes, bundle.Hash(built), nil
		},
		AgentFor: func(person change.ActorID) (id.AgentName, bool) {
			for _, r := range agents.Reviewers {
				if string(person) == r {
					return id.AgentName(r), true
				}
			}
			name, err := id.ParseAgentName(string(person))
			if err != nil {
				return "", false
			}
			return name, true
		},
	})
	if err != nil {
		return nil, fmt.Errorf("step %s: %w", stepReview, err)
	}
	verify, err := engine.NewVerifyEvidence(m[stepVerify].With)
	if err != nil {
		return nil, fmt.Errorf("step %s: %w", stepVerify, err)
	}
	resolver, err := newResolverFanoutStep(m[stepDeliberate], agents, gen, clk)
	if err != nil {
		return nil, fmt.Errorf("step %s: %w", stepDeliberate, err)
	}
	return []engine.Step{wait, sl, pk, rev, verify, resolver}, nil
}

// newPickReviewersStep builds the reviewer selection step, preferring explicit selections in the change and falling
// back to the configured reviewer agents.
func newPickReviewersStep(step workflow.Step, agents config.Agents, gen ports.IDs, clk ports.Clock) (engine.Step, error) {
	return engine.NewPickReviewers(engine.PickReviewersSpec{
		ID:      step.ID,
		Timeout: step.Timeout,
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
}

// newResolverFanoutStep builds the challenge resolver step, scoping each task to its case and bundle.
func newResolverFanoutStep(step workflow.Step, agents config.Agents, gen ports.IDs, clk ports.Clock) (engine.Step, error) {
	return engine.NewResolverFanout(step.ID, id.AgentName(agents.Resolver), step.Timeout, clk, gen,
		func(changeID id.ChangeID, caseID id.CaseID, events []change.Event) ([]byte, workflow.BlobRef, error) {
			built, err := bundle.BuildForCaseEvents(changeID, caseID, events)
			if err != nil {
				return nil, "", err
			}
			return built.Bytes, bundle.Hash(built), nil
		})
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

// sliceRoles lists the agent roles known to the demo workflow.
func sliceRoles() []workflow.Role {
	return []workflow.Role{workflow.RoleTestAuthor, workflow.RoleReviewer, workflow.RoleResolver}
}

// sliceSchemas lists the output schemas registered for the demo workflow.
func sliceSchemas() []string {
	return []string{"TestSubmission@1", "Review@1"}
}
