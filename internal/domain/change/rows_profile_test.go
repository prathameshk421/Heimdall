package change_test

import (
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
	id1 := strings.Repeat("c", 64)

	first := actionsOf(profileIn(t, change.Profile{}, "profiler"))["profile"][0]
	require.Equal(t, &change.Task{Kind: "profile", Subject: "acme/pay", Input: "first"}, first.Task)

	proposed := actionsOf(profileIn(t, change.Profile{Latest: &change.ProfileProposal{ID: id1, State: "proposed"}}, "profiler"))["profile"][0]
	require.Nil(t, proposed.Task)
	require.NotNil(t, proposed.Ask)
	require.Equal(t, []change.ActorID{"bob"}, proposed.Ask.Who)
	require.Contains(t, proposed.Text, "`heimdall approve-profile "+id1+"`")

	approved := actionsOf(profileIn(t, change.Profile{Latest: &change.ProfileProposal{ID: id1, State: "approved"}}, "profiler"))["profile"][0]
	require.NotNil(t, approved.Job)
	require.Equal(t, change.EffectProfileTrial, approved.Job.Kind)
	require.Equal(t, change.ProfileTrialPayload{Repo: "acme/pay", ProposalID: id1, SHA: fHead}, approved.Job.Payload)
	require.Equal(t, change.EffectKey("pr-acme-pay-7/profile/"+id1+"/r0"), approved.Job.Key)

	passed := actionsOf(profileIn(t, change.Profile{Latest: &change.ProfileProposal{ID: id1, State: "approved", Trial: "passed"}}, "profiler"))["profile"][0]
	require.NotNil(t, passed.Ask)
	require.Contains(t, passed.Text, "`heimdall promote-profile "+id1+"`")

	failed := actionsOf(profileIn(t, change.Profile{Latest: &change.ProfileProposal{ID: id1, State: "approved", Trial: "failed"}}, "profiler"))["profile"][0]
	require.Equal(t, &change.Task{Kind: "profile", Subject: "acme/pay", Input: "after-" + id1}, failed.Task, "a failed trial asks for a new proposal")

	require.NotContains(t, actionsOf(profileIn(t, change.Profile{Promoted: true, TestsDir: "tests/heimdall"}, "profiler")), "profile",
		"a promoted profile closes the row")
}

func TestTheProfileRowSaysSoWhenNoProfilerIsConfigured(t *testing.T) {
	got := actionsOf(profileIn(t, change.Profile{}, ""))["profile"][0]
	require.Nil(t, got.Task)
	require.Equal(t, "acme/pay has no sandbox profile, and no profiler is configured in [agents] to propose one", got.Text)
}

// Review never waits for a profile: the row has no dependents.
func TestReviewDoesNotWaitForTheProfile(t *testing.T) {
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
