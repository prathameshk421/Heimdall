package gateuse_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/gateuse"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/slop"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
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

func greenOutside() gateuse.Outside {
	pol := policy.Policy{Version: 1, Workflow: "default"}
	return gateuse.Outside{
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

func fold(t *testing.T, s *stream) gate.Facts {
	t.Helper()
	facts, err := gateuse.Fold(s.events())
	require.NoError(t, err)
	return facts
}

// TestFoldReadsEveryEventFact is the green case: every fact comes from the stream and none is assumed.
func TestFoldReadsEveryEventFact(t *testing.T) {
	f := fold(t, greenStream(t))

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

func TestFoldRefusesAChangeWithoutAPullRequest(t *testing.T) {
	s := newStream(t).add(change.EventChangeTriggered, systemActor, change.Triggered{
		Ticket: changeKey, Repo: prRepo, Assignee: "ana", Source: change.TriggerManual,
	})

	_, err := gateuse.Fold(s.events())
	require.ErrorIs(t, err, gateuse.ErrNoPullRequest)
}

func TestFoldRefusesAMalformedPayload(t *testing.T) {
	s := newStream(t).
		add(change.EventPROpened, githubActor, change.PROpened{
			Repo: prRepo, Number: prNumber, HeadSHA: head, Author: author, Ticket: changeKey,
		})
	s.evs[0].Payload = json.RawMessage(`{"repo": "acme/pay"}`)

	_, err := gateuse.Fold(s.events())
	require.Error(t, err)
	require.Contains(t, err.Error(), "pr.opened")
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
	f := fold(t, s)
	require.NotContains(t, f.CI, gate.CheckAcceptance)
	require.NotContains(t, f.CI, "build")

	s.add(change.EventCIReported, githubActor, change.CIReported{
		Check: gate.CheckAcceptance, SHA: head, Conclusion: change.CISuccess,
	})
	f = fold(t, s)
	require.Equal(t, "success", f.CI[gate.CheckAcceptance])
	require.NotContains(t, f.CI, "build")
}

func TestFoldCountsAnApprovalOnlyOnTheHeadFromSomeoneElse(t *testing.T) {
	s := greenStream(t).add(change.EventPRPushed, githubActor, change.PRPushed{
		Repo: prRepo, Number: prNumber, HeadSHA: nextHead,
	})
	f := fold(t, s)
	require.Empty(t, f.Approvals, "a push voids the approval")

	s.add(change.EventApprovalRecorded, change.Actor{Kind: change.ActorHuman, ID: author},
		change.ApprovalRecorded{Login: author, CommitSHA: nextHead})
	f = fold(t, s)
	require.Empty(t, f.Approvals, "the author's approval does not count")

	s.add(change.EventApprovalRecorded, change.Actor{Kind: change.ActorHuman, ID: approver},
		change.ApprovalRecorded{Login: approver, CommitSHA: nextHead})
	f = fold(t, s)
	require.Equal(t, []gate.Approval{{Login: string(approver), Commit: nextHead, Approved: true}}, f.Approvals)

	sOld := greenStream(t).drop(change.EventApprovalRecorded).
		add(change.EventApprovalRecorded, change.Actor{Kind: change.ActorHuman, ID: approver},
			change.ApprovalRecorded{Login: approver, CommitSHA: oldHead})
	fOld := fold(t, sOld)
	require.Empty(t, fOld.Approvals, "approval on old sha must not be recorded")
}

func TestFoldVoidsAnApprovalThatWasInvalidated(t *testing.T) {
	s := greenStream(t).add(change.EventApprovalInvalidated,
		change.Actor{Kind: change.ActorHuman, ID: approver},
		change.ApprovalInvalidated{Login: approver, CommitSHA: head})
	f := fold(t, s)
	require.Empty(t, f.Approvals)
}

func TestFoldTracksThePinAndTheSignoff(t *testing.T) {
	s := greenStream(t).
		replace(change.EventTestsVerified, change.TestsVerified{
			RunIDs: []id.RunID{runID}, SHA: head, TreeHash: testTree1, NeedsSignoff: true,
		})
	f := fold(t, s)
	require.True(t, f.Testset.Sensitive)
	require.False(t, f.Testset.Approved)
	require.Empty(t, f.HeadTestTreeHash, "the head tree is read from the repository, never folded from events")

	s.add(change.EventTestsApproved, systemActor, change.TestsApproved{
		Approver: approver, TreeHash: testTree1,
	})
	f = fold(t, s)
	require.True(t, f.Testset.Approved)

	s.replace(change.EventTestsCommitted, change.TestsCommitted{SHA: head, TreeHash: testTree2})
	f = fold(t, s)
	require.Equal(t, string(testTree2), f.Testset.TreeHash)
	require.Empty(t, f.HeadTestTreeHash, "the head tree is read from the repository, never folded from events")
}

func TestFoldDoesNotCallUncommittedTestsVerified(t *testing.T) {
	s := greenStream(t).drop(change.EventTestsCommitted)
	f := fold(t, s)
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
	f := fold(t, s)
	require.False(t, f.Slop.Pass)
	require.Equal(t, []slop.Failure{{
		Rule: slop.RuleSize, Message: "the slop rule failed on this head",
	}}, f.Slop.Failures)
	require.False(t, f.Testset.ACCovered)
}

func TestFoldLeavesReviewsUnknownUntilTheyArePicked(t *testing.T) {
	s := greenStream(t).drop(change.EventReviewersPicked)
	f := fold(t, s)
	require.False(t, f.ReviewsKnown)

	s.add(change.EventReviewersPicked, systemActor, change.ReviewersPicked{
		People: []change.ActorID{"ana", "bo"}, By: "lead", Method: change.PickDefault,
	})
	f = fold(t, s)
	require.True(t, f.ReviewsKnown)
	require.Equal(t, []gate.ReviewRequest{
		{Reviewer: "ana", Waived: true},
		{Reviewer: "bo"},
	}, f.ReviewRequests)
}

func TestFoldBuildsTheBlockingSetAndHumanCases(t *testing.T) {
	s := greenStream(t).add(change.EventFindingFiled, systemActor, change.FindingFiled{
		FindingID: findingID, Ref: ref(1), Tier: finding.TierE2, State: finding.StateOpen, By: "reviewer",
	})
	f := fold(t, s)
	require.Equal(t, []id.FindingID{findingID}, f.Blocking)

	s.add(change.EventFindingStateChanged, systemActor, change.FindingStateChanged{
		FindingID: findingID, Ref: ref(1), Tier: finding.TierE2, State: finding.StateResolved, By: "author",
	})
	f = fold(t, s)
	require.Empty(t, f.Blocking)

	s.add(change.EventCaseOpened, systemActor, change.CaseOpened{CaseID: caseID, Finding: ref(1)})
	f = fold(t, s)
	require.Equal(t, []id.CaseID{caseID}, f.HumanPending)

	s.add(change.EventVerdictApplied, systemActor, change.VerdictApplied{
		CaseID: caseID, Verdict: change.VerdictDismiss, Citations: []string{"c"}, Checks: []string{"k"},
	})
	f = fold(t, s)
	require.Empty(t, f.HumanPending)
}

func TestFoldNeverBlocksAnE4Finding(t *testing.T) {
	s := greenStream(t).add(change.EventFindingFiled, systemActor, change.FindingFiled{
		FindingID: findingID, Ref: ref(1), Tier: finding.TierE4, State: finding.StateOpen, By: "reviewer",
	})
	f := fold(t, s)
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
	f := fold(t, s)
	require.True(t, f.TicketLinked)

	s.add(change.EventPRClosed, githubActor, change.PRClosed{
		Repo: prRepo, Number: prNumber, HeadSHA: head,
	})
	eval, err := gateuse.New(gateuse.Config{
		Events: &fakeStore{events: s.events()},
		Source: &fakeSource{outside: greenOutside()},
		Clock:  fixedClock{},
		IDs:    &fakeIDs{},
	})
	require.NoError(t, err)
	_, err = eval.Evaluate(context.Background(), changeKey)
	require.ErrorIs(t, err, gateuse.ErrPullRequestClosed)
}

func TestFoldRefusesCaseInsensitiveAuthorApproval(t *testing.T) {
	s := greenStream(t).drop(change.EventApprovalRecorded).
		add(change.EventApprovalRecorded, change.Actor{Kind: change.ActorHuman, ID: "AARUSH323"},
			change.ApprovalRecorded{Login: "AARUSH323", CommitSHA: head})
	f := fold(t, s)
	require.Empty(t, f.Approvals, "author approval with different case must still be refused")
}

func TestFoldApprovalDoesNotSurvivePushEvenWithSameHead(t *testing.T) {
	s := greenStream(t).add(change.EventPRPushed, githubActor, change.PRPushed{
		Repo: prRepo, Number: prNumber, HeadSHA: head,
	})
	f := fold(t, s)
	require.Empty(t, f.Approvals, "a push with the same head SHA must still void earlier approvals")
}

// TestFoldIsDeterministic guards the reason text and the id order: two folds of the same stream agree.
func TestFoldIsDeterministic(t *testing.T) {
	s := greenStream(t).add(change.EventFindingFiled, systemActor, change.FindingFiled{
		FindingID: id.FindingID("fnd_01ARZ3NDEKTSV4RRFFQ69G5FAW"),
		Ref:       ref(2), Tier: finding.TierE2, State: finding.StateOpen, By: "reviewer",
	})
	a := fold(t, s)
	b := fold(t, s)
	require.Equal(t, a.Blocking, b.Blocking)
	require.Equal(t, a.HumanPending, b.HumanPending)
}

// fakeSource is an Outside source that returns a fixed answer.
type fakeSource struct {
	outside gateuse.Outside
	err     error
	calls   int
}

func (f *fakeSource) Outside(context.Context, gateuse.Request) (gateuse.Outside, error) {
	f.calls++
	if f.err != nil {
		return gateuse.Outside{}, f.err
	}
	return f.outside, nil
}

// fakeStore keeps a stream in memory and can refuse the first Append calls with a version conflict.
type fakeStore struct {
	events       []change.Event
	conflicts    int
	loadErr      error
	appendErr    error
	appendCalls  int
	lastExpected int64
	lastEvents   []change.Event
	lastEffects  []change.Effect
}

func (f *fakeStore) Load(context.Context, id.ChangeID) ([]change.Event, error) {
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	return append([]change.Event(nil), f.events...), nil
}

func (f *fakeStore) Append(_ context.Context, _ id.ChangeID, expected int64, evs []change.Event, fx []change.Effect) (int64, error) {
	f.appendCalls++
	f.lastExpected = expected
	if f.appendErr != nil {
		return 0, f.appendErr
	}
	if f.conflicts > 0 {
		f.conflicts--
		return 0, ports.ErrVersionConflict
	}
	f.lastEvents = evs
	f.lastEffects = fx
	f.events = append(f.events, evs...)
	return int64(len(f.events)), nil
}

type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC) }

type fakeIDs struct{ n int }

func (f *fakeIDs) NewEvent() change.EventID {
	f.n++
	return change.EventID(fmt.Sprintf("20000000-0000-4000-8000-%012d", f.n))
}
func (f *fakeIDs) NewFinding() id.FindingID { return id.FindingID("fnd_01ARZ3NDEKTSV4RRFFQ69G5FAV") }
func (f *fakeIDs) NewRun() id.RunID         { return id.RunID("run_01ARZ3NDEKTSV4RRFFQ69G5FAW") }
func (f *fakeIDs) NewCase() id.CaseID       { return id.CaseID("case_01ARZ3NDEKTSV4RRFFQ69G5FAX") }
func (f *fakeIDs) NewTask() id.TaskID       { return id.TaskID("tsk_01ARZ3NDEKTSV4RRFFQ69G5FAV") }

// evaluate builds an Evaluator over a stream, runs it, and returns the store for assertions.
func evaluate(t *testing.T, s *stream, out gateuse.Outside) (gateuse.Result, *fakeStore) {
	t.Helper()
	store := &fakeStore{events: s.events()}
	eval, err := gateuse.New(gateuse.Config{
		Events: store,
		Source: &fakeSource{outside: out},
		Clock:  fixedClock{},
		IDs:    &fakeIDs{},
	})
	require.NoError(t, err)
	res, err := eval.Evaluate(context.Background(), changeKey)
	require.NoError(t, err)
	return res, store
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

func producedEvent(t *testing.T, store *fakeStore, typ change.EventType) change.Event {
	t.Helper()
	var found []change.Event
	for _, e := range store.lastEvents {
		if e.Type == typ {
			found = append(found, e)
		}
	}
	require.Len(t, found, 1)
	return found[0]
}

var errBoom = errors.New("boom")
