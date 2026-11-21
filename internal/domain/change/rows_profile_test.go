package change_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
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
