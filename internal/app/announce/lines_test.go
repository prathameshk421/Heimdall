package announce_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/announce"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

const key = id.ChangeID("KAN-14")

func ev(t *testing.T, typ change.EventType, payload any) change.Event {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	return change.Event{ID: change.EventID("ev-" + string(typ)), ChangeID: key, Type: typ, Payload: raw}
}

func narrate(t *testing.T, history []change.Event, e change.Event) (announce.Line, bool) {
	t.Helper()
	return announce.Narrate(history, e)
}

// Every line is plain text a person can read in Discord, Telegram or Mattermost: it names the ticket, says what
// happened and, when a person must act, what to type. No ids, no jargon, no markup.
func TestTestLines(t *testing.T) {
	branch := ev(t, change.EventBranchEnsured, change.BranchEnsured{Repo: "aarush323/heimdall-fixture-pay", Branch: "KAN-14-add-a-refund-count", BaseSHA: "a984a21aef9173c85c505ba0e730e23769f92995"})

	cases := []struct {
		name    string
		history []change.Event
		event   change.Event
		want    string
	}{
		{"test author starts", nil, ev(t, change.EventTaskAssigned, change.TaskAssigned{Agent: "test-author", Role: workflow.RoleTestAuthor}),
			"🧪 KAN-14 is in progress. The Heimdall Test Agent is writing failing tests for the ticket's acceptance criteria."},
		{"tests submitted", nil, ev(t, change.EventTestsSubmitted, change.TestsSubmitted{Refs: []string{"tests/heimdall/KAN-14/a_test.go"}, ACMap: map[string][]string{"AC2": {"TestB"}, "AC1": {"TestA"}}}),
			"🔎 KAN-14: tests submitted (1 file, covering AC1 and AC2). I'm running them on the current code to check that they fail for the right reason."},
		{"tests rejected for a build failure", nil, ev(t, change.EventTestsRejected, change.TestsRejected{Reasons: []string{"[build failed] failed with compile or setup error on base"}}),
			"❌ KAN-14: these tests did not pass my check. They did not compile on the current code. The Test Agent will try again."},
		{"tests rejected because they pass already", nil, ev(t, change.EventTestsRejected, change.TestsRejected{Reasons: []string{"TestA passed without the feature on base"}}),
			"❌ KAN-14: these tests did not pass my check. TestA already passes without the new feature, so it proves nothing. The Test Agent will try again."},
		{"tests verified", nil, ev(t, change.EventTestsVerified, change.TestsVerified{}),
			"✅ KAN-14: tests verified. They fail on the current code, as they should, and gave the same result on every run."},
		{"tests verified, a person must approve", nil, ev(t, change.EventTestsVerified, change.TestsVerified{NeedsSignoff: true}),
			"⏸ KAN-14: tests verified. A person has to approve them before I commit. Reply: heimdall approve-tests"},
		{"tests committed", []change.Event{branch}, ev(t, change.EventTestsCommitted, change.TestsCommitted{SHA: "dd3c35c3aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}),
			"✅ KAN-14: tests committed to branch KAN-14-add-a-refund-count (commit dd3c35c)."},
		{"step stuck", nil, ev(t, change.EventStepEscalated, change.StepEscalated{Step: "write-tests", Reason: "three attempts were rejected"}),
			"⚠️ KAN-14: I'm stuck and need a person. three attempts were rejected. When it is fixed, reply: heimdall resume write-tests"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := narrate(t, tc.history, tc.event)
			require.True(t, ok)
			require.Equal(t, announce.AudienceTests, got.Audience)
			require.Equal(t, tc.want, got.Text)
		})
	}
}

func TestReviewLines(t *testing.T) {
	filed := func(ref id.FindingRef, sev string) change.Event {
		return ev(t, change.EventFindingFiled, change.FindingFiled{Ref: ref, Severity: finding.Severity(sev)})
	}
	cases := []struct {
		name    string
		history []change.Event
		event   change.Event
		want    string
	}{
		{"pull request opened", nil, ev(t, change.EventPROpened, change.PROpened{Number: 5}),
			"📬 KAN-14: pull request #5 is open. I'm waiting for CI and the reviewers."},
		{"ci passed", nil, ev(t, change.EventCIReported, change.CIReported{Check: "ci", SHA: "abcdef1234567", Conclusion: "success"}),
			"✅ KAN-14: CI passed on abcdef1."},
		{"ci failed", nil, ev(t, change.EventCIReported, change.CIReported{Check: "ci", SHA: "abcdef1234567", Conclusion: "failure"}),
			"❌ KAN-14: CI failed on abcdef1 (check: ci)."},
		{"reviewers picked", nil, ev(t, change.EventReviewersPicked, change.ReviewersPicked{People: []change.ActorID{"ana", "bob"}}),
			"🔍 KAN-14: reviewers picked: ana and bob."},
		{"review with findings", []change.Event{filed(1, "high"), filed(2, "low")},
			ev(t, change.EventReviewSubmitted, change.ReviewSubmitted{Reviewer: "reviewer-1", FindingRefs: []id.FindingRef{1, 2}}),
			"🔍 KAN-14: Heimdall Code Reviewer 1 finished the review and found 2 issues: F1 (high) and F2 (low)."},
		{"review with no findings", nil, ev(t, change.EventReviewSubmitted, change.ReviewSubmitted{Reviewer: "reviewer-1"}),
			"🔍 KAN-14: Heimdall Code Reviewer 1 finished the review and found no issues."},
		{"finding upheld", nil, ev(t, change.EventFindingStateChanged, change.FindingStateChanged{Ref: 1, State: finding.StateUpheld, By: "prathameshk"}),
			"⚖️ KAN-14: F1 was upheld by prathameshk. It must be fixed before merge."},
		{"finding dismissed", nil, ev(t, change.EventFindingStateChanged, change.FindingStateChanged{Ref: 2, State: finding.StateDismissed, By: "prathameshk"}),
			"⚖️ KAN-14: F2 was dismissed by prathameshk."},
		{"approval", nil, ev(t, change.EventApprovalRecorded, change.ApprovalRecorded{Login: "rekdfr"}),
			"✅ KAN-14: rekdfr approved the pull request."},
		{"gate passed", nil, ev(t, change.EventGateEvaluated, change.GateEvaluated{OK: true}),
			"✅ KAN-14: the merge check passed. This change is ready to merge."},
		{"gate blocked", nil, ev(t, change.EventGateEvaluated, change.GateEvaluated{OK: false, Reasons: []string{"1 upheld finding is open", "no approval on the latest commit"}}),
			"⛔ KAN-14: the merge check is blocked.\n• 1 upheld finding is open\n• no approval on the latest commit"},
		{"merged", nil, ev(t, change.EventChangeMerged, struct{}{}),
			"🎉 KAN-14 is merged."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := narrate(t, tc.history, tc.event)
			require.True(t, ok)
			require.Equal(t, announce.AudienceReview, got.Audience)
			require.Equal(t, tc.want, got.Text)
		})
	}
}

// Events that need no announcement, and a second assignment of the same role (the retry already said so), stay quiet.
func TestQuietEvents(t *testing.T) {
	_, ok := narrate(t, nil, ev(t, change.EventTaskProgress, struct{}{}))
	require.False(t, ok)
	first := ev(t, change.EventTaskAssigned, change.TaskAssigned{Agent: "test-author", Role: workflow.RoleTestAuthor})
	_, ok = narrate(t, []change.Event{first}, ev(t, change.EventTaskAssigned, change.TaskAssigned{Agent: "test-author", Role: workflow.RoleTestAuthor}))
	require.False(t, ok, "a retried task is not announced twice")
}
