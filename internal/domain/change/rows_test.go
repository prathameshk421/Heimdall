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

// A job carries every value the executor needs, so the executor reads nothing from the log: the base the tests must
// fail on, the head they must pass on, and whether a finding was verified on an earlier head.
func TestJobsCarryWhatTheirExecutorsNeed(t *testing.T) {
	v := fold(t, triggered(t), ev(t, 2, change.EventRoomOpened, change.RoomOpened{RoomID: "switch:r1"}),
		ev(t, 3, change.EventBranchEnsured, change.BranchEnsured{Repo: "acme/pay", Branch: "KAN-5-x", BaseSHA: fBase}),
		submission(t, 4, blobA), opened(t))
	qa := actionsOf(inOf("KAN-5", v, t0))["test_qa"][0].Job
	p := qa.Payload.(change.TestQualityPayload)
	require.Equal(t, id.SHA(fBase), p.BaseSHA)
	require.Equal(t, id.SHA(fHead), p.HeadSHA, "tests written while a PR exists are judged on its head")
	require.Contains(t, string(qa.Key), fHead, "a new head is a new job")

	rec := change.FindingRecord{FindingID: "fnd_01HZX3K9QW7M2P5R8T0V4YB6CD", Ref: 1, Tier: "E1", Severity: "high", State: "open", By: "rev-1"}
	review := []change.Event{opened(t), ev(t, 2, change.EventSlopEvaluated, change.SlopEvaluated{Pass: true}),
		ev(t, 3, change.EventReviewersPicked, change.ReviewersPicked{People: []change.ActorID{"rev-1"}, By: "heimdall", Method: change.PickDefault}),
		ev(t, 4, change.EventChecksReported, change.ChecksReported{Head: fHead}),
		ev(t, 5, change.EventReviewSubmitted, change.ReviewSubmitted{Reviewer: "rev-1", PackDate: "2026-11-04"}),
		ev(t, 6, change.EventFindingFiled, withRepro(rec)), ev(t, 7, change.EventFindingVerified, rec),
		ev(t, 8, change.EventPRPushed, change.PRPushed{Repo: "acme/pay", Number: 7, HeadSHA: fHead2}),
		ev(t, 9, change.EventSlopEvaluated, change.SlopEvaluated{Pass: true}),
		ev(t, 10, change.EventReviewSubmitted, change.ReviewSubmitted{Reviewer: "rev-1", PackDate: "2026-11-04"})}
	verify := actionsOf(inOf("pr-acme-pay-7", fold(t, review...), t0))["verify"][0].Job.Payload.(change.VerifyEvidencePayload)
	require.True(t, verify.Reverify, "verified on the first head, so this is a re-run")
	require.Equal(t, id.SHA(fBase), verify.BaseSHA)
}

// Every job a row starts is of a kind the row declares, and every declared kind is started somewhere, so the start-up
// check that each declared kind has an executor covers every job the rows can start (HARNESS_DESIGN B8).
func TestEachRowDeclaresTheJobKindsItStarts(t *testing.T) {
	rec := change.FindingRecord{FindingID: "fnd_01HZX3K9QW7M2P5R8T0V4YB6CD", Ref: 1, Tier: "E1", Severity: "high", State: "open", By: "rev-1"}
	tests := []change.Event{triggered(t), ev(t, 2, change.EventRoomOpened, change.RoomOpened{RoomID: "switch:r1"}),
		ev(t, 3, change.EventBranchEnsured, change.BranchEnsured{Repo: "acme/pay", Branch: "KAN-5-x", BaseSHA: fBase}),
		submission(t, 4, blobA),
		ev(t, 5, change.EventTestsVerified, change.TestsVerified{RunIDs: []id.RunID{"run_01HZX3K9QW7M2P5R8T0V4YB6CD"}, SHA: fHead, TreeHash: fBase}),
		opened(t), ev(t, 7, change.EventSlopEvaluated, change.SlopEvaluated{Pass: true})}
	review := []change.Event{opened(t), ev(t, 2, change.EventSlopEvaluated, change.SlopEvaluated{Pass: true}),
		ev(t, 3, change.EventReviewersPicked, change.ReviewersPicked{People: []change.ActorID{"rev-1"}, By: "heimdall", Method: change.PickDefault}),
		ev(t, 4, change.EventChecksReported, change.ChecksReported{Head: fHead}),
		ev(t, 5, change.EventReviewSubmitted, change.ReviewSubmitted{Reviewer: "rev-1", PackDate: "2026-11-04"}),
		ev(t, 6, change.EventFindingFiled, withRepro(rec))}
	declared := map[string][]change.EffectKind{}
	for _, r := range change.Rows() {
		declared[r.ID] = r.Jobs
	}
	seen := map[change.EffectKind]bool{}
	for _, flow := range []struct {
		change string
		events []change.Event
	}{{"KAN-5", tests}, {"pr-acme-pay-7", review}} {
		for n := 1; n <= len(flow.events); n++ {
			in := inOf(flow.change, fold(t, flow.events[:n]...), t0)
			in.Profile = change.Profile{Checks: []change.ProfileCheck{{ID: "lint"}}}
			in.Outside = change.Outside{ChangedFiles: []string{"a.go"}}
			for _, ra := range change.Next(in) {
				if ra.Job == nil {
					continue
				}
				require.Contains(t, declared[ra.Row], ra.Job.Kind, "row %s starts a %s job it does not declare", ra.Row, ra.Job.Kind)
				seen[ra.Job.Kind] = true
			}
		}
	}
	for _, k := range change.RowJobKinds() {
		require.True(t, seen[k], "no row started a declared %s job", k)
	}
}

// A policy that cannot be used stops every row, and Missing names it in plain words instead of the rows' needs.
func TestAPolicyProblemOpensNoRowAndIsTheOneNeed(t *testing.T) {
	v, err := change.Fold([]change.Event{triggered(t)})
	require.NoError(t, err)
	in := inOf("KAN-5", v, t0)
	require.NotEmpty(t, change.Next(in), "the room is needed while the policy is fine")
	in.Policy = policy.Policy{}
	in.PolicyProblem = "heimdall.yaml: version 3 is not known"
	require.Empty(t, change.Next(in), "no row runs on a policy the server cannot read")
	needs := change.Missing(in)
	require.Len(t, needs, 1)
	require.Equal(t, "policy", needs[0].Step)
	require.Equal(t, "heimdall.yaml cannot be used (heimdall.yaml: version 3 is not known); nothing runs on this change until the policy is fixed on the base branch", needs[0].Text)
}

// A seat whose task failed twice on the same input is not assigned again: each round costs model tokens, and a seat
// that always fails (H11, D6) would loop forever. A person decides with `heimdall resume`, which allows two more.
func TestASeatThatFailedTwiceOnTheSameInputIsNotAssignedAgainUntilResumed(t *testing.T) {
	picked := ev(t, 3, change.EventReviewersPicked, change.ReviewersPicked{People: []change.ActorID{"rev-1"}, By: "heimdall", Method: change.PickDefault})
	base := []change.Event{opened(t), ev(t, 2, change.EventSlopEvaluated, change.SlopEvaluated{Pass: true}), picked,
		ev(t, 4, change.EventChecksReported, change.ChecksReported{Head: fHead})}
	fail := func(n int, task string) []change.Event {
		return []change.Event{
			ev(t, n, change.EventTaskAssigned, change.TaskAssigned{TaskID: tk(task), Agent: "rev-1", Role: "reviewer",
				Deadline: time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC), Kind: "review", Subject: "rev-1", Input: "head-" + fHead}),
			ev(t, n+1, change.EventTaskFailed, change.TaskFailed{TaskID: tk(task), Agent: "rev-1", Role: "reviewer", Reason: "unresponsive"}),
		}
	}
	once := append(append([]change.Event{}, base...), fail(5, "tsk_01KANRY0000000000000000001")...)
	require.NotNil(t, actionsOf(inOf("pr-acme-pay-7", fold(t, once...), t0))["review"][0].Task, "one failure is retried")

	twice := append(append([]change.Event{}, once...), fail(7, "tsk_01KANRY0000000000000000002")...)
	stopped := actionsOf(inOf("pr-acme-pay-7", fold(t, twice...), t0))["review"][0]
	require.Nil(t, stopped.Task, "two failures on the same input stop the seat")
	require.NotNil(t, stopped.Ask)
	require.Equal(t, "rev-1 failed 2 review tasks on "+fHead[:7]+"; `heimdall resume pr-acme-pay-7 review` tries again", stopped.Text)

	resumed := append(append([]change.Event{}, twice...), ev(t, 9, change.EventStepResumed, change.StepResumed{Step: "review", By: "ann"}))
	require.NotNil(t, actionsOf(inOf("pr-acme-pay-7", fold(t, resumed...), t0))["review"][0].Task, "a resume allows the seat again")

	pushed := append(append([]change.Event{}, twice...),
		ev(t, 9, change.EventPRPushed, change.PRPushed{Repo: "acme/pay", Number: 7, HeadSHA: fHead2}),
		ev(t, 10, change.EventSlopEvaluated, change.SlopEvaluated{Pass: true}),
		ev(t, 11, change.EventChecksReported, change.ChecksReported{Head: fHead2}))
	in := inOf("pr-acme-pay-7", fold(t, pushed...), t0)
	require.NotNil(t, actionsOf(in)["review"][0].Task, "a new head is a new input")
}

// A pull request with no linked ticket is still reviewed: the review is worth having, and the gate stays red on the
// ticket clause (H11, D7). Any other failing slop rule still stops the review before it costs tokens.
func TestAPullRequestWhoseOnlySlopFailureIsTheTicketIsStillReviewed(t *testing.T) {
	ticketless := fold(t, ev(t, 1, change.EventPROpened, change.PROpened{Repo: "acme/pay", Number: 7, HeadSHA: fHead, Author: "ann",
		BaseSHA: fBase, SuggestedReviewers: []change.ActorID{"rev-1"}}),
		ev(t, 2, change.EventSlopEvaluated, change.SlopEvaluated{Pass: false, FailingRules: []string{"ticket_linked"}}))
	slopAt := time.Date(2026, 11, 4, 9, 2, 0, 0, time.UTC)
	acts := actionsOf(inOf("pr-acme-pay-7", ticketless, slopAt.Add(time.Hour)))
	require.NotEmpty(t, acts["pick"], "reviewers are picked although no ticket is linked")
	require.Empty(t, acts["slop"])

	oversized := fold(t, ev(t, 1, change.EventPROpened, change.PROpened{Repo: "acme/pay", Number: 7, HeadSHA: fHead, Author: "ann",
		BaseSHA: fBase, SuggestedReviewers: []change.ActorID{"rev-1"}}),
		ev(t, 2, change.EventSlopEvaluated, change.SlopEvaluated{Pass: false, FailingRules: []string{"ticket_linked", "size"}}))
	acts = actionsOf(inOf("pr-acme-pay-7", oversized, slopAt.Add(time.Hour)))
	require.Empty(t, acts["pick"], "another failing rule still stops the review")
	require.NotEmpty(t, acts["slop"])

	g := change.GateFacts(ticketless, change.Outside{})
	require.False(t, g.Slop.Pass, "the gate still sees the failed slop")
	require.False(t, g.TicketLinked)
}
