package change_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

const (
	blobA = "blob:sha256:00000000000000000000000000000000000000000000000000000000000000aa"
	blobB = "blob:sha256:00000000000000000000000000000000000000000000000000000000000000bb"
)

var t0 = time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)

func settings() change.Settings {
	return change.Settings{TestAttempts: 2, PickWait: time.Minute, Reviewers: []change.ActorID{"rev-a", "rev-b"}}
}

func inOf(c string, v change.View, now time.Time) change.In {
	return change.In{Change: id.ChangeID(c), View: v, Settings: settings(), Now: now,
		Policy: policy.Policy{People: map[string]policy.Person{"ann": {GitHub: "ann"}}, Approvers: map[string][]string{"core": {"bob"}}}}
}

func triggered(t *testing.T) change.Event {
	return ev(t, 1, change.EventChangeTriggered, change.Triggered{Ticket: "KAN-5", Repo: "acme/pay", Assignee: "ann", Source: "jira"})
}

func submission(t *testing.T, n int, blob workflow.BlobRef) change.Event {
	return ev(t, n, change.EventTestsSubmitted, change.TestsSubmitted{ACMap: map[string][]string{"AC1": {"T"}},
		Files: []change.TestFile{{Path: "tests/heimdall/KAN-5/a_test.go", Blob: blob}}}, blob)
}

func actionsOf(in change.In) map[string][]change.Action {
	out := map[string][]change.Action{}
	for _, ra := range change.Next(in) {
		out[ra.Row] = append(out[ra.Row], ra.Action)
	}
	return out
}

func TestRowsStartOnlyWhenTheirFlowHas(t *testing.T) {
	require.Empty(t, change.Next(inOf("KAN-5", fold(t), t0)), "no fact yet: nothing is open")

	v := fold(t, triggered(t))
	got := actionsOf(inOf("KAN-5", v, t0))
	require.Contains(t, got, "open_room")
	require.NotContains(t, got, "branch", "branch waits for the room")
	for _, review := range []string{"slop", "pick", "gate"} {
		require.NotContains(t, got, review, "the Review flow has not started")
	}

	v = fold(t, triggered(t), ev(t, 2, change.EventRoomOpened, change.RoomOpened{RoomID: "switch:r1"}))
	require.Contains(t, actionsOf(inOf("KAN-5", v, t0)), "branch")
}

func TestOpenRoomInvitesTheTestAuthorAssigneeAndPRAuthor(t *testing.T) {
	v := fold(t, triggered(t), opened(t)) // opened(t) is event 1 too; version numbers do not matter to the fold
	room := actionsOf(inOf("KAN-5", v, t0))["open_room"][0].Job
	require.Equal(t, change.EffectRoomOpen, room.Kind)
	require.Equal(t, change.EffectKey("KAN-5/room/r0"), room.Key)
	require.Equal(t, []string{"role:test-author", "person:ann"}, room.Payload.(change.RoomOpenPayload).Invite)
}

func TestTestQAKeyNamesTheLatestSubmission(t *testing.T) {
	v := fold(t, triggered(t), ev(t, 2, change.EventRoomOpened, change.RoomOpened{RoomID: "switch:r1"}),
		ev(t, 3, change.EventBranchEnsured, change.BranchEnsured{Repo: "acme/pay", Branch: "KAN-5-x", BaseSHA: fBase}),
		submission(t, 4, blobA),
		ev(t, 5, change.EventTestsRejected, change.TestsRejected{Reasons: []string{"flaky"}, SHA: fHead}),
		submission(t, 6, blobB))
	qa := actionsOf(inOf("KAN-5", v, t0))["test_qa"]
	require.Len(t, qa, 1)
	require.Equal(t, change.EffectKey("KAN-5/test_qa/"+blobB+"/r0"), qa[0].Job.Key, "a resubmission is verified, not skipped")
	require.Equal(t, workflow.BlobRef(blobB), qa[0].Job.Payload.(change.TestQualityPayload).SubmissionRef)
	require.NotContains(t, actionsOf(inOf("KAN-5", v, t0)), "write_tests", "the resubmission is not rejected")
}

func TestWriteTestsRetriesWithANewInputThenAsksPeopleAndResumeAllowsMore(t *testing.T) {
	base := []change.Event{triggered(t), ev(t, 2, change.EventRoomOpened, change.RoomOpened{RoomID: "switch:r1"}),
		ev(t, 3, change.EventBranchEnsured, change.BranchEnsured{Repo: "acme/pay", Branch: "KAN-5-x", BaseSHA: fBase})}
	rej := func(n int) change.Event {
		return ev(t, n, change.EventTestsRejected, change.TestsRejected{Reasons: []string{"too weak"}, SHA: fHead})
	}
	first := actionsOf(inOf("KAN-5", fold(t, base...), t0))["write_tests"][0]
	require.Equal(t, change.Task{Kind: "tests", Subject: "KAN-5", Input: "attempt-1"}, *first.Task)

	one := append(append([]change.Event{}, base...), submission(t, 4, blobA), rej(5))
	retry := actionsOf(inOf("KAN-5", fold(t, one...), t0))["write_tests"][0]
	require.Equal(t, "attempt-2", retry.Task.Input, "the rejection reasons reach the agent through get_task")

	two := append(append([]change.Event{}, one...), submission(t, 6, blobB), rej(7))
	ask := actionsOf(inOf("KAN-5", fold(t, two...), t0))["write_tests"][0]
	require.Nil(t, ask.Task, "the budget is used up")
	require.NotNil(t, ask.Ask)
	require.Equal(t, []change.ActorID{"bob"}, ask.Ask.Who)
	require.Contains(t, ask.Text, "rejected 2 times")
	require.Contains(t, ask.Text, "`heimdall resume KAN-5 write_tests`")

	resumed := append(append([]change.Event{}, two...), ev(t, 8, change.EventStepResumed, change.StepResumed{Step: "write_tests", By: "ann"}))
	again := actionsOf(inOf("KAN-5", fold(t, resumed...), t0))["write_tests"][0]
	require.NotNil(t, again.Task, "one resume allows the same number of attempts again")
	require.Equal(t, "attempt-3", again.Task.Input)
}

func TestResumeRearmsAJobWithANewKey(t *testing.T) {
	evs := []change.Event{triggered(t), ev(t, 2, change.EventRoomOpened, change.RoomOpened{RoomID: "switch:r1"}),
		ev(t, 3, change.EventBranchEnsured, change.BranchEnsured{Repo: "acme/pay", Branch: "KAN-5-x", BaseSHA: fBase}),
		submission(t, 4, blobA),
		ev(t, 5, change.EventTestsVerified, change.TestsVerified{RunIDs: []id.RunID{"run_01HZX3K9QW7M2P5R8T0V4YB6CD"}, SHA: fHead, TreeHash: fBase})}
	key := func(evs []change.Event) change.EffectKey {
		return actionsOf(inOf("KAN-5", fold(t, evs...), t0))["commit"][0].Job.Key
	}
	require.True(t, strings.HasSuffix(string(key(evs)), "/r0"))
	evs = append(evs, ev(t, 6, change.EventStepResumed, change.StepResumed{Step: "commit", By: "ann"}))
	require.True(t, strings.HasSuffix(string(key(evs)), "/r1"))
}

func TestCommitFilesComeFromTheSubmission(t *testing.T) {
	v := fold(t, triggered(t), submission(t, 2, blobA),
		ev(t, 3, change.EventRoomOpened, change.RoomOpened{RoomID: "switch:r1"}),
		ev(t, 4, change.EventBranchEnsured, change.BranchEnsured{Repo: "acme/pay", Branch: "KAN-5-x", BaseSHA: fBase}),
		ev(t, 5, change.EventTestsVerified, change.TestsVerified{RunIDs: []id.RunID{"run_01HZX3K9QW7M2P5R8T0V4YB6CD"}, SHA: fHead, TreeHash: fBase}))
	p := actionsOf(inOf("KAN-5", v, t0))["commit"][0].Job.Payload.(change.CommitTestsPayload)
	require.Equal(t, "tests/heimdall/KAN-5", p.Dir)
	require.Equal(t, []change.CommitFile{{Path: "tests/heimdall/KAN-5/a_test.go", BlobRef: blobA}}, p.Files)
	require.NoError(t, p.Validate())
}

// The Review flow needs nothing from the Tests flow (HARNESS_DESIGN B1): a PR that names no ticket is reviewed.
func TestATicketlessPullRequestRunsTheReviewFlow(t *testing.T) {
	v := fold(t, opened(t), ev(t, 2, change.EventSlopEvaluated, change.SlopEvaluated{Pass: true}))
	in := inOf("pr-acme-pay-7", v, t0.Add(time.Second))
	in.Profile = change.Profile{Checks: []change.ProfileCheck{{ID: "lint", When: []string{"**/*.go"}}}}
	in.Outside = change.Outside{ChangedFiles: []string{"api/x.go"}}
	got := actionsOf(in)
	for _, row := range []string{"open_room", "checks", "pick"} {
		require.Contains(t, got, row, row)
	}
	var open []string
	for _, r := range change.Open(in) {
		open = append(open, r.ID)
	}
	require.Equal(t, []string{"open_room", "checks", "pick", "gate"}, open, "the gate row is open as long as the PR is")
	require.NotContains(t, got, "acceptance", "no ticket, so no acceptance tests to run")
	for _, row := range []string{"branch", "write_tests", "test_qa", "signoff", "commit"} {
		require.NotContains(t, got, row, row)
	}
	require.Equal(t, change.RunChecksPayload{Repo: "acme/pay", Change: "pr-acme-pay-7", Head: fHead, Base: fBase, CheckIDs: []string{"lint"}},
		got["checks"][0].Job.Payload)
}

func TestChecksIsDoneWhenNoCheckAppliesOrTheyRan(t *testing.T) {
	v := fold(t, opened(t), ev(t, 2, change.EventSlopEvaluated, change.SlopEvaluated{Pass: true}))
	in := inOf("pr-acme-pay-7", v, t0)
	in.Profile = change.Profile{Checks: []change.ProfileCheck{{ID: "lint", When: []string{"**/*.go"}}}}
	in.Outside = change.Outside{ChangedFiles: []string{"README.md"}}
	require.NotContains(t, actionsOf(in), "checks", "no changed file matches")

	in.Outside = change.Outside{ChangedFiles: []string{"a.go"}}
	require.Contains(t, actionsOf(in), "checks")
	v = fold(t, opened(t), ev(t, 2, change.EventSlopEvaluated, change.SlopEvaluated{Pass: true}),
		ev(t, 3, change.EventChecksReported, change.ChecksReported{Head: fHead}))
	in.View = v
	require.NotContains(t, actionsOf(in), "checks")
}

func TestPickAsksTheAuthorThenFallsBackToSuggestionsThenToSettings(t *testing.T) {
	v := fold(t, opened(t), ev(t, 2, change.EventSlopEvaluated, change.SlopEvaluated{Pass: true})) // slop at 09:02
	slopAt := time.Date(2026, 11, 4, 9, 2, 0, 0, time.UTC)

	before := actionsOf(inOf("pr-acme-pay-7", v, slopAt.Add(30*time.Second)))["pick"][0]
	require.NotNil(t, before.Ask)
	require.Equal(t, []change.ActorID{"ann"}, before.Ask.Who)
	require.Contains(t, before.Ask.What, "heimdall review")

	after := actionsOf(inOf("pr-acme-pay-7", v, slopAt.Add(time.Minute)))["pick"][0]
	require.Equal(t, change.ReviewersPicked{People: []change.ActorID{"rev-1"}, By: "heimdall", Method: change.PickDefault}, after.Fact)

	bare := fold(t, ev(t, 1, change.EventPROpened, change.PROpened{Repo: "acme/pay", Number: 7, HeadSHA: fHead, Author: "ann"}),
		ev(t, 2, change.EventSlopEvaluated, change.SlopEvaluated{Pass: true}))
	fallback := actionsOf(inOf("pr-acme-pay-7", bare, slopAt.Add(time.Hour)))["pick"][0]
	require.Equal(t, []change.ActorID{"rev-a", "rev-b"}, fallback.Fact.(change.ReviewersPicked).People)
}

func TestReviewWantsEverySeatAgainOnANewHead(t *testing.T) {
	picked := ev(t, 3, change.EventReviewersPicked, change.ReviewersPicked{People: []change.ActorID{"rev-1", "rev-2", "rev-3"}, By: "heimdall", Method: change.PickDefault})
	base := []change.Event{opened(t), ev(t, 2, change.EventSlopEvaluated, change.SlopEvaluated{Pass: true}), picked,
		ev(t, 4, change.EventChecksReported, change.ChecksReported{Head: fHead})}
	got := actionsOf(inOf("pr-acme-pay-7", fold(t, base...), t0))["review"]
	require.Len(t, got, 3)
	require.Equal(t, change.Task{Kind: "review", Subject: "rev-1", Input: "head-" + fHead}, *got[0].Task)

	pushed := append(append([]change.Event{}, base...),
		ev(t, 5, change.EventReviewSubmitted, change.ReviewSubmitted{Reviewer: "rev-1", PackDate: "2026-11-04"}),
		ev(t, 6, change.EventReviewUnavailable, change.ReviewUnavailable{Reviewer: "rev-2", Reason: "silent"}),
		ev(t, 7, change.EventPRPushed, change.PRPushed{Repo: "acme/pay", Number: 7, HeadSHA: fHead2}),
		ev(t, 8, change.EventSlopEvaluated, change.SlopEvaluated{Pass: true}))
	in := inOf("pr-acme-pay-7", fold(t, pushed...), t0)
	in.Profile = change.Profile{}
	got = actionsOf(in)["review"]
	require.Len(t, got, 3, "a new head voids every outcome")
	for _, a := range got {
		require.Equal(t, "head-"+fHead2, a.Task.Input)
	}
}

func TestVerifyIsDueForEachUncheckedProvableFindingAndAgainAfterARecheck(t *testing.T) {
	rec := change.FindingRecord{FindingID: "fnd_01HZX3K9QW7M2P5R8T0V4YB6CD", Ref: 1, Tier: "E1", Severity: "high", State: "open", By: "rev-1"}
	picked := ev(t, 3, change.EventReviewersPicked, change.ReviewersPicked{People: []change.ActorID{"rev-1"}, By: "heimdall", Method: change.PickDefault})
	evs := []change.Event{opened(t), ev(t, 2, change.EventSlopEvaluated, change.SlopEvaluated{Pass: true}), picked,
		ev(t, 4, change.EventChecksReported, change.ChecksReported{Head: fHead}),
		ev(t, 5, change.EventReviewSubmitted, change.ReviewSubmitted{Reviewer: "rev-1", PackDate: "2026-11-04"}),
		ev(t, 6, change.EventFindingFiled, withRepro(rec))}
	job := func(evs []change.Event) *change.Job {
		return actionsOf(inOf("pr-acme-pay-7", fold(t, evs...), t0))["verify"][0].Job
	}
	require.Equal(t, change.EffectKey("pr-acme-pay-7/verify/F1/"+fHead+"/k0/r0"), job(evs).Key)
	p := job(evs).Payload.(change.VerifyEvidencePayload)
	require.Equal(t, "TestRepro", p.TestName)
	require.NotEmpty(t, p.TestSource)
	require.NoError(t, p.Validate())

	evs = append(evs, ev(t, 7, change.EventFindingVerified, rec))
	require.NotContains(t, actionsOf(inOf("pr-acme-pay-7", fold(t, evs...), t0)), "verify")
	evs = append(evs, ev(t, 8, change.EventFindingRecheckRequested, change.FindingRecheckRequested{FindingID: rec.FindingID, Ref: 1, Reason: "wrong answer", By: "ann"}))
	require.Equal(t, change.EffectKey("pr-acme-pay-7/verify/F1/"+fHead+"/k1/r0"), job(evs).Key)
}

func withRepro(r change.FindingRecord) change.FindingRecord {
	r.ReproTest = &finding.ReproTest{Path: "tests/r_test.go", Name: "TestRepro", Content: "package r"}
	return r
}

func TestDeadJobsAreNeedsWithTheResumeLine(t *testing.T) {
	v := fold(t, triggered(t))
	in := inOf("KAN-5", v, t0)
	in.Dead = []change.DeadJob{{Key: "KAN-5/room/r0", Kind: change.EffectRoomOpen, Attempts: 8, LastError: "switch is down"}}
	needs := change.Missing(in)
	last := needs[len(needs)-1]
	require.Equal(t, "open_room", last.Step)
	require.Equal(t, "room_open failed 8 times: switch is down; `heimdall resume KAN-5 open_room` runs it again", last.Text)
}

func TestNextIsDeterministic(t *testing.T) {
	v := fold(t, opened(t), ev(t, 2, change.EventSlopEvaluated, change.SlopEvaluated{Pass: true}))
	a := change.Next(inOf("pr-acme-pay-7", v, t0.Add(time.Hour)))
	b := change.Next(inOf("pr-acme-pay-7", v, t0.Add(2*time.Hour)))
	require.True(t, reflect.DeepEqual(a, b), "no action holds the clock")
}

func TestAnOverChangeHasNoOpenRow(t *testing.T) {
	v := fold(t, triggered(t), ev(t, 2, change.EventChangeRejected, change.Rejected{Reason: "no"}))
	require.Empty(t, change.Next(inOf("KAN-5", v, t0)))
}
