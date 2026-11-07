package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
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

// TestTheReviewScenariosRecordAReviewOnTheRealServer plays the clean and the finding review against the real MCP edge.
// Each one reads the diff, acknowledges, and reads every changed file at head before it submits.
func TestTheReviewScenariosRecordAReviewOnTheRealServer(t *testing.T) {
	cases := []struct {
		name         string
		wantClean    bool
		wantFindings int
	}{
		{name: "review-clean", wantClean: true, wantFindings: 0},
		{name: "review-finding", wantClean: false, wantFindings: 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			task := reviewTask()
			w := buildWorld(t, workflow.RoleReviewer, task, &testBlobs{})
			code, stdout, stderr := playReviewScenario(t, w, c.name)
			require.Equal(t, exitOK, code, "stderr: %s", stderr)
			require.Contains(t, stdout, c.name+": recorded: The review is recorded.")
			require.Len(t, w.reviews.reviews, 1)
			rev := w.reviews.reviews[0]
			require.Equal(t, c.wantClean, rev.Clean)
			require.Len(t, rev.Findings, c.wantFindings)
			require.Equal(t, testHead, rev.HeadSHA, "the review names the head commit, never the base")
			require.ElementsMatch(t, task.Inputs.FilesChanged, rev.FilesReviewed, "a review lists every file it read")
			require.NoError(t, rev.Validate(), "the scripted review passes the domain's own rules")
			require.Equal(t, 1, w.tasks.acks, "the reviewer acknowledges once before it submits")
			require.Contains(t, w.blobs.reads, task.Inputs.Diff, "the reviewer reads the diff through read_untrusted")
			for _, file := range task.Inputs.FilesChanged {
				require.Contains(t, w.code.reads, string(testHead)+" "+file, "the reviewer reads %s at head", file)
			}
		})
	}
}

// TestTheReviewFindingCitesARealLineRangeAndToolEvidence checks the finding against the file it names: a path from the
// envelope's changed files, a range inside the file and tool evidence at the same location.
func TestTheReviewFindingCitesARealLineRangeAndToolEvidence(t *testing.T) {
	task := reviewTask()
	w := buildWorld(t, workflow.RoleReviewer, task, &testBlobs{})
	code, _, stderr := playReviewScenario(t, w, "review-finding")
	require.Equal(t, exitOK, code, "stderr: %s", stderr)
	require.Len(t, w.reviews.reviews, 1)
	f := w.reviews.reviews[0].Findings[0]
	require.Equal(t, task.Inputs.FilesChanged[0], f.Location.File)
	first, last := f.Location.Lines[0], f.Location.Lines[1]
	require.GreaterOrEqual(t, first, 1)
	require.LessOrEqual(t, first, last)
	require.LessOrEqual(t, last, 3, "the fixed head file has three lines, so a range past line 3 does not exist")
	require.Len(t, f.Evidence, 1)
	require.Equal(t, finding.EvidenceToolResult, f.Evidence[0].Type, "the finding carries E2 tool evidence")
	require.NotNil(t, f.Evidence[0].ToolResult)
	require.Equal(t, f.Location, f.Evidence[0].ToolResult.Location, "the tool reports the location the finding names")
}

// TestTheBadPathReviewIsReturnedWithItsIndexAndThenCorrected is the round trip: the first review names a file outside
// the diff, the server refuses it with the finding's index, and the agent submits a corrected review that is recorded.
func TestTheBadPathReviewIsReturnedWithItsIndexAndThenCorrected(t *testing.T) {
	task := reviewTask()
	w := buildWorld(t, workflow.RoleReviewer, task, &testBlobs{})
	code, stdout, stderr := playReviewScenario(t, w, "review-bad-path")
	require.Equal(t, exitOK, code, "stderr: %s", stderr)
	require.Len(t, w.reviews.reviews, 2, "one refused review and one corrected review")
	first, second := w.reviews.reviews[0], w.reviews.reviews[1]
	require.False(t, slices.Contains(task.Inputs.FilesChanged, first.Findings[0].Location.File),
		"the first finding names a path outside the diff")
	require.Equal(t, task.Inputs.FilesChanged[0], second.Findings[0].Location.File,
		"the corrected finding names a changed file")
	require.NoError(t, second.Validate())

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	require.Len(t, lines, 2, "stdout: %q", stdout)
	require.Contains(t, lines[0], "review-bad-path: refused: ")
	require.Contains(t, lines[0], "finding 0:", "the refusal names the index of the bad finding")
	require.Equal(t, "review-bad-path: recorded: The review is recorded.", lines[1])
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
