package workflow_test

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// specRegistry is what the spec names: the step types of 5.1, the roles of 6.1 and the output schemas the
// steps use. The workflow package does not import the schema package, so the caller builds this.
func specRegistry() workflow.Registry {
	return workflow.Registry{
		StepTypes: []string{
			"switch.open_room", "github.ensure_branch", "agent.task", "sandbox.test_quality", "github.commit_tests",
			"wait", "rule.slop", "human.pick_reviewers", "agent.fanout", "sandbox.verify_evidence",
			"human.approve_tests", "sandbox.mutation", "deliberation.run", "rule.merge_allowed",
		},
		Roles:   []workflow.Role{workflow.RoleTestAuthor, workflow.RoleReviewer, workflow.RoleResolver},
		Schemas: []string{"TestSubmission@1", "Review@1"},
	}
}

func TestDefaultHasTheFourteenStepsOfTheSpecInOrder(t *testing.T) {
	w := workflow.Default()
	require.Equal(t, 1, w.Version)
	var ids []string
	for _, s := range w.Steps {
		ids = append(ids, s.ID)
	}
	require.Equal(t, []string{
		"open_room", "branch", "write_tests", "test_qa", "commit", "wait_pr", "slop", "pick",
		"review", "verify", "signoff", "mutation", "deliberate", "gate",
	}, ids)
}

func TestDefaultCarriesTheSpecsOrderingAndDeadlines(t *testing.T) {
	byID := map[string]workflow.Step{}
	for _, s := range workflow.Default().Steps {
		byID[s.ID] = s
	}
	require.Equal(t, []string{"test_qa", "signoff"}, byID["commit"].After)
	require.Equal(t, []string{"review"}, byID["mutation"].After)
	require.Equal(t, workflow.RoleTestAuthor, byID["write_tests"].Role)
	require.Equal(t, "TestSubmission@1", byID["write_tests"].Output)
	require.Equal(t, 2*time.Hour, byID["write_tests"].Timeout)
	require.Equal(t, 30*time.Minute, byID["review"].Timeout)
	require.Equal(t, time.Minute, byID["pick"].Timeout)
	require.Equal(t, "write_tests", byID["test_qa"].OnFail.Retry)
	require.Equal(t, "reject", byID["slop"].OnFail.Then)
	require.Equal(t, "any", byID["gate"].On)
}

func TestDefaultEncodesToTheGoldenJSONAndBack(t *testing.T) {
	out, err := json.Marshal(workflow.Default())
	require.NoError(t, err)
	want, err := os.ReadFile("testdata/default_workflow.golden.json")
	require.NoError(t, err)
	require.JSONEq(t, string(want), string(out), "golden JSON: the default workflow is frozen")

	var back workflow.Workflow
	require.NoError(t, json.Unmarshal(want, &back))
	require.NoError(t, back.Validate(specRegistry()))
}

// The default workflow must pass the same validation the spec requires of any workflow.
func TestDefaultValidatesWithTheSpecRegistry(t *testing.T) {
	require.NoError(t, workflow.Default().Validate(specRegistry()))
}
