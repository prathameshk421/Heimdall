package change_test

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// profileIn is a ticketless pull request whose slop passed, on a repository whose profile state is p.
func profileIn(t *testing.T, p change.Profile, profiler string) change.In {
	v := fold(t, opened(t), ev(t, 2, change.EventSlopEvaluated, change.SlopEvaluated{Pass: true}))
	in := inOf("pr-acme-pay-7", v, t0)
	in.Profile = p
	in.Settings.Profiler = profiler
	return in
}

// The profile row walks a repository from no profile to a promoted one (D175): propose, approve, trial, promote.
func TestTheProfileRowProposesThenAsksThenTrialsThenAsksToPromote(t *testing.T) {
	t.Parallel()
	id1 := strings.Repeat("c", 64)

	first := actionsOf(profileIn(t, change.Profile{}, "profiler"))["profile"][0]
	require.Equal(t, &change.Task{Kind: "profile", Subject: "acme/pay", Input: "first"}, first.Task)

	proposed := actionsOf(profileIn(t, change.Profile{Latest: &change.ProfileProposal{ID: id1, State: "proposed"}}, "profiler"))["profile"][0]
	require.Nil(t, proposed.Task)
	require.NotNil(t, proposed.Ask)
	require.Equal(t, []change.ActorID{"bob"}, proposed.Ask.Who)
	require.Contains(t, proposed.Text, "`heimdall approve-profile "+id1[:7]+"`")

	approved := actionsOf(profileIn(t, change.Profile{Latest: &change.ProfileProposal{ID: id1, State: "approved"}}, "profiler"))["profile"][0]
	require.NotNil(t, approved.Job)
	require.Equal(t, change.EffectProfileTrial, approved.Job.Kind)
	require.Equal(t, change.ProfileTrialPayload{Repo: "acme/pay", ProposalID: id1, SHA: fHead}, approved.Job.Payload)
	require.Equal(t, change.EffectKey("pr-acme-pay-7/profile/"+id1+"/r0"), approved.Job.Key)

	passed := actionsOf(profileIn(t, change.Profile{Latest: &change.ProfileProposal{ID: id1, State: "approved", Trial: "passed"}}, "profiler"))["profile"][0]
	require.NotNil(t, passed.Ask)
	require.Contains(t, passed.Text, "`heimdall promote-profile "+id1[:7]+"`")

	failed := actionsOf(profileIn(t, change.Profile{Latest: &change.ProfileProposal{ID: id1, State: "approved", Trial: "failed"}}, "profiler"))["profile"][0]
	require.Equal(t, &change.Task{Kind: "profile", Subject: "acme/pay", Input: "after-" + id1}, failed.Task, "a failed trial asks for a new proposal")

	require.NotContains(t, actionsOf(profileIn(t, change.Profile{Promoted: true, TestsDir: "tests/heimdall"}, "profiler")), "profile",
		"a promoted profile closes the row")
}

func TestTheProfileRowSaysSoWhenNoProfilerIsConfigured(t *testing.T) {
	t.Parallel()
	got := actionsOf(profileIn(t, change.Profile{}, ""))["profile"][0]
	require.Nil(t, got.Task)
	require.Equal(t, "acme/pay has no sandbox profile, and no profiler is configured in [agents] to propose one", got.Text)
}

// A promoted profile can gain a check: an owner's reprofile request is pending until a proposal created at or after it
// arrives, then that newer proposal drives approve, trial and promote, and the row is done again (D184).
func TestAPromotedProfileTakesARevisionRequestAndWalksTheNewProposal(t *testing.T) {
	t.Parallel()
	promoted := strings.Repeat("a", 64)
	newer := strings.Repeat("b", 64)
	asked := t0.Add(time.Hour)

	done := change.Profile{Promoted: true, PromotedID: promoted, TestsDir: "tests/heimdall",
		Latest: &change.ProfileProposal{ID: promoted, State: "promoted", CreatedAt: t0}}
	require.NotContains(t, actionsOf(profileIn(t, done, "profiler")), "profile", "a promoted profile with no request is done")

	request := &change.ProfileRequestInfo{Repo: "acme/pay", By: "ana", Note: "add a golangci-lint check", Asked: asked}
	reviseIn := profileIn(t, done, "profiler")
	reviseIn.View.ProfileRequest = request
	revise := actionsOf(reviseIn)["profile"][0]
	require.NotNil(t, revise.Task, "a pending request revises the promoted profile")
	require.Equal(t, "profile", revise.Task.Kind)
	require.Equal(t, "acme/pay", revise.Task.Subject)
	require.Equal(t, "revise-"+promoted[:12]+"-"+strconv.FormatInt(asked.Unix(), 10), revise.Task.Input)
	require.Equal(t, "the profiler revises the sandbox profile of acme/pay", revise.Text)

	// The profiler's proposal is newer than the request, so it asks for approval instead of revising again.
	proposedIn := profileIn(t, change.Profile{Promoted: true, PromotedID: promoted,
		Latest: &change.ProfileProposal{ID: newer, State: "proposed", CreatedAt: asked.Add(time.Minute)}}, "profiler")
	proposedIn.View.ProfileRequest = request
	proposed := actionsOf(proposedIn)["profile"][0]
	require.Nil(t, proposed.Task)
	require.NotNil(t, proposed.Ask)
	require.Contains(t, proposed.Text, "`heimdall approve-profile "+newer[:7]+"`")

	// Once the new proposal is promoted, the request is answered and the row is done again.
	again := profileIn(t, change.Profile{Promoted: true, PromotedID: newer, TestsDir: "tests/heimdall",
		Latest: &change.ProfileProposal{ID: newer, State: "promoted", CreatedAt: asked.Add(time.Minute)}}, "profiler")
	again.View.ProfileRequest = request
	require.NotContains(t, actionsOf(again), "profile")

	// A newer proposal that failed its trial is not hidden by the promoted one: its failure drives the next proposal.
	failedIn := profileIn(t, change.Profile{Promoted: true, PromotedID: promoted,
		Latest: &change.ProfileProposal{ID: newer, State: "approved", Trial: "failed", CreatedAt: asked.Add(time.Minute)}}, "profiler")
	failedIn.View.ProfileRequest = request
	failed := actionsOf(failedIn)["profile"][0]
	require.Equal(t, &change.Task{Kind: "profile", Subject: "acme/pay", Input: "after-" + newer}, failed.Task)
}

// A request with no profiler configured is words, not a task.
func TestARevisionRequestWithNoProfilerIsWords(t *testing.T) {
	t.Parallel()
	in := profileIn(t, change.Profile{Promoted: true, PromotedID: strings.Repeat("a", 64),
		Latest: &change.ProfileProposal{ID: strings.Repeat("a", 64), State: "promoted", CreatedAt: t0}}, "")
	in.View.ProfileRequest = &change.ProfileRequestInfo{Repo: "acme/pay", By: "ana", Note: "add lint", Asked: t0.Add(time.Hour)}
	got := actionsOf(in)["profile"][0]
	require.Nil(t, got.Task)
	require.Equal(t, "acme/pay was asked for a profile change, and no profiler is configured in [agents] to make it", got.Text)
}

// The D173 cap still applies to a revise task: two failures on the same input ask a person to resume.
func TestAReviseTaskThatFailedTwiceWaitsForAPerson(t *testing.T) {
	t.Parallel()
	promoted := strings.Repeat("a", 64)
	v := fold(t, opened(t), ev(t, 2, change.EventSlopEvaluated, change.SlopEvaluated{Pass: true}),
		ev(t, 3, change.EventProfileRequested, change.ProfileRequested{Repo: "acme/pay", By: "ana", Note: "add lint"}))
	asked := v.ProfileRequest.Asked
	require.False(t, asked.IsZero())
	input := "revise-" + promoted[:12] + "-" + strconv.FormatInt(asked.Unix(), 10)
	v = fold(t, opened(t), ev(t, 2, change.EventSlopEvaluated, change.SlopEvaluated{Pass: true}),
		ev(t, 3, change.EventProfileRequested, change.ProfileRequested{Repo: "acme/pay", By: "ana", Note: "add lint"}),
		ev(t, 4, change.EventTaskAssigned, change.TaskAssigned{TaskID: tk("tsk_01HZX3K9QW7M2P5R8T0V4YB6C1"), Agent: "profiler",
			Role: workflow.RoleProfiler, Deadline: asked.Add(time.Hour), Kind: "profile", Subject: "acme/pay", Input: input}),
		ev(t, 5, change.EventTaskFailed, change.TaskFailed{TaskID: tk("tsk_01HZX3K9QW7M2P5R8T0V4YB6C1"), Agent: "profiler",
			Role: workflow.RoleProfiler, Reason: "sandbox down"}),
		ev(t, 6, change.EventTaskAssigned, change.TaskAssigned{TaskID: tk("tsk_01HZX3K9QW7M2P5R8T0V4YB6C2"), Agent: "profiler",
			Role: workflow.RoleProfiler, Deadline: asked.Add(time.Hour), Kind: "profile", Subject: "acme/pay", Input: input}),
		ev(t, 7, change.EventTaskFailed, change.TaskFailed{TaskID: tk("tsk_01HZX3K9QW7M2P5R8T0V4YB6C2"), Agent: "profiler",
			Role: workflow.RoleProfiler, Reason: "sandbox down"}))
	in := inOf("pr-acme-pay-7", v, asked.Add(2*time.Hour))
	in.Settings.Profiler = "profiler"
	in.Profile = change.Profile{Promoted: true, PromotedID: promoted, Latest: &change.ProfileProposal{ID: promoted, State: "promoted", CreatedAt: t0}}
	got := actionsOf(in)["profile"][0]
	require.Nil(t, got.Task, "two failures on the same input stop the seat")
	require.NotNil(t, got.Ask)
	require.Contains(t, got.Text, "`heimdall resume pr-acme-pay-7 profile` tries again")
}

// Review never waits for a profile: the row has no dependents.
func TestReviewDoesNotWaitForTheProfile(t *testing.T) {
	t.Parallel()
	v := fold(t, opened(t), ev(t, 2, change.EventSlopEvaluated, change.SlopEvaluated{Pass: true}),
		ev(t, 3, change.EventReviewersPicked, change.ReviewersPicked{People: []change.ActorID{"rev-1"}, By: "heimdall", Method: change.PickDefault}))
	in := inOf("pr-acme-pay-7", v, t0.Add(time.Hour))
	in.Settings.Profiler = "profiler"
	acts := actionsOf(in)
	require.NotEmpty(t, acts["profile"])
	require.NotEmpty(t, acts["review"])
}

// profileTask is one profiler task and its failure: the profiler could not read the sandbox, so the task failed.
func profileTask(t *testing.T, n int, task string) []change.Event {
	t.Helper()
	return []change.Event{
		ev(t, n, change.EventTaskAssigned, change.TaskAssigned{TaskID: tk(task), Agent: "profiler", Role: workflow.RoleProfiler,
			Deadline: time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC), Kind: "profile", Subject: "acme/pay", Input: "first"}),
		ev(t, n+1, change.EventTaskFailed, change.TaskFailed{TaskID: tk(task), Agent: "profiler", Role: workflow.RoleProfiler, Reason: "sandbox down"}),
	}
}

// profileActions is what the profile row does on the given events.
func profileActions(t *testing.T, evs ...change.Event) []change.Action {
	t.Helper()
	in := inOf("pr-acme-pay-7", fold(t, evs...), t0)
	in.Settings.Profiler = "profiler"
	return actionsOf(in)["profile"]
}

// A profiler seat that failed twice on the same input is not assigned again until a person resumes the profile row,
// which starts a new round and allows two more failures (D173, D175).
func TestAProfileTaskThatFailedTwiceIsNotAssignedAgainUntilResumed(t *testing.T) {
	t.Parallel()
	base := []change.Event{opened(t), ev(t, 2, change.EventSlopEvaluated, change.SlopEvaluated{Pass: true})}
	once := append(append([]change.Event{}, base...), profileTask(t, 3, "tsk_01HZX3K9QW7M2P5R8T0V4YB6C1")...)
	got := profileActions(t, once...)[0]
	require.NotNil(t, got.Task, "one failure is retried")
	require.Equal(t, &change.Task{Kind: "profile", Subject: "acme/pay", Input: "first"}, got.Task)

	twice := append(append([]change.Event{}, once...), profileTask(t, 5, "tsk_01HZX3K9QW7M2P5R8T0V4YB6C2")...)
	stopped := profileActions(t, twice...)[0]
	require.Nil(t, stopped.Task, "two failures on the same input stop the profiler seat")
	require.NotNil(t, stopped.Ask)
	require.Equal(t, "acme/pay failed 2 profile tasks on first; `heimdall resume pr-acme-pay-7 profile` tries again", stopped.Text)

	resumed := append(append([]change.Event{}, twice...), ev(t, 7, change.EventStepResumed, change.StepResumed{Step: "profile", By: "ann"}))
	again := profileActions(t, resumed...)[0]
	require.NotNil(t, again.Task, "a resume starts a new round, so the profiler is assigned again")
	require.Equal(t, &change.Task{Kind: "profile", Subject: "acme/pay", Input: "first"}, again.Task)

	more := append(append([]change.Event{}, resumed...), profileTask(t, 8, "tsk_01HZX3K9QW7M2P5R8T0V4YB6C3")...)
	more = append(more, profileTask(t, 10, "tsk_01HZX3K9QW7M2P5R8T0V4YB6C4")...)
	capped := profileActions(t, more...)[0]
	require.Nil(t, capped.Task, "round 1 caps again after two failures")
	require.NotNil(t, capped.Ask)
}
