package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testset"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// criteriaKeys lists the acceptance criterion ids a submission covers.
func criteriaKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	return out
}

// expect is what the server checks a submission against in these tests (internal/transport/mcp/tools.go).
func expect() testset.Expect {
	return testset.Expect{TestsDir: testsDir, ACIDs: []string{"AC1", "AC2"}}
}

// TestTheScenarioTableHoldsTheCannedBehaviours pins the names a scripted run and the e2e stories call. The test-author
// names come first, then the reviewer names, then the resolver names.
func TestTheScenarioTableHoldsTheCannedBehaviours(t *testing.T) {
	want := []string{
		"valid", "missing-ac", "import-error", "outside-dir", "schema-bad", "slow", "silent",
		"review-clean", "review-finding", "review-bad-path", "review-silent",
		"verdict-uphold", "verdict-dismiss", "verdict-needs-human",
	}
	var got []string
	for _, s := range allScenarios() {
		got = append(got, s.name)
		require.NotEmpty(t, s.about, "scenario %s needs a description", s.name)
		require.NotNil(t, s.run, "scenario %s needs a run", s.name)
	}
	require.Equal(t, want, got)
	for _, name := range want {
		s, ok := scenarioFor(name)
		require.True(t, ok, "scenario %s is missing", name)
		require.Equal(t, name, s.name)
	}
	_, ok := scenarioFor("nope")
	require.False(t, ok)
}

// TestTheValidSubmissionPassesTheServersOwnRules is the strongest statement the canned "valid" output can make here:
// the same validator the MCP edge runs accepts it.
func TestTheValidSubmissionPassesTheServersOwnRules(t *testing.T) {
	sub := correctSubmission(testTask())
	require.Len(t, sub.Files, 1)
	require.Equal(t, testsDir+"/refund_over_limit_test.go", sub.Files[0].Path)
	require.NotEmpty(t, sub.Files[0].Content)
	require.ElementsMatch(t, []string{"AC1", "AC2"}, criteriaKeys(sub.AcceptanceCriteria))
	for _, ac := range []string{"AC1", "AC2"} {
		require.NotEmpty(t, sub.AcceptanceCriteria[ac])
	}
	require.Equal(t, []string{changedFile}, sub.ExpectedPaths)
	require.NoError(t, sub.Validate(expect()))
}

// TestTheMissingACSubmissionLeavesExactlyOneCriterionOut pins the failure the scenario exists for.
func TestTheMissingACSubmissionLeavesExactlyOneCriterionOut(t *testing.T) {
	built, err := buildMissingAC(testTask())
	require.NoError(t, err)
	sub, ok := built.(testset.Submission)
	require.True(t, ok, "buildMissingAC must send a TestSubmission, got %T", built)
	require.Len(t, sub.AcceptanceCriteria, 1)
	require.Contains(t, sub.AcceptanceCriteria, "AC1")
	require.NotContains(t, sub.AcceptanceCriteria, "AC2")
	err = sub.Validate(expect())
	require.ErrorContains(t, err, "acceptance_criteria[AC2]")
	require.NotContains(t, err.Error(), "acceptance_criteria[AC1]")
}

// TestTheMissingACSubmissionRefusesATaskWithNoCriterionToDrop reports a wrong setup instead of sending something else.
func TestTheMissingACSubmissionRefusesATaskWithNoCriterionToDrop(t *testing.T) {
	task := testTask()
	task.Inputs.AcceptanceCriteria = nil
	_, err := buildMissingAC(task)
	require.Error(t, err)
	require.Contains(t, err.Error(), "criterion")
}

// TestTheImportErrorSubmissionIsWellFormedButDoesNotBuild is the seam check's case: the MCP edge accepts the shape,
// the sandbox must reject the compile error. This agent does not pretend to run the check.
func TestTheImportErrorSubmissionIsWellFormedButDoesNotBuild(t *testing.T) {
	built, err := buildImportError(testTask())
	require.NoError(t, err)
	sub, ok := built.(testset.Submission)
	require.True(t, ok)
	require.NoError(t, sub.Validate(expect()), "the shape is valid; only a compiler rejects this file")
	require.Contains(t, sub.Files[0].Content, "NoSuchTestHelper")
	require.ElementsMatch(t, []string{"AC1", "AC2"}, criteriaKeys(sub.AcceptanceCriteria))
}

// TestTheOutsideDirSubmissionWritesOutsideTheTestsDirectory keeps the scenario's one fault isolated.
func TestTheOutsideDirSubmissionWritesOutsideTheTestsDirectory(t *testing.T) {
	built, err := buildOutsideDir(testTask())
	require.NoError(t, err)
	sub, ok := built.(testset.Submission)
	require.True(t, ok)
	require.Equal(t, outsideTestsFile, sub.Files[0].Path)
	require.False(t, strings.HasPrefix(sub.Files[0].Path, testsDir+"/"))
	err = sub.Validate(expect())
	require.ErrorContains(t, err, "is outside the tests directory "+testsDir)
	require.ElementsMatch(t, []string{"AC1", "AC2"}, criteriaKeys(sub.AcceptanceCriteria))
}

// TestTheSchemaBadSubmissionAddsOneFieldTheSchemaDoesNotHave keeps the scenario's one fault isolated.
func TestTheSchemaBadSubmissionAddsOneFieldTheSchemaDoesNotHave(t *testing.T) {
	valid := correctSubmission(testTask())
	raw, err := json.Marshal(valid)
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(raw, &fields))
	require.NotContains(t, fields, "ticket_key")

	built, err := buildSchemaBad(testTask())
	require.NoError(t, err)
	doc, ok := built.(map[string]any)
	require.True(t, ok, "buildSchemaBad must send a JSON object, got %T", built)
	require.Contains(t, doc, "ticket_key")
	for _, field := range []string{"files", "acceptance_criteria", "expected_paths", "notes"} {
		require.Contains(t, doc, field, "the schema-bad document keeps the real fields")
	}
}

// TestOneLineFlattensAMultiLineRefusal keeps a refusal to the single line a scripted run prints.
func TestOneLineFlattensAMultiLineRefusal(t *testing.T) {
	require.Equal(t, "The submission is not valid. Fix these problems: files[0].path is outside.",
		oneLine("The submission is not valid.\nFix these problems:\n\tfiles[0].path is outside.\n"))
	require.Empty(t, oneLine(" \n\t "))
}

// TestEveryScenarioRunsWithATaskWithEveryCriterion is a guard on the canned builders: a task without criteria is
// still a valid input for every builder except missing-ac.
func TestEveryScenarioBuilderWorksOnACriterionlessTask(t *testing.T) {
	task := testTask()
	task.Inputs.AcceptanceCriteria = nil
	for name, build := range map[string]submissionBuilder{
		"valid":        buildValid,
		"import-error": buildImportError,
		"outside-dir":  buildOutsideDir,
		"schema-bad":   buildSchemaBad,
	} {
		t.Run(name, func(t *testing.T) {
			built, err := build(task)
			require.NoError(t, err)
			require.NotNil(t, built)
		})
	}
}

// TestTheValidBuilderSendsEveryCriterionItIsGiven covers a ticket with a different criterion set.
func TestTheValidBuilderSendsEveryCriterionItIsGiven(t *testing.T) {
	task := testTask()
	task.Inputs.AcceptanceCriteria = []workflow.CriterionRef{
		task.Inputs.AcceptanceCriteria[0],
		{ID: "AC7", Ref: task.Inputs.AcceptanceCriteria[1].Ref},
	}
	built, err := buildValid(task)
	require.NoError(t, err)
	sub, ok := built.(testset.Submission)
	require.True(t, ok)
	require.ElementsMatch(t, []string{"AC1", "AC7"}, criteriaKeys(sub.AcceptanceCriteria))
}
