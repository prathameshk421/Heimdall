package gateuse_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/gateuse"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// The pin clause compares the test tree pinned by tests.committed with the test tree AT THE PR HEAD. The head tree
// must come from the repository (Outside), never from the event log: otherwise a PR that edits or deletes the pinned
// tests would still pass (TECH_SPEC 3.4, D150).
func TestLeadTheTestTreeAtHeadComesFromTheRepositoryNotTheEvents(t *testing.T) {
	out := greenOutside()
	out.HeadTestTreeHash = string(testTree1)
	res, _ := evaluate(t, greenStream(t), out)
	require.True(t, res.OK, "the head tree equals the pinned tree")

	changed := greenOutside()
	changed.HeadTestTreeHash = string(testTree2)
	res, _ = evaluate(t, greenStream(t), changed)
	require.False(t, res.OK, "a PR that changed the pinned tests must not pass")
	require.Contains(t, fmt.Sprint(res.Reasons), "test tree")

	unknown := greenOutside()
	unknown.HeadTestTreeHash = ""
	res, _ = evaluate(t, greenStream(t), unknown)
	require.False(t, res.OK, "a head tree nobody read must fail closed")
}

// The fold must not invent the head tree from events.
func TestLeadFoldLeavesTheHeadTestTreeUnknown(t *testing.T) {
	require.Empty(t, fold(t, greenStream(t)).HeadTestTreeHash)
}

// The blocking set follows the thresholds of the policy at the merge base, not a constant in the fold.
func TestLeadBlockingFollowsThePolicyThresholds(t *testing.T) {
	filed := func(tier finding.Tier) *stream {
		return greenStream(t).add(change.EventFindingFiled, systemActor, change.FindingFiled{
			FindingID: findingID, Ref: ref(1), Tier: tier, State: finding.StateOpen, By: "reviewer",
		})
	}
	out := greenOutside()
	out.HeadTestTreeHash = string(testTree1)
	out.PolicyAtBase.Blocking = policy.Blocking{MinSeverity: finding.SeverityCritical, MinTier: finding.TierE2}
	out.PolicyAtHead = out.PolicyAtBase

	res, _ := evaluate(t, filed(finding.TierE3), out)
	require.True(t, res.OK, "an open E3 finding is below this policy's minimum tier E2")

	res, _ = evaluate(t, filed(finding.TierE2), out)
	require.False(t, res.OK, "an open E2 finding meets this policy's minimum tier")

	res, _ = evaluate(t, filed(finding.TierE4), out)
	require.True(t, res.OK, "E4 never blocks")
}

// The event the use case appends must be a valid event on its own: a real store validates the envelope, and a
// wrapper in the composition root must never have to repair it.
func TestLeadTheGateEventPassesEnvelopeValidation(t *testing.T) {
	out := greenOutside()
	_, store := evaluate(t, greenStream(t), out)
	ev := producedEvent(t, store, change.EventGateEvaluated)
	require.NoError(t, ev.Validate())
	require.NotEmpty(t, ev.CorrelationID)
}

// A late CI report for an OLD commit must neither count nor wipe out the good result already held for the head.
func TestLeadALateCIReportForAnOldCommitDoesNotReplaceTheHeadResult(t *testing.T) {
	s := greenStream(t).add(change.EventCIReported, githubActor, change.CIReported{
		Check: "build", SHA: oldHead, Conclusion: change.CIFailure,
	})
	facts := fold(t, s)
	require.Equal(t, "success", facts.CI["build"], "the result for the head commit must survive a late report for another commit")
}

// Without a known policy the gate is red anyway; the fold's own default stays strict.
func TestLeadFoldDefaultBlockingIsStrict(t *testing.T) {
	s := greenStream(t).add(change.EventFindingFiled, systemActor, change.FindingFiled{
		FindingID: findingID, Ref: ref(1), Tier: finding.TierE3, State: finding.StateOpen, By: "reviewer",
	})
	facts, err := gateuse.Fold(s.events())
	require.NoError(t, err)
	require.NotEmpty(t, facts.Blocking)
}

// A slop rejection is final for that head: the check must stay red (completed, failure), not fall back to pending
// because the tests have not been verified yet. Found on the live run: the gate overwrote intake's failure with
// in_progress.
func TestLeadASlopFailureStaysRedAndNeverBecomesPending(t *testing.T) {
	// No tests verified or committed yet, as on the live PR: that alone would make the check pending.
	s := greenStream(t).
		replace(change.EventSlopEvaluated, change.SlopEvaluated{Pass: false, FailingRules: []string{"size"}}).
		drop(change.EventTestsVerified).
		drop(change.EventTestsCommitted)
	out := greenOutside()
	res, store := evaluate(t, s, out)
	require.False(t, res.OK)
	require.Equal(t, ports.CheckCompleted, res.Status)
	c := checkPayloadOf(t, store)
	require.Equal(t, ports.CheckCompleted, c.Status)
	require.Equal(t, change.CIFailure, c.Conclusion)
}

// A submitted review covers its reviewer for the head it followed (the event carries no head sha, so the rule is
// order): a later push voids it and the reviewer must look again. Found on the live run: the fold never read
// review.submitted, so no review could ever satisfy the gate.
func TestLeadASubmittedReviewCoversTheReviewerUntilThePushThatFollowsIt(t *testing.T) {
	s := greenStream(t).
		drop(change.EventReviewWaived).
		replace(change.EventReviewersPicked, change.ReviewersPicked{
			People: []change.ActorID{"bob"}, By: "lead", Method: change.PickDefault,
		})
	f := fold(t, s)
	require.Len(t, f.ReviewRequests, 1)
	require.False(t, f.ReviewRequests[0].Covered, "nobody has reviewed yet")

	s.add(change.EventReviewSubmitted, systemActor, change.ReviewSubmitted{Reviewer: "bob", PackDate: "2026-11-05"})
	f = fold(t, s)
	require.True(t, f.ReviewRequests[0].Covered, "a review after the head covers the reviewer")

	s.add(change.EventPRPushed, githubActor, change.PRPushed{Repo: prRepo, Number: prNumber, HeadSHA: oldHead})
	f = fold(t, s)
	require.False(t, f.ReviewRequests[0].Covered, "a new head voids the review")
}

// A reviewer who was never asked cannot cover anything, and a review of an unavailable reviewer is not a review.
func TestLeadAReviewFromSomeoneNotPickedCoversNobody(t *testing.T) {
	s := greenStream(t).
		drop(change.EventReviewWaived).
		replace(change.EventReviewersPicked, change.ReviewersPicked{
			People: []change.ActorID{"bob"}, By: "lead", Method: change.PickDefault,
		}).
		add(change.EventReviewSubmitted, systemActor, change.ReviewSubmitted{Reviewer: "carol", PackDate: "2026-11-05"})
	f := fold(t, s)
	require.False(t, f.ReviewRequests[0].Covered)
}
