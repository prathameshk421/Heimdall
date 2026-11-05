package engine_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/engine"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

// reviewerActor is the reviewer agent that submits a review in these tests.
var reviewerActor = change.Actor{Kind: change.ActorAgent, ID: "bob-reviewer"}

// prOpened is the pull request the test-author's commit produces, as GitHub reports it.
func prOpened() change.PROpened {
	return change.PROpened{Repo: repo, Number: 42, HeadSHA: sha, Author: "aarush323", Ticket: ticket}
}

// TestDefaultWorkflowRunsTicketToRedTestsOnFakes drives the default workflow of TECH_SPEC 5.1 on the fakes, one
// incoming event at a time, the way the server feeds it. It ends when the gate has evaluated on the review.
func TestDefaultWorkflowRunsTicketToRedTestsOnFakes(t *testing.T) {
	h := newHarness(t)
	e := h.build(h.defaultConfig(h.defaultSteps()))
	ctx := context.Background()

	// The ticket moves to In Progress: the room opens, the branch exists and the test-author gets its task.
	out, err := e.Advance(ctx, ticket, h.triggered())
	require.NoError(t, err)
	require.Equal(t, []change.EventType{
		change.EventChangeTriggered, change.EventRoomOpened, change.EventBranchEnsured, change.EventTaskAssigned,
	}, h.types())
	require.Equal(t, []engine.Timer{{Step: "write_tests", Due: dayStart.Add(2 * time.Hour)}}, out.Timers,
		"the deadline timer is reported once, although the step is started again in the same run")
	require.False(t, out.Complete)

	// The test-author submits. The sandbox verifies the tests and the commit writes them to the branch.
	out, err = e.Advance(ctx, ticket, submission(h))
	require.NoError(t, err)
	require.Equal(t, []change.EventType{
		change.EventChangeTriggered, change.EventRoomOpened, change.EventBranchEnsured, change.EventTaskAssigned,
		change.EventTestsSubmitted, change.EventTestsVerified, change.EventTestsCommitted,
	}, h.types())
	require.Len(t, h.store.effects, 1, "one branch write, with its idempotency key")
	require.Equal(t, change.EffectGitHubBranch, h.store.effects[0].Kind)
	require.False(t, out.Complete)

	// The pull request opens. The slop rule passes it and the reviewer pick waits for a person.
	out, err = e.Advance(ctx, ticket, h.incoming(change.EventPROpened, ghActor, prOpened()))
	require.NoError(t, err)
	require.True(t, h.has(change.EventSlopEvaluated))
	require.Equal(t, []engine.Timer{{Step: "pick", Due: dayStart.Add(15 * time.Minute)}}, out.Timers)
	require.False(t, out.Complete)

	// A person picks the reviewer. The review step starts and sets its deadline.
	pick := change.ReviewersPicked{People: []change.ActorID{"ana"}, By: "ana", Method: change.PickCommand}
	out, err = e.Advance(ctx, ticket, h.incoming(change.EventReviewersPicked, humanActor, pick))
	require.NoError(t, err)
	require.Equal(t, []engine.Timer{{Step: "review", Due: dayStart.Add(30 * time.Minute)}}, out.Timers)
	require.False(t, out.Complete)

	// The reviewer submits. Verify, mutation and deliberation are finished with the review, and the gate goes green.
	review := change.ReviewSubmitted{Reviewer: "bob-reviewer", PackDate: "2026-11-04"}
	out, err = e.Advance(ctx, ticket, h.incoming(change.EventReviewSubmitted, reviewerActor, review))
	require.NoError(t, err)
	require.Equal(t, []change.EventType{
		change.EventChangeTriggered, change.EventRoomOpened, change.EventBranchEnsured, change.EventTaskAssigned,
		change.EventTestsSubmitted, change.EventTestsVerified, change.EventTestsCommitted,
		change.EventPROpened, change.EventSlopEvaluated, change.EventReviewersPicked,
		change.EventReviewSubmitted, change.EventGateEvaluated,
	}, h.types())
	require.True(t, out.Complete)

	// Nothing is left to do: a later run with no events writes nothing.
	appends := h.store.appends
	out, err = e.Advance(ctx, ticket, nil)
	require.NoError(t, err)
	require.Zero(t, out.Events)
	require.True(t, out.Complete)
	require.Equal(t, appends, h.store.appends)
}

// TestDefaultWorkflowWaitsForSignoffOnSensitiveDomains checks the signoff step (D33): with sensitive_domain true, the
// commit waits for a person to approve the tests, and the sandbox reports that the tests need signoff.
func TestDefaultWorkflowWaitsForSignoffOnSensitiveDomains(t *testing.T) {
	h := newHarness(t)
	h.sensitive = true
	e := h.build(h.defaultConfig(h.defaultSteps()))
	ctx := context.Background()

	_, err := e.Advance(ctx, ticket, h.triggered())
	require.NoError(t, err)
	out, err := e.Advance(ctx, ticket, submission(h))
	require.NoError(t, err)
	require.True(t, h.has(change.EventTestsVerified))
	require.False(t, h.has(change.EventTestsCommitted), "the commit waits for the approval")
	require.Empty(t, h.store.effects)
	require.False(t, out.Complete)

	approval := change.TestsApproved{Approver: "ana", TreeHash: sha}
	out, err = e.Advance(ctx, ticket, h.incoming(change.EventTestsApproved, humanActor, approval))
	require.NoError(t, err)
	require.True(t, h.has(change.EventTestsCommitted), "the approval lets the commit run")
	require.Len(t, h.store.effects, 1)
	require.False(t, out.Complete, "the change still waits for the pull request")
}
