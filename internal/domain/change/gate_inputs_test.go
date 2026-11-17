package change_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/slop"
)

const (
	changeKey = id.ChangeID("PAY-1")
	prRepo    = change.Repo("acme/pay")
	prNumber  = 7
	head      = id.SHA("1111111111111111111111111111111111111111")
	oldHead   = id.SHA("2222222222222222222222222222222222222222")
	nextHead  = id.SHA("3333333333333333333333333333333333333333")
	testTree1 = id.SHA("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	testTree2 = id.SHA("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	author    = change.ActorID("aarush323")
	approver  = change.ActorID("rekdfr")
	findingID = id.FindingID("fnd_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	caseID    = id.CaseID("case_01ARZ3NDEKTSV4RRFFQ69G5FAX")
	runID     = id.RunID("run_01ARZ3NDEKTSV4RRFFQ69G5FAW")
)

var (
	systemActor = change.Actor{Kind: change.ActorSystem, ID: "engine"}
	githubActor = change.Actor{Kind: change.ActorGitHub, ID: "github"}
)

// stream collects events with their payloads, then numbers them 1..n so Fold sees a real stream.
type stream struct {
	t   *testing.T
	evs []change.Event
}

func newStream(t *testing.T) *stream {
	t.Helper()
	return &stream{t: t}
}

func (s *stream) add(typ change.EventType, actor change.Actor, p change.Payload) *stream {
	s.t.Helper()
	require.NoError(s.t, p.Validate(), "payload of %s", typ)
	raw, err := json.Marshal(p)
	require.NoError(s.t, err)
	s.evs = append(s.evs, change.Event{Type: typ, Actor: actor, Payload: raw})
	return s
}

func (s *stream) drop(typ change.EventType) *stream {
	out := s.evs[:0]
	for _, e := range s.evs {
		if e.Type != typ {
			out = append(out, e)
		}
	}
	s.evs = out
	return s
}

func (s *stream) replace(typ change.EventType, p change.Payload) *stream {
	s.t.Helper()
	require.NoError(s.t, p.Validate(), "payload of %s", typ)
	raw, err := json.Marshal(p)
	require.NoError(s.t, err)
	for i := range s.evs {
		if s.evs[i].Type == typ {
			s.evs[i].Payload = raw
			return s
		}
	}
	s.t.Fatalf("no event of type %s to replace", typ)
	return s
}

func (s *stream) events() []change.Event {
	out := make([]change.Event, len(s.evs))
	for i, e := range s.evs {
		e.ID = change.EventID(fmt.Sprintf("10000000-0000-4000-8000-%012d", i+1))
		e.ChangeID = changeKey
		e.Version = int64(i + 1)
		out[i] = e
	}
	return out
}

// greenStream is a change that satisfies every clause of TECH_SPEC 12.4. Each test flips one thing from it.
func greenStream(t *testing.T) *stream {
	t.Helper()
	return newStream(t).
		add(change.EventPROpened, githubActor, change.PROpened{
			Repo: prRepo, Number: prNumber, HeadSHA: head, Author: author, Ticket: changeKey,
		}).
		add(change.EventSlopEvaluated, systemActor, change.SlopEvaluated{Pass: true}).
		add(change.EventTestsSubmitted, systemActor, change.TestsSubmitted{
			Refs: []string{"tests/heimdall/PAY-1/pay_test.go"}, ACMap: map[string][]string{"AC1": {"TestPay"}},
			SHA: head, TreeHash: testTree1,
		}).
		add(change.EventTestsVerified, systemActor, change.TestsVerified{
			RunIDs: []id.RunID{runID}, SHA: head, TreeHash: testTree1,
		}).
		add(change.EventTestsCommitted, systemActor, change.TestsCommitted{SHA: head, TreeHash: testTree1}).
		add(change.EventCIReported, githubActor, change.CIReported{
			Check: gate.CheckAcceptance, SHA: head, Conclusion: change.CISuccess,
		}).
		add(change.EventCIReported, githubActor, change.CIReported{
			Check: "build", SHA: head, Conclusion: change.CISuccess,
		}).
		add(change.EventReviewersPicked, systemActor, change.ReviewersPicked{
			People: []change.ActorID{"ana"}, By: "lead", Method: change.PickDefault,
		}).
		add(change.EventReviewWaived, systemActor, change.ReviewWaived{
			Reviewer: "ana", Reason: "the change is small", Approver: "lead",
		}).
		add(change.EventApprovalRecorded, change.Actor{Kind: change.ActorHuman, ID: approver},
			change.ApprovalRecorded{Login: approver, CommitSHA: head})
}

func ref(n int) id.FindingRef {
	r, err := id.NewFindingRef(n)
	if err != nil {
		panic(err)
	}
	return r
}

func greenOutside() change.Outside {
	pol := policy.Policy{Version: 1, Workflow: "default"}
	return change.Outside{
		HeadTestTreeHash:           string(testTree1),
		TestDiffKnown:              true,
		CIRequired:                 []string{"build"},
		ApproversForChangedDomains: []string{string(approver)},
		PolicyKnown:                true,
		PolicyAtBase:               pol,
		PolicyAtHead:               pol,
		PolicyVersion:              3,
	}
}

// foldFacts reads the gate facts of a stream from its view, with no outside facts.
func foldFacts(t *testing.T, s *stream) gate.Facts {
	t.Helper()
	v, err := change.Fold(s.events())
	require.NoError(t, err)
	return change.GateFacts(v, change.Outside{})
}

// TestFoldReadsEveryEventFact is the green case: every fact comes from the stream and none is assumed.
func TestFoldReadsEveryEventFact(t *testing.T) {
	f := foldFacts(t, greenStream(t))

	require.Equal(t, string(author), f.AuthorLogin)
	require.Equal(t, head, f.HeadSHA)
	require.True(t, f.TicketLinked)
	require.True(t, f.Slop.Pass)
	require.True(t, f.Testset.Verified)
	require.True(t, f.Testset.ACCovered)
	require.False(t, f.Testset.Sensitive)
	require.Equal(t, string(testTree1), f.Testset.TreeHash)
	require.Empty(t, f.HeadTestTreeHash, "the head tree is read from the repository, never folded from events")
	require.Equal(t, map[string]string{
		gate.CheckAcceptance: "success",
		"build":              "success",
	}, f.CI)
	require.True(t, f.ReviewsKnown)
	require.Equal(t, []gate.ReviewRequest{{Reviewer: "ana", Covered: false, Waived: true}}, f.ReviewRequests)
	require.True(t, f.BlockingKnown)
	require.Empty(t, f.Blocking)
	require.Empty(t, f.HumanPending)
	require.Equal(t, []gate.Approval{{Login: string(approver), Commit: head, Approved: true}}, f.Approvals)
}

// TestFoldTakesCIOnlyFromTheCurrentHead proves an old check cannot make the new head green.
func TestFoldTakesCIOnlyFromTheCurrentHead(t *testing.T) {
	s := greenStream(t).
		drop(change.EventCIReported).
		add(change.EventCIReported, githubActor, change.CIReported{
			Check: gate.CheckAcceptance, SHA: oldHead, Conclusion: change.CISuccess,
		}).
		add(change.EventCIReported, githubActor, change.CIReported{
			Check: "build", SHA: oldHead, Conclusion: change.CISuccess,
		})
	f := foldFacts(t, s)
	require.NotContains(t, f.CI, gate.CheckAcceptance)
	require.NotContains(t, f.CI, "build")

	s.add(change.EventCIReported, githubActor, change.CIReported{
		Check: gate.CheckAcceptance, SHA: head, Conclusion: change.CISuccess,
	})
	f = foldFacts(t, s)
	require.Equal(t, "success", f.CI[gate.CheckAcceptance])
	require.NotContains(t, f.CI, "build")
}

func TestFoldCountsAnApprovalOnlyOnTheHeadFromSomeoneElse(t *testing.T) {
	s := greenStream(t).add(change.EventPRPushed, githubActor, change.PRPushed{
		Repo: prRepo, Number: prNumber, HeadSHA: nextHead,
	})
	f := foldFacts(t, s)
	require.Empty(t, f.Approvals, "a push voids the approval")

	s.add(change.EventApprovalRecorded, change.Actor{Kind: change.ActorHuman, ID: author},
		change.ApprovalRecorded{Login: author, CommitSHA: nextHead})
	f = foldFacts(t, s)
	require.Empty(t, f.Approvals, "the author's approval does not count")

	s.add(change.EventApprovalRecorded, change.Actor{Kind: change.ActorHuman, ID: approver},
		change.ApprovalRecorded{Login: approver, CommitSHA: nextHead})
	f = foldFacts(t, s)
	require.Equal(t, []gate.Approval{{Login: string(approver), Commit: nextHead, Approved: true}}, f.Approvals)

	sOld := greenStream(t).drop(change.EventApprovalRecorded).
		add(change.EventApprovalRecorded, change.Actor{Kind: change.ActorHuman, ID: approver},
			change.ApprovalRecorded{Login: approver, CommitSHA: oldHead})
	fOld := foldFacts(t, sOld)
	require.Empty(t, fOld.Approvals, "approval on old sha must not be recorded")
}

func TestFoldVoidsAnApprovalThatWasInvalidated(t *testing.T) {
	s := greenStream(t).add(change.EventApprovalInvalidated,
		change.Actor{Kind: change.ActorHuman, ID: approver},
		change.ApprovalInvalidated{Login: approver, CommitSHA: head})
	f := foldFacts(t, s)
	require.Empty(t, f.Approvals)
}

func TestFoldTracksThePinAndTheSignoff(t *testing.T) {
	s := greenStream(t).
		replace(change.EventTestsVerified, change.TestsVerified{
			RunIDs: []id.RunID{runID}, SHA: head, TreeHash: testTree1, NeedsSignoff: true,
		})
	f := foldFacts(t, s)
	require.True(t, f.Testset.Sensitive)
	require.False(t, f.Testset.Approved)
	require.Empty(t, f.HeadTestTreeHash, "the head tree is read from the repository, never folded from events")

	s.add(change.EventTestsApproved, systemActor, change.TestsApproved{
		Approver: approver, TreeHash: testTree1,
	})
	f = foldFacts(t, s)
	require.True(t, f.Testset.Approved)

	s.replace(change.EventTestsCommitted, change.TestsCommitted{SHA: head, TreeHash: testTree2})
	f = foldFacts(t, s)
	require.Equal(t, string(testTree2), f.Testset.TreeHash)
	require.Empty(t, f.HeadTestTreeHash, "the head tree is read from the repository, never folded from events")
}

func TestFoldDoesNotCallUncommittedTestsVerified(t *testing.T) {
	s := greenStream(t).drop(change.EventTestsCommitted)
	f := foldFacts(t, s)
	require.False(t, f.Testset.Verified)
	require.Empty(t, f.Testset.TreeHash)
	require.Empty(t, f.HeadTestTreeHash, "the head tree is read from the repository, never folded from events")
}

func TestFoldTracksSlopFailuresAndAcceptanceCoverage(t *testing.T) {
	s := greenStream(t).
		replace(change.EventSlopEvaluated, change.SlopEvaluated{
			Pass: false, FailingRules: []string{string(slop.RuleSize)},
		}).
		replace(change.EventTestsSubmitted, change.TestsSubmitted{
			Refs: []string{"test"}, ACMap: nil, SHA: head, TreeHash: testTree1,
		})
	f := foldFacts(t, s)
	require.False(t, f.Slop.Pass)
	require.Equal(t, []slop.Failure{{
		Rule: slop.RuleSize, Message: "the slop rule failed on this head",
	}}, f.Slop.Failures)
	require.False(t, f.Testset.ACCovered)
}

func TestFoldLeavesReviewsUnknownUntilTheyArePicked(t *testing.T) {
	s := greenStream(t).drop(change.EventReviewersPicked)
	f := foldFacts(t, s)
	require.False(t, f.ReviewsKnown)

	s.add(change.EventReviewersPicked, systemActor, change.ReviewersPicked{
		People: []change.ActorID{"ana", "bo"}, By: "lead", Method: change.PickDefault,
	})
	f = foldFacts(t, s)
	require.True(t, f.ReviewsKnown)
	require.Equal(t, []gate.ReviewRequest{
		{Reviewer: "ana", Waived: true},
		{Reviewer: "bo"},
	}, f.ReviewRequests)
}

func TestFoldBuildsTheBlockingSetAndHumanCases(t *testing.T) {
	s := greenStream(t).add(change.EventFindingFiled, systemActor, change.FindingFiled{
		FindingID: findingID, Ref: ref(1), Tier: finding.TierE2, Severity: "high", State: finding.StateOpen, By: "reviewer",
	})
	f := foldFacts(t, s)
	require.Equal(t, []id.FindingID{findingID}, f.Blocking)

	s.add(change.EventFindingStateChanged, systemActor, change.FindingStateChanged{
		FindingID: findingID, Ref: ref(1), Tier: finding.TierE2, Severity: "high", State: finding.StateResolved, By: "author",
	})
	f = foldFacts(t, s)
	require.Empty(t, f.Blocking)

	s.add(change.EventCaseOpened, systemActor, change.CaseOpened{CaseID: caseID, Finding: ref(1)})
	f = foldFacts(t, s)
	require.Equal(t, []id.CaseID{caseID}, f.HumanPending)

	s.add(change.EventVerdictApplied, systemActor, change.VerdictApplied{
		CaseID: caseID, Verdict: change.VerdictDismiss, Citations: []string{"c"}, Checks: []string{"k"},
	})
	f = foldFacts(t, s)
	require.Empty(t, f.HumanPending)
}

func TestFoldNeverBlocksAnE4Finding(t *testing.T) {
	s := greenStream(t).add(change.EventFindingFiled, systemActor, change.FindingFiled{
		FindingID: findingID, Ref: ref(1), Tier: finding.TierE4, Severity: "high", State: finding.StateOpen, By: "reviewer",
	})
	f := foldFacts(t, s)
	require.Empty(t, f.Blocking)
}

func TestFoldMarksAChangedTicketAndAClosedPR(t *testing.T) {
	s := greenStream(t).
		replace(change.EventPROpened, change.PROpened{
			Repo: prRepo, Number: prNumber, HeadSHA: head, Author: author,
		}).
		add(change.EventPRLinked, githubActor, change.PRLinked{
			Repo: prRepo, Number: prNumber, Ticket: changeKey,
		})
	f := foldFacts(t, s)
	require.True(t, f.TicketLinked)

	s.add(change.EventPRClosed, githubActor, change.PRClosed{
		Repo: prRepo, Number: prNumber, HeadSHA: head,
	})
	v, err := change.Fold(s.events())
	require.NoError(t, err)
	require.True(t, v.Over(), "a closed pull request ends the change: no row is open and the gate is not evaluated")
	require.Empty(t, change.Next(change.In{Change: changeKey, View: v}))
}

func TestFoldRefusesCaseInsensitiveAuthorApproval(t *testing.T) {
	s := greenStream(t).drop(change.EventApprovalRecorded).
		add(change.EventApprovalRecorded, change.Actor{Kind: change.ActorHuman, ID: "AARUSH323"},
			change.ApprovalRecorded{Login: "AARUSH323", CommitSHA: head})
	f := foldFacts(t, s)
	require.Empty(t, f.Approvals, "author approval with different case must still be refused")
}

func TestFoldApprovalDoesNotSurvivePushEvenWithSameHead(t *testing.T) {
	s := greenStream(t).add(change.EventPRPushed, githubActor, change.PRPushed{
		Repo: prRepo, Number: prNumber, HeadSHA: head,
	})
	f := foldFacts(t, s)
	require.Empty(t, f.Approvals, "a push with the same head SHA must still void earlier approvals")
}

// TestFoldIsDeterministic guards the reason text and the id order: two folds of the same stream agree.
func TestFoldIsDeterministic(t *testing.T) {
	s := greenStream(t).add(change.EventFindingFiled, systemActor, change.FindingFiled{
		FindingID: id.FindingID("fnd_01ARZ3NDEKTSV4RRFFQ69G5FAW"),
		Ref:       ref(2), Tier: finding.TierE2, Severity: "high", State: finding.StateOpen, By: "reviewer",
	})
	a := foldFacts(t, s)
	b := foldFacts(t, s)
	require.Equal(t, a.Blocking, b.Blocking)
	require.Equal(t, a.HumanPending, b.HumanPending)
}

// gateResult is what the merge rule says about a stream: the outcome the gate row would publish.
type gateResult = change.GateOutcome

// evaluate runs the merge rule on a stream and the outside facts.
func evaluate(t *testing.T, s *stream, out change.Outside) gateResult {
	t.Helper()
	v, err := change.Fold(s.events())
	require.NoError(t, err)
	return change.EvaluateGate(v, out)
}

func hasClause(reasons []string, want gate.ClauseID) bool {
	prefix := string(want) + ":"
	for _, r := range reasons {
		if strings.HasPrefix(r, prefix) {
			return true
		}
	}
	return false
}
