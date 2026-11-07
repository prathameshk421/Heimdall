package mcp

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// Found live on KAN-9: the app refused a .go file that does not parse, but the agent saw only the generic failure and
// resubmitted the same file. A problem the app finds in the submission is shown to the agent like any other refusal.
func TestAnAppValidationProblemIsShownToTheAgent(t *testing.T) {
	r := newRig(t, workflow.RoleTestAuthor)
	r.tasks.submitErr = fmt.Errorf("%w: files[0].content is not valid Go: 1:1414: expected 'package', found 'EOF'", ErrInvalidSubmission)
	res := r.call("submit_tests", validSubmission())
	require.True(t, res.IsError)
	require.Contains(t, res.text(), "files[0].content is not valid Go")
	require.Contains(t, res.text(), "Fix these problems and submit again")
}

// Any other failure inside the server stays generic, so no internal detail reaches the agent.
func TestAnyOtherSubmitFailureStaysGeneric(t *testing.T) {
	r := newRig(t, workflow.RoleTestAuthor)
	r.tasks.submitErr = fmt.Errorf("postgres: connection refused at 10.0.0.5")
	res := r.call("submit_tests", validSubmission())
	require.True(t, res.IsError)
	require.Equal(t, genericFailure, res.text())
}
