package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testset"
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

// TestTheMissingACSubmissionRefusesATaskWithNoCriterionToDrop reports a wrong setup instead of sending something else.
func TestTheMissingACSubmissionRefusesATaskWithNoCriterionToDrop(t *testing.T) {
	task := testTask()
	task.Inputs.AcceptanceCriteria = nil
	_, err := buildMissingAC(task)
	require.Error(t, err)
	require.Contains(t, err.Error(), "criterion")
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

// TestOneLineFlattensAMultiLineRefusal keeps a refusal to the single line a scripted run prints.
func TestOneLineFlattensAMultiLineRefusal(t *testing.T) {
	require.Equal(t, "The submission is not valid. Fix these problems: files[0].path is outside.",
		oneLine("The submission is not valid.\nFix these problems:\n\tfiles[0].path is outside.\n"))
	require.Empty(t, oneLine(" \n\t "))
}
