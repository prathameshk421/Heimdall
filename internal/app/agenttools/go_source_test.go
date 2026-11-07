package agenttools

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testset"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

const goodTestFile = "package heimdall_test\n\nimport \"testing\"\n\nfunc TestRefundIsCreated(t *testing.T) {\n\tt.Fatal(\"no refund endpoint\")\n}\n"

func goSubmission(content string, names ...string) testset.Submission {
	return testset.Submission{
		Files:              []testset.File{{Path: "tests/heimdall/KAN-9/refunds_test.go", Content: content}},
		AcceptanceCriteria: map[string][]string{"AC1": names},
	}
}

var kan9Inputs = workflow.Inputs{AcceptanceCriteria: []workflow.CriterionRef{{ID: "AC1"}}}

// Found live on KAN-9: the test-author submitted a .go file that was one long header comment with no package clause and
// no tests. It passed every check, then failed to build in the sandbox, which is not a valid red. The agent must get
// the problem back from submit_tests, where it can still fix it.
func TestAGoFileThatDoesNotParseIsRefusedWithItsParseError(t *testing.T) {
	err := validateSubmissionExpect("KAN-9", kan9Inputs, goSubmission("// Ticket: KAN-9, a header comment and nothing else", "TestRefundIsCreated"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "files[0].content is not valid Go")
	require.Contains(t, err.Error(), "expected 'package'")
}

// Every test named in acceptance_criteria must be declared in the submitted files, so the coverage map cannot point at
// tests that do not exist.
func TestATestNamedForACriterionMustBeDeclaredInTheFiles(t *testing.T) {
	err := validateSubmissionExpect("KAN-9", kan9Inputs, goSubmission(goodTestFile, "TestRefundIsCreated", "TestNobodyWroteThis"))
	require.Error(t, err)
	require.Contains(t, err.Error(), `acceptance_criteria[AC1] names "TestNobodyWroteThis", which no submitted file declares`)
}

// A valid Go file that declares every named test passes, and a file that is not Go is not parsed.
func TestValidGoAndNonGoFilesPass(t *testing.T) {
	sub := goSubmission(goodTestFile, "TestRefundIsCreated")
	sub.Files = append(sub.Files, testset.File{Path: "tests/heimdall/KAN-9/testdata/refund.json", Content: `{"amount": 10}`})
	require.NoError(t, validateSubmissionExpect("KAN-9", kan9Inputs, sub))
}

// Found live on KAN-9: the agent sent the whole file on one line (newlines turned into spaces), so it read as one long
// comment. "expected 'package'" did not tell it why; the refusal names the missing line breaks.
func TestAOneLineGoFileIsRefusedWithAHintAboutLineBreaks(t *testing.T) {
	flat := `// Ticket: KAN-9 header. package heimdall_test import "testing" func TestRefundIsCreated(t *testing.T) {}`
	err := validateSubmissionExpect("KAN-9", kan9Inputs, goSubmission(flat, "TestRefundIsCreated"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "has no line breaks")
}
