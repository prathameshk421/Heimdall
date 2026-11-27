package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// reviewTask is the envelope a reviewer gets for the fixed change: the same change as the test-author's, two changed
// files and the Review@1 schema.
func reviewTask() workflow.TaskEnvelope {
	task := testTask()
	task.Role = workflow.RoleReviewer
	task.Skill = "reviewer@1.0.0"
	task.OutputSchema = "Review@1"
	task.Inputs.FilesChanged = []string{"internal/refunds/service.go", "internal/api/refunds.go"}
	return task
}

// playReviewScenario runs one scenario against a world and returns the exit status and the two output streams.
func playReviewScenario(t *testing.T, w world, name string) (int, string, string) {
	t.Helper()
	return runScenario(t, w.hs.URL+"/mcp", writeToken(t), name)
}

// TestTheReviewSilentScenarioNeverAcks is the ack-window failure path for a reviewer.
func TestTheReviewSilentScenarioNeverAcks(t *testing.T) {
	w := buildWorld(t, workflow.RoleReviewer, reviewTask(), &testBlobs{})
	code, stdout, stderr := playReviewScenario(t, w, "review-silent")
	require.Equal(t, exitOK, code, "stderr: %s", stderr)
	require.Zero(t, w.tasks.acks, "review-silent must never acknowledge")
	require.Empty(t, w.reviews.reviews)
	require.Contains(t, stdout, "review-silent: no ack")
}

// TestAReviewerScenarioWithAResolverTokenIsRefused is the role mismatch: the server refuses the task, the refusal is
// printed as the outcome, and the run exits 0 with nothing acknowledged or submitted.
func TestAReviewerScenarioWithAResolverTokenIsRefused(t *testing.T) {
	for _, name := range []string{"review-clean", "review-finding"} {
		t.Run(name, func(t *testing.T) {
			w := buildWorld(t, workflow.RoleResolver, reviewTask(), &testBlobs{})
			code, stdout, stderr := playReviewScenario(t, w, name)
			require.Equal(t, exitOK, code, "stderr: %s", stderr)
			require.Equal(t, name+": refused: Your active task is not for your role.", strings.TrimSpace(stdout))
			require.Zero(t, w.tasks.acks)
			require.Empty(t, w.reviews.reviews)
		})
	}
}

// TestTheReviewScenariosNeverPrintTheToken uses the canary token: it must reach the server only as a hash and never
// appear in a line the agent prints.
func TestTheReviewScenariosNeverPrintTheToken(t *testing.T) {
	for _, name := range []string{"review-clean", "review-finding", "review-bad-path", "review-silent"} {
		t.Run(name, func(t *testing.T) {
			w := buildWorld(t, workflow.RoleReviewer, reviewTask(), &testBlobs{})
			code, stdout, stderr := playReviewScenario(t, w, name)
			require.Equal(t, exitOK, code, "stderr: %s", stderr)
			require.NotContains(t, stdout, testToken)
			require.NotContains(t, stderr, testToken)
			for _, seen := range w.tokens.seen {
				require.Equal(t, hashOf(testToken), seen, "the server must see the hash, never the token")
			}
		})
	}
}
