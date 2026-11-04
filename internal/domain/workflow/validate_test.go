package workflow_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// tiny is a two-step workflow that validates. Each table case breaks one rule of TECH_SPEC 5.1 in it.
func tiny() workflow.Workflow {
	return workflow.Workflow{Version: 1, Steps: []workflow.Step{
		{ID: "a", Do: "rule.slop"},
		{ID: "b", After: []string{"a"}, Do: "agent.task", Role: workflow.RoleReviewer, Output: "Review@1", Timeout: time.Hour},
	}}
}

func TestATinyWorkflowValidates(t *testing.T) {
	require.NoError(t, tiny().Validate(specRegistry()))
}

// Every refusal wraps ErrPolicyInvalid, so the caller can fail closed with the spec's word, and names the step.
func TestValidateRefusesEachBrokenRuleAndWrapsPolicyInvalid(t *testing.T) {
	cases := map[string]struct {
		mutate func(*workflow.Workflow)
		want   string
	}{
		"version 2":                   {func(w *workflow.Workflow) { w.Version = 2 }, "version must be 1"},
		"no steps":                    {func(w *workflow.Workflow) { w.Steps = nil }, "no steps"},
		"empty id":                    {func(w *workflow.Workflow) { w.Steps[0].ID = "" }, "id is empty"},
		"duplicate id":                {func(w *workflow.Workflow) { w.Steps[1].ID = "a"; w.Steps[1].After = nil }, `"a" is used twice`},
		"after names no step":         {func(w *workflow.Workflow) { w.Steps[1].After = []string{"ghost"} }, `after "ghost" names no step`},
		"a step after itself":         {func(w *workflow.Workflow) { w.Steps[0].After = []string{"a"} }, "cycle"},
		"a two-step cycle":            {func(w *workflow.Workflow) { w.Steps[0].After = []string{"b"} }, "cycle"},
		"unknown step type":           {func(w *workflow.Workflow) { w.Steps[0].Do = "shell.run" }, `unknown step type "shell.run"`},
		"unknown role":                {func(w *workflow.Workflow) { w.Steps[1].Role = "janitor" }, `role "janitor" is not known`},
		"unknown output schema":       {func(w *workflow.Workflow) { w.Steps[1].Output = "Verdict@9" }, `output "Verdict@9" is not a registered schema`},
		"timeout negative":            {func(w *workflow.Workflow) { w.Steps[1].Timeout = -time.Minute }, "timeout must not be negative"},
		"timeout past the bound":      {func(w *workflow.Workflow) { w.Steps[1].Timeout = 25 * time.Hour }, "timeout must be at most"},
		"on_fail then is unknown":     {func(w *workflow.Workflow) { w.Steps[0].OnFail = &workflow.OnFail{Then: "shrug"} }, `on_fail.then "shrug"`},
		"on_fail retry names no step": {func(w *workflow.Workflow) { w.Steps[0].OnFail = &workflow.OnFail{Retry: "ghost"} }, `on_fail.retry "ghost" names no step`},
		"retry to a step that is not earlier": {func(w *workflow.Workflow) {
			w.Steps[0].OnFail = &workflow.OnFail{Retry: "b"}
		}, "on_fail.retry must point at a step this one comes after"},
		"with is not an object": {func(w *workflow.Workflow) { w.Steps[0].With = json.RawMessage(`[1]`) }, "with must be a JSON object"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := tiny()
			c.mutate(&w)
			err := w.Validate(specRegistry())
			require.Error(t, err)
			require.ErrorIs(t, err, workflow.ErrPolicyInvalid)
			require.Contains(t, err.Error(), c.want)
		})
	}
}

func TestTimeoutAtTheBoundPasses(t *testing.T) {
	w := tiny()
	w.Steps[1].Timeout = 24 * time.Hour
	require.NoError(t, w.Validate(specRegistry()))
}

func TestAStepWithNoTimeoutIsValid(t *testing.T) {
	w := tiny()
	w.Steps[1].Timeout = 0
	require.NoError(t, w.Validate(specRegistry()))
}

func TestARetryToAnEarlierStepIsAllowedAndAForwardRetryIsNot(t *testing.T) {
	w := workflow.Workflow{Version: 1, Steps: []workflow.Step{
		{ID: "write", Do: "agent.task", Role: workflow.RoleTestAuthor},
		{ID: "check", After: []string{"write"}, Do: "sandbox.test_quality", OnFail: &workflow.OnFail{Retry: "write", Then: "escalate"}},
	}}
	require.NoError(t, w.Validate(specRegistry()))

	w.Steps[0].OnFail = &workflow.OnFail{Retry: "check"}
	require.Error(t, w.Validate(specRegistry()))
}

// A cycle is refused, and the check is a real topological sort: a long chain that closes on itself is found.
func TestALongCycleIsFoundAndADiamondIsNot(t *testing.T) {
	diamond := workflow.Workflow{Version: 1, Steps: []workflow.Step{
		{ID: "a", Do: "rule.slop"},
		{ID: "b", After: []string{"a"}, Do: "rule.slop"},
		{ID: "c", After: []string{"a"}, Do: "rule.slop"},
		{ID: "d", After: []string{"b", "c"}, Do: "rule.slop"},
	}}
	require.NoError(t, diamond.Validate(specRegistry()))

	ring := workflow.Workflow{Version: 1, Steps: []workflow.Step{
		{ID: "a", After: []string{"c"}, Do: "rule.slop"},
		{ID: "b", After: []string{"a"}, Do: "rule.slop"},
		{ID: "c", After: []string{"b"}, Do: "rule.slop"},
		{ID: "d", Do: "rule.slop"},
	}}
	err := ring.Validate(specRegistry())
	require.Error(t, err)
	require.Contains(t, err.Error(), "cycle")
}

func TestErrorsNameEveryProblemNotOnlyTheFirst(t *testing.T) {
	w := tiny()
	w.Version = 9
	w.Steps[0].Do = "nope"
	err := w.Validate(specRegistry())
	require.Error(t, err)
	require.Contains(t, err.Error(), "version must be 1")
	require.Contains(t, err.Error(), `unknown step type "nope"`)
}
