//go:build component

package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// TestTheReviewFlowRunsFromPullRequestToMergeReady is B9 item 2 (HARNESS_DESIGN H10) on the real wiring. The pull
// request of KAN-1 opens; the profile check reports a lead; nobody picks reviewers, so the pick falls back to the three
// seats after the wait (fake clock). One seat runs the check with run_check and files an E2 finding that cites it; the
// server repeats the check and confirms E2, and the gate holds. The developer pushes a fix: the three tasks are
// superseded, the finding is checked again and resolves, and the seats review the new head. One seat stays silent: it is
// woken again, the provider is probed, the agent is reset, the task fails and is assigned again, and the new task is
// done. An approval from a non-author approver on the head makes the gate check green.
//
// A ticketless pull request cannot reach a green gate (the ticket_linked clause), so the flow runs on the ticket's own
// pull request; the tests half is the same journey as B9 item 1.
func TestTheReviewFlowRunsFromPullRequestToMergeReady(t *testing.T) {
	started := time.Now()
	r := newFlowRig(t)
	r.runTestsFlow()

	// The developer pushes the change with a bug the profile check reports on line 8, and opens the pull request.
	head1 := r.gh.push(map[string]string{flowProduct: flowProductFixed})
	r.runner.setFinding(head1, flowProduct+":8")
	r.gh.openPR()
	r.syncPRs()
	r.settle()
	v := r.view()
	require.True(t, v.Checks[head1].Ran, "the profile checks ran on the head")
	require.Nil(t, v.PR.Picked, "the author has the pick wait to choose reviewers")
	require.Zero(t, r.reviewTasks(v), "no review task before the pick")

	r.clock.advance(pickWait + time.Second)
	r.settle()
	v = r.view()
	require.NotNil(t, v.PR.Picked, "the pick falls back after the wait")
	require.Equal(t, 3, r.reviewTasks(v), "one review task per seat")

	// reviewer-1 runs the check and files the finding on its result; the other two seats review clean.
	task, _ := r.getTask(reviewerToken("reviewer-1"))
	require.Equal(t, "review", task.Task.Kind)
	require.Contains(t, task.input("leads"), "patterns", "the check's result is a lead")
	ran := r.call(reviewerToken("reviewer-1"), "run_check", map[string]any{"check_id": "patterns"})
	require.False(t, ran.IsError, ran.Text)
	require.Contains(t, ran.Text, flowProduct+":8")
	r.review("reviewer-1", head1, true)
	r.review("reviewer-2", head1, false)
	r.review("reviewer-3", head1, false)
	r.syncPRs()
	r.settle()
	v = r.view()
	require.Len(t, v.Findings, 1)
	for _, f := range v.Findings {
		require.Equal(t, finding.TierE2, f.Tier, "the repeated check confirms E2")
		require.True(t, f.Checked)
	}
	gate1 := r.gh.checksOn(head1)["heimdall/gate"]
	require.NotEqual(t, "success", gate1.Conclusion, "a confirmed E2 finding of medium severity holds the gate")
	require.Contains(t, gate1.Summary, "blocking finding")

	// A push: each seat reviews the new head. A second push lands while the three tasks are at work, so they are
	// superseded and assigned again on the newest head. The check reports nothing there.
	head2 := r.gh.push(map[string]string{flowProduct: flowProductFixed + "\n// todo: check the amount\n"})
	r.runner.setFinding(head2, flowProduct+":8")
	r.syncPRs()
	r.settle()
	require.Equal(t, 6, r.reviewTasks(r.view()), "each seat reviews the new head")
	head3 := r.gh.push(map[string]string{flowProduct: flowProductFixed + "\n// amount checked before the refund\n"})
	r.syncPRs()
	r.settle()
	v = r.view()
	require.Equal(t, head3, v.PR.HeadSHA)
	require.Equal(t, 3, r.count(change.EventTaskSuperseded), "the push supersedes the three tasks at work")
	require.Equal(t, 9, r.reviewTasks(v), "each seat reviews the newest head")
	r.review("reviewer-1", head3, false)
	r.review("reviewer-2", head3, false)

	// reviewer-3 is silent: woken again after the ack window, probed and reset after the stall window, failed after
	// another, and assigned again.
	r.clock.advance(ackWindow + time.Second)
	r.settle()
	require.Equal(t, 1, r.count(change.EventTaskRewoken), "the silent seat is woken again")
	r.clock.advance(stallWindow)
	r.settle()
	require.Equal(t, 1, r.model.probeCount(), "the provider is probed once")
	require.Equal(t, 2, r.count(change.EventTaskRewoken), "the silent agent is reset and woken")
	r.clock.advance(stallWindow + time.Second)
	r.settle()
	require.Equal(t, 1, r.count(change.EventTaskFailed), "the silent task fails")
	require.Equal(t, 10, r.reviewTasks(r.view()), "the seat gets a new task")
	r.review("reviewer-3", head3, false)
	r.syncPRs()
	r.settle()
	for _, f := range r.view().Findings {
		require.Equal(t, finding.StateResolved, f.State, "the check no longer reports the finding on the head; %s", r.state())
	}

	r.gh.approve(flowApprover)
	r.syncPRs()
	r.settle()
	r.syncPRs()
	r.settle()
	gate := r.gh.checksOn(head3)["heimdall/gate"]
	needs, err := r.app.reconciler.Needs(r.ctx, flowChange)
	require.NoError(t, err)
	require.Equal(t, "success", gate.Conclusion, "the gate is green on the head: %s; needs %v", gate.Summary, needs)
	require.Less(t, time.Since(started), 20*time.Second, "B9 item 2 runs in under 20 s")
}

// reviewTasks counts the review tasks ever assigned on the change.
func (r *flowRig) reviewTasks(v change.View) int {
	n := 0
	for _, task := range v.Tasks {
		if task.Kind == "review" {
			n++
		}
	}
	return n
}

// review plays one seat: get_task, then a review of head. withFinding files the E2 finding on the check's result.
func (r *flowRig) review(seat string, head id.SHA, withFinding bool) {
	t := r.t
	task, text := r.getTask(reviewerToken(seat))
	require.Equal(t, "review", task.Task.Kind, text)
	files := []string{flowProduct, flowTestsDir + "/refund_test.go"}
	review := finding.Review{HeadSHA: head, FilesReviewed: files, Clean: !withFinding, Findings: []finding.Input{}}
	if withFinding {
		loc := finding.Location{File: flowProduct, Lines: [2]int{8, 8}}
		review.Findings = append(review.Findings, finding.Input{
			Claim:    "The refund amount is compared after the payment is charged, so an over-refund is accepted.",
			Category: finding.CategoryCorrectness, Severity: finding.SeverityMedium, Confidence: finding.ConfidenceHigh,
			Location:    loc,
			Evidence:    []finding.Evidence{finding.NewToolResultEvidence(finding.ToolResult{CheckID: "patterns", Location: loc})},
			ContextRefs: []string{},
		})
	}
	res := r.call(reviewerToken(seat), "submit_review", review)
	require.False(t, res.IsError, "%s: %s", seat, res.Text)
}
