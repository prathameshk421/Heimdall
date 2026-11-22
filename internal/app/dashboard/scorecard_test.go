package dashboard

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/testkit/changelog"
)

func TestScorecardFromEventsCountsEveryRole(t *testing.T) {
	sha := id.SHA("0123456789abcdef0123456789abcdef01234567")
	f1 := id.FindingID("fnd_01HZX3K9QW7M2P5R8T0V4YB6C1")
	f2 := id.FindingID("fnd_01HZX3K9QW7M2P5R8T0V4YB6C2")
	sys := change.Actor{Kind: change.ActorSystem, ID: "heimdall"}
	evs := changelog.New(t, "KAN-10").
		Add(change.EventTestsSubmitted, changelog.Agent("test-author"), change.TestsSubmitted{Refs: []string{"a_test.go"}}).
		Add(change.EventTestsRejected, sys, change.TestsRejected{Reasons: []string{"passed on base"}, SHA: sha}).
		Add(change.EventTestsSubmitted, changelog.Agent("test-author"), change.TestsSubmitted{Refs: []string{"a_test.go"}}).
		Add(change.EventTestsVerified, sys, change.TestsVerified{RunIDs: []id.RunID{"run_01HZX3K9QW7M2P5R8T0V4YB6CD"}, SHA: sha, TreeHash: sha}).
		Add(change.EventTestsCommitted, sys, change.TestsCommitted{SHA: sha, TreeHash: sha}).
		Add(change.EventFindingFiled, changelog.Agent("reviewer-1"), change.FindingFiled{FindingID: f1, Ref: 1, Tier: finding.TierE1, Severity: finding.SeverityHigh, State: finding.StateOpen, By: "reviewer-1"}).
		Add(change.EventFindingVerified, sys, change.FindingVerified{FindingID: f1, Ref: 1, Tier: finding.TierE1, Severity: finding.SeverityHigh, State: finding.StateOpen, By: "heimdall"}).
		Add(change.EventFindingStateChanged, changelog.Human("bob"), change.FindingStateChanged{FindingID: f1, Ref: 1, Tier: finding.TierE1, Severity: finding.SeverityHigh, State: finding.StateUpheld, By: "bob"}).
		Add(change.EventFindingFiled, changelog.Agent("reviewer-2"), change.FindingFiled{FindingID: f2, Ref: 2, Tier: finding.TierE4, Severity: finding.SeverityLow, State: finding.StateUnverified, By: "reviewer-2"}).
		Add(change.EventFindingStateChanged, changelog.Human("bob"), change.FindingStateChanged{FindingID: f2, Ref: 2, Tier: finding.TierE4, Severity: finding.SeverityLow, State: finding.StateDismissed, By: "bob"}).
		Add(change.EventReviewSubmitted, changelog.Agent("reviewer-1"), change.ReviewSubmitted{Reviewer: "reviewer-1", FindingRefs: []id.FindingRef{1}, PackDate: "2026-11-22"}).
		Add(change.EventCaseOpened, sys, change.CaseOpened{CaseID: "case_01HZX3K9QW7M2P5R8T0V4YB6CD", Finding: 1}).
		Add(change.EventProfileRequested, changelog.Human("bob"), change.ProfileRequested{Repo: "acme/pay", By: "bob", Note: "add lint"}).
		Events()

	sc := ScorecardFromEvents(evs)
	require.Equal(t, 2, sc.TestAuthor.Submitted)
	require.Equal(t, 1, sc.TestAuthor.Verified)
	require.Equal(t, 1, sc.TestAuthor.Rejected)
	require.Equal(t, 1, sc.TestAuthor.Committed)
	require.InDelta(t, 0.5, sc.TestAuthor.RedFirstRate, 0.001)

	require.Equal(t, 1, sc.Reviewer.Reviews)
	require.Equal(t, 2, sc.Reviewer.Filed)
	require.Equal(t, 1, sc.Reviewer.Verified)
	require.Equal(t, 1, sc.Reviewer.Upheld)
	require.Equal(t, 1, sc.Reviewer.Dismissed)
	require.Equal(t, 1, sc.BySeat["reviewer-1"].Verified)
	require.Equal(t, 1, sc.BySeat["reviewer-1"].Upheld)
	require.Equal(t, 1, sc.BySeat["reviewer-2"].Dismissed)

	require.Equal(t, 1, sc.Resolver.CasesOpened)
	require.Equal(t, 1, sc.Profiler.Requests)
}
