package change

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

func TestTheRequestPredicates(t *testing.T) {
	t.Parallel()
	submitted := View{Tests: &TestsInfo{Rejected: []string{}}}
	rejected := View{Tests: &TestsInfo{Rejected: []string{"red first failed"}}}
	committed := View{Tests: &TestsInfo{Rejected: []string{}, Committed: &TestsCommittedInfo{}}}
	needsSignoff := View{Tests: &TestsInfo{Verified: &TestsVerifiedInfo{NeedsSignoff: true}}}
	signedOff := View{Tests: &TestsInfo{Verified: &TestsVerifiedInfo{NeedsSignoff: true}, ApprovedBy: "rekdfr"}}
	noSignoff := View{Tests: &TestsInfo{Verified: &TestsVerifiedInfo{}}}
	withPR := View{PR: &PRInfo{Number: 10}}
	merged := View{PR: &PRInfo{Number: 10}, Closed: &ClosedInfo{Merged: true}}
	rejectedChange := View{Rejected: &RejectedInfo{By: "operator"}}
	task := View{Tasks: map[id.TaskID]TaskInfo{"tsk_1": {}}}

	tests := []struct {
		name string
		err  error
		ok   bool
	}{
		{"first tests", CanSubmitTests(View{}), true},
		{"tests again while the first are pending", CanSubmitTests(submitted), false},
		{"tests again after a rejection", CanSubmitTests(rejected), true},
		{"tests after they are committed", CanSubmitTests(committed), false},
		{"tests on a rejected change", CanSubmitTests(rejectedChange), false},
		{"sign-off with nothing verified", CanApproveTests(View{}), false},
		{"sign-off of tests that need it", CanApproveTests(needsSignoff), true},
		{"a second sign-off", CanApproveTests(signedOff), false},
		{"sign-off nobody needs", CanApproveTests(noSignoff), false},
		{"a review with no pull request", CanReview(View{}), false},
		{"a review of an open pull request", CanReview(withPR), true},
		{"a review after the merge", CanReview(merged), false},
		{"progress on an assigned task", CanReportOnTask(task, "tsk_1"), true},
		{"progress on an unknown task", CanReportOnTask(task, "tsk_2"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.ok {
				require.NoError(t, tt.err)
				return
			}
			require.ErrorIs(t, tt.err, ErrRefused)
		})
	}
}

func TestCanAppendLetsFactsThroughAndRefusesAnyRequestOnAnEndedChange(t *testing.T) {
	t.Parallel()
	open := View{PR: &PRInfo{Number: 10}}
	require.NoError(t, CanAppend(open, Event{Type: EventCIReported}), "a fact is never refused")
	require.ErrorIs(t, CanAppend(View{Rejected: &RejectedInfo{}}, Event{Type: EventQuestionAsked}), ErrRefused)
	require.ErrorIs(t, CanAppend(View{}, Event{Type: EventReviewSubmitted}), ErrRefused)
}
