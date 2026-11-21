package change_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/external"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/fastcheck"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
)

// Invariant: the Action facts touch no existing fold. The recorded and authored logs hold no Action event, so their
// view has no action part and its JSON has no action key. TestFoldAndMissingMatchTheGoldens already proves the bytes.
func TestTheExistingLogsFoldWithNoActionFacts(t *testing.T) {
	logs, err := filepath.Glob(filepath.Join("..", "..", "..", "testdata", "plan", "view", "*", "log.jsonl"))
	require.NoError(t, err)
	require.NotEmpty(t, logs)
	for _, log := range logs {
		v, err := change.Fold(readEvents(t, log))
		require.NoError(t, err)
		require.Nil(t, v.Action, log)
		require.NotContains(t, encode(t, v), `"action"`, log)
		require.NotContains(t, encode(t, v), `"proof"`, log)
		for ref, f := range v.Findings {
			require.Nil(t, f.Proof, "%s %s", log, ref)
		}
	}
}

func TestAViewWithNoActionEventHasNoActionPart(t *testing.T) {
	v := fold(t, opened(t))
	require.Nil(t, v.Action)
}

func actionEv(t *testing.T, n int, typ change.EventType, payload any) change.Event {
	t.Helper()
	return ev(t, n, typ, payload)
}

func clause(st fastcheck.Status) change.FastClause {
	return change.FastClause{ID: fastcheck.ClauseSecrets, Status: st, Reason: "because"}
}

func TestFastReportedKeepsTheLastResultAndItsHead(t *testing.T) {
	v := fold(t, opened(t),
		actionEv(t, 2, change.EventFastReported, change.FastReported{Head: fHead, Verdict: gate.VerdictFail,
			Clauses: []change.FastClause{clause(fastcheck.StatusUnmet)}}),
		actionEv(t, 3, change.EventPRPushed, change.PRPushed{Repo: "acme/pay", Number: 7, HeadSHA: fHead2}),
		actionEv(t, 4, change.EventFastReported, change.FastReported{Head: fHead2, Verdict: gate.VerdictPass,
			Clauses: []change.FastClause{clause(fastcheck.StatusMet)}}))
	require.NotNil(t, v.Action)
	require.Equal(t, fHead2, string(v.Action.Fast.Head))
	require.Equal(t, gate.VerdictPass, v.Action.Fast.Verdict)
	require.Equal(t, []change.FastClause{clause(fastcheck.StatusMet)}, v.Action.Fast.Clauses)
}

func TestAFastResultForAnOldHeadStaysStampedWithThatHead(t *testing.T) {
	v := fold(t, opened(t),
		actionEv(t, 2, change.EventFastReported, change.FastReported{Head: fHead, Verdict: gate.VerdictPass,
			Clauses: []change.FastClause{clause(fastcheck.StatusMet)}}),
		actionEv(t, 3, change.EventPRPushed, change.PRPushed{Repo: "acme/pay", Number: 7, HeadSHA: fHead2}))
	require.Equal(t, fHead, string(v.Action.Fast.Head), "a push does not rewrite what the check said")
	require.Equal(t, fHead2, string(v.PR.HeadSHA))
}

func proofKey() change.ProofStarted {
	return change.ProofStarted{WorkflowRun: 10, Head: fHead, Base: fBase, PolicyHash: aHash, Model: "m"}
}

func TestProofStartedThenReportedFoldsToOneFinishedRun(t *testing.T) {
	started := proofKey()
	v := fold(t, opened(t), actionEv(t, 2, change.EventProofStarted, started))
	require.Equal(t, change.ProofInfo{WorkflowRun: 10, Head: fHead, Base: fBase, PolicyHash: aHash, Model: "m"}, *v.Action.Proof)
	require.False(t, v.Action.Proof.Done)

	rep := change.ProofReported{WorkflowRun: 10, Head: fHead, Base: fBase, PolicyHash: aHash, Model: "m", Kind: proof.KindUnknown, Reason: proof.ReasonBudget,
		Counts: change.ProofCounts{In: 1, NotChecked: 1}, Cost: change.Cost{USDMicros: 500000, PromptTokens: 800000, CompletionTokens: 40000}}
	v = fold(t, opened(t), actionEv(t, 2, change.EventProofStarted, started), actionEv(t, 3, change.EventProofReported, rep))
	got := *v.Action.Proof
	require.True(t, got.Done)
	require.Equal(t, proof.KindUnknown, got.Kind)
	require.Equal(t, proof.ReasonBudget, got.Reason)
	require.Equal(t, rep.Counts, got.Counts)
	require.Equal(t, rep.Cost, got.Cost)
	require.Equal(t, int64(10), got.WorkflowRun)
}

func TestAProofReportWithNoStartIsStillAFinishedRun(t *testing.T) {
	rep := change.ProofReported{WorkflowRun: 3, Head: fHead, Base: fBase, PolicyHash: aHash, Kind: proof.KindSkippedTrivial}
	v := fold(t, opened(t), actionEv(t, 2, change.EventProofReported, rep))
	require.True(t, v.Action.Proof.Done)
	require.Equal(t, proof.KindSkippedTrivial, v.Action.Proof.Kind)
}

func TestALateStartNeverReopensAFinishedRunOfTheSameKey(t *testing.T) {
	rep := change.ProofReported{WorkflowRun: 10, Head: fHead, Base: fBase, PolicyHash: aHash, Model: "m", Kind: proof.KindProven}
	v := fold(t, opened(t),
		actionEv(t, 2, change.EventProofStarted, proofKey()),
		actionEv(t, 3, change.EventProofReported, rep),
		actionEv(t, 4, change.EventProofStarted, proofKey()))
	require.True(t, v.Action.Proof.Done, "a repeated start (a crash after the append) is not a new run")
}

func TestANewRunReplacesTheProofOfAnOldOne(t *testing.T) {
	rep := change.ProofReported{WorkflowRun: 10, Head: fHead, Base: fBase, PolicyHash: aHash, Model: "m", Kind: proof.KindProven}
	next := change.ProofStarted{WorkflowRun: 11, Head: fHead2, Base: fBase, PolicyHash: aHash, Model: "m"}
	v := fold(t, opened(t),
		actionEv(t, 2, change.EventProofReported, rep),
		actionEv(t, 3, change.EventProofStarted, next))
	require.Equal(t, int64(11), v.Action.Proof.WorkflowRun)
	require.Equal(t, fHead2, string(v.Action.Proof.Head))
	require.False(t, v.Action.Proof.Done)
}

func fref(n int) id.FindingRef {
	r, err := id.NewFindingRef(n)
	if err != nil {
		panic(err)
	}
	return r
}

func filedRec(n int) change.FindingRecord {
	return change.FindingRecord{FindingID: id.FindingID("fnd_01HZX3K9QW7M2P5R8T0V4YB6C" + string(rune('A'+n))), Ref: fref(n), Tier: finding.TierE4,
		Severity: "high", State: "open", By: "coderabbit"}
}

func TestProofReportedMarksTheFindingsItNames(t *testing.T) {
	rep := change.ProofReported{WorkflowRun: 10, Head: fHead, Base: fBase, PolicyHash: aHash, Model: "m", Kind: proof.KindBlocked,
		Counts: change.ProofCounts{In: 3, Proven: 1, Blocking: 1, Refuted: 1, NotChecked: 1},
		Results: []change.CandidateResult{
			{Ref: fref(1), Class: proof.ClassProven, Blocking: true},
			{Ref: fref(2), Class: proof.ClassDropped, Dropped: proof.DropRefuted},
			{Ref: fref(3), Class: proof.ClassNotChecked, Reason: proof.ReasonBudget},
			{Ref: fref(9), Class: proof.ClassProven}, // no such finding in this log: a fact about nothing we hold
		}}
	rep.Counts.In = 4
	rep.Counts.Proven = 2
	rep.Counts.NeedsPerson = 1
	v := fold(t, opened(t),
		actionEv(t, 2, change.EventFindingFiled, filedRec(1)),
		actionEv(t, 3, change.EventFindingFiled, filedRec(2)),
		actionEv(t, 4, change.EventFindingFiled, filedRec(3)),
		actionEv(t, 5, change.EventProofReported, rep))
	require.Equal(t, &change.FindingProofInfo{Head: fHead, Class: proof.ClassProven, Blocking: true}, v.Findings["F1"].Proof)
	require.Equal(t, &change.FindingProofInfo{Head: fHead, Class: proof.ClassDropped, Dropped: proof.DropRefuted}, v.Findings["F2"].Proof)
	require.Equal(t, &change.FindingProofInfo{Head: fHead, Class: proof.ClassNotChecked, Reason: proof.ReasonBudget}, v.Findings["F3"].Proof)
	require.NotContains(t, v.Findings, "F9")
	require.Equal(t, finding.TierE4, v.Findings["F1"].Tier, "a proof result does not move the tier: finding events do")
}

func TestAFindingFiledAfterItsProofResultKeepsNoResultFromBefore(t *testing.T) {
	// A repeated finding.filed replaces the record, as it always did; the proof result belongs to a run, so it goes too.
	rep := change.ProofReported{WorkflowRun: 10, Head: fHead, Base: fBase, PolicyHash: aHash, Kind: proof.KindProven,
		Counts:  change.ProofCounts{In: 1, Proven: 1, NeedsPerson: 1},
		Results: []change.CandidateResult{{Ref: fref(1), Class: proof.ClassProven}}}
	v := fold(t,
		actionEv(t, 1, change.EventFindingFiled, filedRec(1)),
		actionEv(t, 2, change.EventProofReported, rep))
	require.NotNil(t, v.Findings["F1"].Proof)
	v = fold(t,
		actionEv(t, 1, change.EventFindingFiled, filedRec(1)),
		actionEv(t, 2, change.EventProofReported, rep),
		actionEv(t, 3, change.EventFindingFiled, filedRec(1)))
	require.Nil(t, v.Findings["F1"].Proof)
}

func TestCostRecordedAddsUpPerRunAndModelAndIgnoresARepeat(t *testing.T) {
	a := change.CostRecorded{WorkflowRun: 10, Model: "flash", Cost: change.Cost{USDMicros: 200000, PromptTokens: 1000, CompletionTokens: 100}}
	b := change.CostRecorded{WorkflowRun: 10, Model: "pro", Cost: change.Cost{USDMicros: 50000, PromptTokens: 500, CompletionTokens: 50}}
	c := change.CostRecorded{WorkflowRun: 11, Model: "flash", Cost: change.Cost{USDMicros: 1, PromptTokens: 2, CompletionTokens: 3}}
	once := fold(t, actionEv(t, 1, change.EventCostRecorded, a), actionEv(t, 2, change.EventCostRecorded, b), actionEv(t, 3, change.EventCostRecorded, c))
	twice := fold(t, actionEv(t, 1, change.EventCostRecorded, a), actionEv(t, 2, change.EventCostRecorded, b), actionEv(t, 3, change.EventCostRecorded, c),
		actionEv(t, 4, change.EventCostRecorded, a))
	require.Equal(t, once, twice, "a repeated record folds to the same view (H1)")
	require.Len(t, once.Action.Costs, 3)
	require.Equal(t, change.Cost{USDMicros: 250001, PromptTokens: 1502, CompletionTokens: 153}, once.Action.CostTotal())
}

func TestCostTotalOfAChangeWithNoCostIsZero(t *testing.T) {
	var none *change.ActionInfo
	require.Equal(t, change.Cost{}, none.CostTotal(), "a change with no Action event has no action part, and no cost")
	require.False(t, none.OpenBreakGlass(fHead))
	require.Equal(t, change.Cost{}, fold(t, opened(t)).Action.CostTotal())
}

func ingested(comment int64, claim string) change.ExternalIngested {
	return change.ExternalIngested{Source: external.ToolCodeRabbit, CommentID: comment, Head: fHead,
		Location: finding.Location{File: "pay/refund.go", Lines: [2]int{40, 44}}, Side: external.SideRight, Claim: claim, Severity: finding.SeverityHigh}
}

func TestExternalIngestedIsKeptPerCommentSoARepeatChangesNothing(t *testing.T) {
	once := fold(t, actionEv(t, 1, change.EventExternalIngested, ingested(11, "the amount is not checked")))
	twice := fold(t, actionEv(t, 1, change.EventExternalIngested, ingested(11, "the amount is not checked")),
		actionEv(t, 2, change.EventExternalIngested, ingested(11, "the amount is not checked")))
	require.Equal(t, once, twice)
	require.Len(t, once.Action.External, 1)
	got := once.Action.External[11]
	require.Equal(t, external.ToolCodeRabbit, got.Source)
	require.Equal(t, "the amount is not checked", got.Claim)
	require.Equal(t, finding.SeverityHigh, got.Severity)
	require.Equal(t, "pay/refund.go", got.Location.File)
}

func TestAnEditedCommentReplacesItsEarlierCandidate(t *testing.T) {
	v := fold(t, actionEv(t, 1, change.EventExternalIngested, ingested(11, "the first claim is long enough")),
		actionEv(t, 2, change.EventExternalIngested, ingested(12, "another comment, a second claim")),
		actionEv(t, 3, change.EventExternalIngested, ingested(11, "the edited claim is long enough")))
	require.Len(t, v.Action.External, 2)
	require.Equal(t, "the edited claim is long enough", v.Action.External[11].Claim)
}

func TestSummaryPostedKeepsTheLatestComment(t *testing.T) {
	v := fold(t,
		actionEv(t, 1, change.EventSummaryPosted, change.SummaryPosted{Head: fHead, CommentID: 5, Marker: "<!-- m1 -->"}),
		actionEv(t, 2, change.EventSummaryPosted, change.SummaryPosted{Head: fHead2, CommentID: 5, Marker: "<!-- m2 -->"}))
	require.Equal(t, &change.SummaryInfo{Head: fHead2, CommentID: 5, Marker: "<!-- m2 -->"}, v.Action.Summary)
}

func bgRecorded(login string, source string) change.BreakGlassRecorded {
	return change.BreakGlassRecorded{Head: fHead, Login: change.ActorID(login), Reason: "production incident, rollback blocked", SourceRef: source}
}

func TestABreakGlassIsOpenUntilAnotherPersonReviewsIt(t *testing.T) {
	v := fold(t, opened(t), actionEv(t, 2, change.EventBreakGlassRecorded, bgRecorded("alice", "comment-1")))
	require.Len(t, v.Action.BreakGlass, 1)
	bg := v.Action.BreakGlass[0]
	require.Equal(t, change.ActorID("alice"), bg.Login)
	require.Equal(t, "production incident, rollback blocked", bg.Reason)
	require.True(t, bg.Open())
	require.True(t, v.Action.OpenBreakGlass(fHead))
	require.False(t, v.Action.OpenBreakGlass(fHead2), "a break-glass is for the head it was recorded on")

	v = fold(t, opened(t), actionEv(t, 2, change.EventBreakGlassRecorded, bgRecorded("alice", "comment-1")),
		actionEv(t, 3, change.EventBreakGlassReviewed, change.BreakGlassReviewed{Head: fHead, Login: "bob", SourceRef: "comment-2"}))
	require.False(t, v.Action.BreakGlass[0].Open())
	require.Equal(t, change.ActorID("bob"), v.Action.BreakGlass[0].ReviewedBy)
	require.Equal(t, "comment-2", v.Action.BreakGlass[0].ReviewRef)
	require.False(t, v.Action.OpenBreakGlass(fHead))
}

func TestThePersonWhoBypassedCannotReviewTheirOwnBypass(t *testing.T) {
	v := fold(t, opened(t), actionEv(t, 2, change.EventBreakGlassRecorded, bgRecorded("alice", "comment-1")),
		actionEv(t, 3, change.EventBreakGlassReviewed, change.BreakGlassReviewed{Head: fHead, Login: "alice", SourceRef: "comment-2"}))
	require.True(t, v.Action.BreakGlass[0].Open(), "ACTION_MODE 5.4: another approver posts the review")
	require.True(t, v.Action.OpenBreakGlass(fHead))
}

func TestAReviewClosesOnlyBypassesOfItsOwnHeadAndOthersStayOpen(t *testing.T) {
	other := bgRecorded("carol", "comment-7")
	other.Head = fHead2
	v := fold(t, opened(t),
		actionEv(t, 2, change.EventBreakGlassRecorded, bgRecorded("alice", "comment-1")),
		actionEv(t, 3, change.EventBreakGlassRecorded, other),
		actionEv(t, 4, change.EventBreakGlassReviewed, change.BreakGlassReviewed{Head: fHead2, Login: "bob", SourceRef: "comment-8"}))
	require.True(t, v.Action.BreakGlass[0].Open())
	require.False(t, v.Action.BreakGlass[1].Open())
}

func TestABreakGlassRecordedTwiceFromTheSameCommentFoldsOnce(t *testing.T) {
	once := fold(t, actionEv(t, 1, change.EventBreakGlassRecorded, bgRecorded("alice", "comment-1")))
	twice := fold(t, actionEv(t, 1, change.EventBreakGlassRecorded, bgRecorded("alice", "comment-1")),
		actionEv(t, 2, change.EventBreakGlassRecorded, bgRecorded("alice", "comment-1")))
	require.Equal(t, once, twice)
}

func TestAReviewWithNothingToReviewChangesNothing(t *testing.T) {
	v := fold(t, actionEv(t, 1, change.EventBreakGlassReviewed, change.BreakGlassReviewed{Head: fHead, Login: "bob", SourceRef: "comment-2"}))
	require.Empty(t, v.Action.BreakGlass)
}

func TestStateResetRecordsWhyAndDropsEveryProducedFactBeforeIt(t *testing.T) {
	rep := change.ProofReported{WorkflowRun: 10, Head: fHead, Base: fBase, PolicyHash: aHash, Kind: proof.KindProven,
		Counts:  change.ProofCounts{In: 1, Proven: 1, NeedsPerson: 1},
		Results: []change.CandidateResult{{Ref: fref(1), Class: proof.ClassProven}}}
	cost := change.CostRecorded{WorkflowRun: 10, Model: "m", Cost: change.Cost{USDMicros: 7}}
	v := fold(t, opened(t),
		actionEv(t, 2, change.EventFindingFiled, filedRec(1)),
		actionEv(t, 3, change.EventFastReported, change.FastReported{Head: fHead, Verdict: gate.VerdictPass, Clauses: []change.FastClause{clause(fastcheck.StatusMet)}}),
		actionEv(t, 4, change.EventProofReported, rep),
		actionEv(t, 5, change.EventExternalIngested, ingested(11, "the amount is not checked")),
		actionEv(t, 6, change.EventSummaryPosted, change.SummaryPosted{Head: fHead, CommentID: 5, Marker: "<!-- m -->"}),
		actionEv(t, 7, change.EventBreakGlassRecorded, bgRecorded("alice", "comment-1")),
		actionEv(t, 8, change.EventCostRecorded, cost),
		actionEv(t, 9, change.EventStateReset, change.StateReset{Reason: "state log failed its MAC check"}))
	a := v.Action
	require.Equal(t, &change.ResetInfo{Reason: "state log failed its MAC check", Count: 1}, a.Reset)
	require.Nil(t, a.Fast)
	require.Nil(t, a.Proof)
	require.Nil(t, a.Summary)
	require.Empty(t, a.External)
	require.Empty(t, a.BreakGlass)
	require.False(t, a.OpenBreakGlass(fHead), "a forged bypass cannot survive a reset")
	require.Nil(t, v.Findings["F1"].Proof)
	require.NotNil(t, v.PR, "observed facts are not touched: GitHub holds them")
	require.Equal(t, change.Cost{USDMicros: 7}, a.CostTotal(), "money that was spent stays spent")
}

func TestFactsAfterAResetFoldNormallyAndResetsAreCounted(t *testing.T) {
	v := fold(t,
		actionEv(t, 1, change.EventStateReset, change.StateReset{Reason: "chain broken"}),
		actionEv(t, 2, change.EventFastReported, change.FastReported{Head: fHead, Verdict: gate.VerdictPass, Clauses: []change.FastClause{clause(fastcheck.StatusMet)}}),
		actionEv(t, 3, change.EventStateReset, change.StateReset{Reason: "mac wrong"}))
	require.Equal(t, &change.ResetInfo{Reason: "mac wrong", Count: 2}, v.Action.Reset)
	require.Nil(t, v.Action.Fast)
}

// H1: executors may record a fact twice. Each Action event, repeated, folds to the same view.
func TestEveryActionFactFoldsTheSameWhenRepeated(t *testing.T) {
	facts := []change.Event{
		actionEv(t, 2, change.EventFastReported, change.FastReported{Head: fHead, Verdict: gate.VerdictPass, Clauses: []change.FastClause{clause(fastcheck.StatusMet)}}),
		actionEv(t, 3, change.EventProofStarted, proofKey()),
		actionEv(t, 4, change.EventProofReported, change.ProofReported{WorkflowRun: 10, Head: fHead, Base: fBase, PolicyHash: aHash, Kind: proof.KindProven}),
		actionEv(t, 5, change.EventCostRecorded, change.CostRecorded{WorkflowRun: 10, Model: "m", Cost: change.Cost{USDMicros: 7}}),
		actionEv(t, 6, change.EventExternalIngested, ingested(11, "the amount is not checked")),
		actionEv(t, 7, change.EventSummaryPosted, change.SummaryPosted{Head: fHead, CommentID: 5, Marker: "<!-- m -->"}),
		actionEv(t, 8, change.EventBreakGlassRecorded, bgRecorded("alice", "comment-1")),
		actionEv(t, 9, change.EventBreakGlassReviewed, change.BreakGlassReviewed{Head: fHead, Login: "bob", SourceRef: "comment-2"}),
	}
	for i, f := range facts {
		base := append([]change.Event{opened(t)}, facts[:i+1]...)
		dup := append(append([]change.Event{}, base...), f)
		require.Equal(t, fold(t, base...), fold(t, dup...), string(f.Type))
	}
}

func TestFoldRefusesAnActionEventWhoseActorBreaksTheBreakGlassRule(t *testing.T) {
	e := actionEv(t, 1, change.EventBreakGlassRecorded, bgRecorded("alice", "comment-1"))
	e.Actor = change.Actor{Kind: change.ActorAgent, ID: "test-author"}
	_, err := change.Fold([]change.Event{e})
	require.Error(t, err)
}
