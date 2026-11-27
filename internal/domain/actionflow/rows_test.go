package actionflow_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/actionflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/external"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/fastcheck"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/summary"
)

const (
	head  id.SHA = "9f1c2ab"
	head2 id.SHA = "1a2b3c4"
	base  id.SHA = "7d3e9a1"
	hash         = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

var t0 = time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)

// pullRequest is a change with a pull request on head and nothing else: the first state a gate run sees.
func pullRequest() actionflow.In {
	return actionflow.In{
		View: change.View{
			PR:       &change.PRInfo{Number: 7, Author: "ann", HeadSHA: head, Base: base},
			Findings: map[string]change.FindingInfo{},
		},
		PolicyHash:     hash,
		Files:          []proof.File{{Path: "internal/pay/refund.go", Added: 30, Deleted: 5}},
		MonthKnown:     true,
		MonthSpentUSD:  1,
		SummaryVersion: 3,
		HeadAt:         t0,
		Now:            t0.Add(time.Minute),
	}
}

func docsOnly() actionflow.In {
	in := pullRequest()
	in.Files = []proof.File{{Path: "README.md", Added: 3}, {Path: "docs/setup.md", Added: 9, Deleted: 1}}
	return in
}

func withAction(in actionflow.In, f func(a *change.ActionInfo)) actionflow.In {
	a := &change.ActionInfo{}
	if in.View.Action != nil {
		*a = *in.View.Action
	}
	f(a)
	in.View.Action = a
	return in
}

func withMode(in actionflow.In, mode string, required bool) actionflow.In {
	in.Action = policy.Action{Mode: mode, ProofRequired: required, BreakGlass: []string{"lead"}}
	return in
}

func fastOn(h id.SHA, v gate.Verdict) func(*change.ActionInfo) {
	return func(a *change.ActionInfo) {
		a.Fast = &change.FastInfo{Head: h, Verdict: v, Clauses: []change.FastClause{
			{ID: fastcheck.ClauseSecrets, Status: fastcheck.StatusMet, Reason: "none"},
			{ID: fastcheck.ClauseApproval, Status: statusOf(v), Reason: "why"},
		}}
	}
}

func statusOf(v gate.Verdict) fastcheck.Status {
	switch v {
	case gate.VerdictFail:
		return fastcheck.StatusUnmet
	case gate.VerdictUnknown:
		return fastcheck.StatusUnknown
	case gate.VerdictPass:
		return fastcheck.StatusMet
	}
	return fastcheck.StatusMet
}

// proofOn records a run on the current head, base and policy.
func proofOn(p change.ProofInfo) func(*change.ActionInfo) {
	return func(a *change.ActionInfo) {
		p.Head, p.Base, p.PolicyHash = head, base, hash
		a.Proof = &p
	}
}

func inFlight(run int64) change.ProofInfo { return change.ProofInfo{WorkflowRun: run, Model: "m"} }

func finished(run int64, k proof.Kind, c change.ProofCounts) change.ProofInfo {
	return change.ProofInfo{WorkflowRun: run, Model: "m", Done: true, Kind: k, Counts: c}
}

func summaryFor(h id.SHA, version int) func(*change.ActionInfo) {
	return func(a *change.ActionInfo) {
		a.Summary = &change.SummaryInfo{Head: h, CommentID: 55,
			Marker: summary.Marker{Change: "pr-acme-pay-7", Version: version, Head: string(h)}.String()}
	}
}

func rowsOf(in actionflow.In) map[actionflow.RowID]actionflow.Row {
	out := map[actionflow.RowID]actionflow.Row{}
	for _, r := range actionflow.Decide(in) {
		out[r.ID] = r
	}
	return out
}

func requireRow(t *testing.T, rows map[actionflow.RowID]actionflow.Row, id actionflow.RowID, st actionflow.State, reason string) actionflow.Row {
	t.Helper()
	r, ok := rows[id]
	require.True(t, ok, "row %s is missing", id)
	require.Equal(t, st, r.State, "row %s: %s", id, r.Reason)
	require.Contains(t, r.Reason, reason, "row %s", id)
	return r
}

func ids(rows []actionflow.Row) []actionflow.RowID {
	var out []actionflow.RowID
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

func TestTheTableHasEveryRowOnceInOrderAndIsDeterministic(t *testing.T) {
	want := []actionflow.RowID{actionflow.RowState, actionflow.RowFast, actionflow.RowIngest, actionflow.RowProof, actionflow.RowFindings,
		actionflow.RowBudget, actionflow.RowBreakGlass, actionflow.RowSweep, actionflow.RowPublish}
	require.Equal(t, want, actionflow.RowIDs())
	for _, in := range []actionflow.In{pullRequest(), docsOnly(), {}, withAction(pullRequest(), fastOn(head, gate.VerdictPass))} {
		a, b := actionflow.Decide(in), actionflow.Decide(in)
		require.Equal(t, want, ids(a))
		require.Equal(t, a, b)
		for _, r := range a {
			require.NotEmpty(t, r.Reason, r.ID)
		}
	}
}

func TestAFreshPullRequestNeedsTheFastCheckAndWaitsForTheProofRun(t *testing.T) {
	rows := rowsOf(pullRequest())
	requireRow(t, rows, actionflow.RowState, actionflow.StateDone, "")
	fast := requireRow(t, rows, actionflow.RowFast, actionflow.StateNeeded, "no fast check")
	require.Equal(t, []actionflow.Effect{actionflow.RunFast{Head: head}}, fast.Effects)
	requireRow(t, rows, actionflow.RowIngest, actionflow.StateDone, "no third-party")
	proofRow := requireRow(t, rows, actionflow.RowProof, actionflow.StateWaiting, "has not started")
	require.Empty(t, proofRow.Effects, "GitHub starts the proof workflow, not the gate")
	requireRow(t, rows, actionflow.RowFindings, actionflow.StateDone, "no finding")
	requireRow(t, rows, actionflow.RowBudget, actionflow.StateDone, "within")
	requireRow(t, rows, actionflow.RowBreakGlass, actionflow.StateDone, "no break-glass")
	requireRow(t, rows, actionflow.RowSweep, actionflow.StateWaiting, "left to report")
	pub := requireRow(t, rows, actionflow.RowPublish, actionflow.StateWaiting, "fast check")
	require.Empty(t, pub.Effects)
	require.Equal(t, []actionflow.Effect{actionflow.RunFast{Head: head}}, actionflow.Effects(actionflow.Decide(pullRequest())))
}

func TestTheFastResultIsFreshOnlyForTheHeadItRanOn(t *testing.T) {
	fresh := rowsOf(withAction(pullRequest(), fastOn(head, gate.VerdictPass)))
	requireRow(t, fresh, actionflow.RowFast, actionflow.StateDone, "pass")

	stale := rowsOf(withAction(pullRequest(), fastOn(head2, gate.VerdictPass)))
	r := requireRow(t, stale, actionflow.RowFast, actionflow.StateNeeded, string(head2))
	require.Equal(t, []actionflow.Effect{actionflow.RunFast{Head: head}}, r.Effects)

	bad := rowsOf(withAction(pullRequest(), fastOn(head, gate.Verdict("maybe"))))
	requireRow(t, bad, actionflow.RowFast, actionflow.StateNeeded, "verdict")
}

func TestADocsOnlyPullRequestSkipsTheProofRunAtZeroCost(t *testing.T) {
	rows := rowsOf(docsOnly())
	r := requireRow(t, rows, actionflow.RowProof, actionflow.StateNeeded, "trivial")
	require.Equal(t, []actionflow.Effect{actionflow.SkipProof{Head: head, Base: base, PolicyHash: hash,
		Detail: "every changed path matches an action.trivial glob"}}, r.Effects)
	requireRow(t, rows, actionflow.RowBudget, actionflow.StateDone, "$0")
	requireRow(t, rows, actionflow.RowSweep, actionflow.StateDone, "trivial")

	skipped := withAction(docsOnly(), proofOn(finished(1, proof.KindSkippedTrivial, change.ProofCounts{})))
	requireRow(t, rowsOf(skipped), actionflow.RowProof, actionflow.StateDone, "skipped: trivial")

	pending := withAction(docsOnly(), proofOn(inFlight(4)))
	got := requireRow(t, rowsOf(pending), actionflow.RowProof, actionflow.StateWaiting, "in flight")
	require.Empty(t, got.Effects, "a run that began on a trivial change reports its own skip")
}

func TestATrivialRecordIsNotTrustedWhenTheRuleNoLongerHolds(t *testing.T) {
	in := withAction(pullRequest(), proofOn(finished(1, proof.KindSkippedTrivial, change.ProofCounts{})))
	r := requireRow(t, rowsOf(in), actionflow.RowProof, actionflow.StateUnknown, "trivial")
	require.Equal(t, proof.ReasonNoResult, r.Why)
}

func TestAPushMidProofSupersedesTheOldRunAndItsFastResult(t *testing.T) {
	in := withAction(pullRequest(), func(a *change.ActionInfo) {
		fastOn(head, gate.VerdictPass)(a)
		proofOn(inFlight(10))(a)
	})
	in.View.PR.HeadSHA = head2 // pr.pushed

	rows := rowsOf(in)
	f := requireRow(t, rows, actionflow.RowFast, actionflow.StateNeeded, string(head))
	require.Equal(t, []actionflow.Effect{actionflow.RunFast{Head: head2}}, f.Effects)
	p := requireRow(t, rows, actionflow.RowProof, actionflow.StateWaiting, "superseded")
	require.Equal(t, proof.ReasonSuperseded, p.Why)
	require.Empty(t, p.Effects, "the old run is cancelled by GitHub and its result is ignored")
	requireRow(t, rows, actionflow.RowPublish, actionflow.StateWaiting, "fast check")
}

func TestAProofResultForAnOldHeadIsNeverUsed(t *testing.T) {
	in := withAction(pullRequest(), func(a *change.ActionInfo) {
		fastOn(head2, gate.VerdictPass)(a)
		proofOn(finished(9, proof.KindProven, change.ProofCounts{}))(a)
	})
	in.View.PR.HeadSHA = head2
	in.View.Findings["F1"] = change.FindingInfo{Tier: finding.TierE1, State: finding.StateOpen,
		Proof: &change.FindingProofInfo{Head: head, Class: proof.ClassProven, Blocking: true}}

	rows := rowsOf(in)
	requireRow(t, rows, actionflow.RowProof, actionflow.StateWaiting, "old head")
	r := requireRow(t, rows, actionflow.RowFindings, actionflow.StateWaiting, "not yet proven")
	require.NotContains(t, r.Reason, "blocking", "a proof of the old head cannot block the new one")
	pub := requireRow(t, rows, actionflow.RowPublish, actionflow.StateNeeded, "")
	for _, e := range pub.Effects {
		if c, ok := e.(actionflow.PostChecks); ok {
			require.Nil(t, c.Proof, "no proof check is posted for a head the proof did not cover")
		}
	}
}

func TestAProofRunOnOtherInputsIsNotCurrent(t *testing.T) {
	in := withAction(pullRequest(), func(a *change.ActionInfo) {
		p := finished(9, proof.KindProven, change.ProofCounts{})
		p.Head, p.Base, p.PolicyHash = head, "0000000", hash
		a.Proof = &p
	})
	requireRow(t, rowsOf(in), actionflow.RowProof, actionflow.StateWaiting, "base")

	in = withAction(pullRequest(), proofOn(finished(9, proof.KindProven, change.ProofCounts{})))
	in.PolicyHash = strings.Repeat("b", 64)
	requireRow(t, rowsOf(in), actionflow.RowProof, actionflow.StateWaiting, "policy")
}

func TestTheSweepClosesAProofRunThatNeverReported(t *testing.T) {
	in := withAction(pullRequest(), proofOn(inFlight(10)))
	in.Now = t0.Add(19 * time.Minute)
	requireRow(t, rowsOf(in), actionflow.RowProof, actionflow.StateWaiting, "in flight")
	requireRow(t, rowsOf(in), actionflow.RowSweep, actionflow.StateWaiting, "1m0s left")

	in.Now = t0.Add(21 * time.Minute)
	r := requireRow(t, rowsOf(in), actionflow.RowSweep, actionflow.StateNeeded, "20m0s")
	require.Equal(t, []actionflow.Effect{actionflow.ReportProofUnknown{Head: head, Base: base, PolicyHash: hash,
		Reason: proof.ReasonNoResult, WorkflowRun: 10}}, r.Effects)

	none := pullRequest()
	none.Now = t0.Add(time.Hour)
	r = requireRow(t, rowsOf(none), actionflow.RowSweep, actionflow.StateNeeded, "")
	require.Equal(t, int64(0), r.Effects[0].(actionflow.ReportProofUnknown).WorkflowRun, "no run to close: the gate uses its own id")

	closed := withAction(in, proofOn(finished(0, proof.KindUnknown, change.ProofCounts{})))
	closed.View.Action.Proof.Reason = proof.ReasonNoResult
	closed.View.Action.Proof.WorkflowRun = 77
	requireRow(t, rowsOf(closed), actionflow.RowSweep, actionflow.StateDone, "reported")
	requireRow(t, rowsOf(closed), actionflow.RowProof, actionflow.StateUnknown, "no result")
}

func TestTheSweepNeverGuessesWithoutAClock(t *testing.T) {
	in := pullRequest()
	in.Now = time.Time{}
	r := requireRow(t, rowsOf(in), actionflow.RowSweep, actionflow.StateWaiting, "clock")
	require.Empty(t, r.Effects)
}

func TestAFinishedRunMapsToTheStateOfItsKind(t *testing.T) {
	cases := []struct {
		name   string
		p      change.ProofInfo
		state  actionflow.State
		reason string
		why    proof.Reason
	}{
		{"proven", finished(1, proof.KindProven, change.ProofCounts{}), actionflow.StateDone, "proven", ""},
		{"blocked", finished(1, proof.KindBlocked, change.ProofCounts{In: 1, Proven: 1, Blocking: 1}), actionflow.StateDone, "blocked", ""},
		{"not reproduced", finished(1, proof.KindNotReproduced, change.ProofCounts{}), actionflow.StateUnknown, "not reproduced", ""},
		{"unknown with a reason", func() change.ProofInfo {
			p := finished(1, proof.KindUnknown, change.ProofCounts{})
			p.Reason = proof.ReasonDeadline
			return p
		}(), actionflow.StateUnknown, "timeout", proof.ReasonDeadline},
		{"a kind nobody defined", finished(1, proof.Kind("weird"), change.ProofCounts{}), actionflow.StateUnknown, "no result", proof.ReasonNoResult},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := requireRow(t, rowsOf(withAction(pullRequest(), proofOn(c.p))), actionflow.RowProof, c.state, c.reason)
			require.Equal(t, c.why, r.Why)
		})
	}
}

func TestABudgetHitStopsTheRowsAndNeverPasses(t *testing.T) {
	over := finished(5, proof.KindUnknown, change.ProofCounts{In: 2, NotChecked: 2})
	over.Reason = proof.ReasonBudget
	over.Cost = change.Cost{USDMicros: 700_000, PromptTokens: 1000}
	rows := rowsOf(withAction(pullRequest(), proofOn(over)))
	b := requireRow(t, rows, actionflow.RowBudget, actionflow.StateBlocked, "per-PR budget hit")
	require.Equal(t, proof.ReasonBudget, b.Why)
	p := requireRow(t, rows, actionflow.RowProof, actionflow.StateUnknown, "budget hit")
	require.Equal(t, proof.ReasonBudget, p.Why)

	exact := finished(5, proof.KindProven, change.ProofCounts{})
	exact.Cost = change.Cost{USDMicros: 500_000}
	requireRow(t, rowsOf(withAction(pullRequest(), proofOn(exact))), actionflow.RowBudget, actionflow.StateDone, "$0.50 of $0.50")

	run := withAction(pullRequest(), func(a *change.ActionInfo) {
		proofOn(inFlight(8))(a)
		a.Costs = []change.CostRecorded{{WorkflowRun: 8, Model: "m", Cost: change.Cost{USDMicros: 600_000}},
			{WorkflowRun: 2, Model: "m", Cost: change.Cost{USDMicros: 9_000_000}}}
	})
	requireRow(t, rowsOf(run), actionflow.RowBudget, actionflow.StateBlocked, "$0.60 of $0.50")
}

func TestTheMonthlyCapStopsAProofRunBeforeItStarts(t *testing.T) {
	in := pullRequest()
	in.MonthSpentUSD = 49.75 // plus the $0.50 per-PR budget is over the $50 cap
	rows := rowsOf(in)
	b := requireRow(t, rows, actionflow.RowBudget, actionflow.StateBlocked, "monthly cap")
	require.Equal(t, proof.ReasonMonthlyCap, b.Why)
	p := requireRow(t, rows, actionflow.RowProof, actionflow.StateNeeded, "monthly cap")
	require.Equal(t, []actionflow.Effect{actionflow.ReportProofUnknown{Head: head, Base: base, PolicyHash: hash, Reason: proof.ReasonMonthlyCap}}, p.Effects)
	requireRow(t, rows, actionflow.RowSweep, actionflow.StateWaiting, "own result")

	recorded := withAction(in, proofOn(func() change.ProofInfo {
		p := finished(3, proof.KindUnknown, change.ProofCounts{})
		p.Reason = proof.ReasonMonthlyCap
		return p
	}()))
	requireRow(t, rowsOf(recorded), actionflow.RowProof, actionflow.StateUnknown, "monthly cap")

	unknown := pullRequest()
	unknown.MonthKnown = false
	rows = rowsOf(unknown)
	requireRow(t, rows, actionflow.RowBudget, actionflow.StateUnknown, "monthly spend")
	requireRow(t, rows, actionflow.RowProof, actionflow.StateWaiting, "has not started")

	nan := pullRequest()
	nan.MonthSpentUSD = -1
	requireRow(t, rowsOf(nan), actionflow.RowBudget, actionflow.StateBlocked, "monthly cap")
}

func ingested(n int64, claim string) change.ExternalIngested {
	return change.ExternalIngested{Source: external.ToolCodeRabbit, CommentID: n, Head: head,
		Location: finding.Location{File: "internal/pay/refund.go", Lines: [2]int{10, 12}}, Side: external.SideRight,
		Claim: claim, Severity: finding.SeverityMedium}
}

func TestThirdPartyCommentsAreIngestedOnceAndAgainWhenTheyChange(t *testing.T) {
	in := pullRequest()
	in.Candidates = []change.ExternalIngested{ingested(1, "refund can double charge"), ingested(2, "nil pointer on empty cart")}
	r := requireRow(t, rowsOf(in), actionflow.RowIngest, actionflow.StateNeeded, "2 third-party")
	require.Equal(t, []actionflow.Effect{actionflow.IngestExternal{Head: head, Candidates: in.Candidates}}, r.Effects)

	one := withAction(in, func(a *change.ActionInfo) {
		a.External = map[int64]change.ExternalInfo{1: change.ExternalInfo(ingested(1, "refund can double charge"))}
	})
	r = requireRow(t, rowsOf(one), actionflow.RowIngest, actionflow.StateNeeded, "1 third-party")
	require.Equal(t, []actionflow.Effect{actionflow.IngestExternal{Head: head, Candidates: in.Candidates[1:]}}, r.Effects)

	all := withAction(in, func(a *change.ActionInfo) {
		a.External = map[int64]change.ExternalInfo{1: change.ExternalInfo(in.Candidates[0]), 2: change.ExternalInfo(in.Candidates[1])}
	})
	requireRow(t, rowsOf(all), actionflow.RowIngest, actionflow.StateDone, "2 third-party")

	edited := all
	edited.Candidates = []change.ExternalIngested{ingested(1, "refund double charges on retry"), in.Candidates[1]}
	r = requireRow(t, rowsOf(edited), actionflow.RowIngest, actionflow.StateNeeded, "1 third-party")
	require.Equal(t, edited.Candidates[:1], r.Effects[0].(actionflow.IngestExternal).Candidates)
}

func TestIngestSkipsCandidatesItCannotRecordOrThatAreForAnotherHead(t *testing.T) {
	other := ingested(3, "refund can double charge")
	other.Head = head2
	broken := ingested(4, "x") // a claim below the minimum
	in := pullRequest()
	in.Candidates = []change.ExternalIngested{other, broken}
	r := requireRow(t, rowsOf(in), actionflow.RowIngest, actionflow.StateDone, "ignored")
	require.Empty(t, r.Effects)
}

func TestACommentOnATrivialPullRequestIsStillRecordedAndNotChecked(t *testing.T) {
	in := docsOnly()
	in.Candidates = []change.ExternalIngested{ingested(1, "typo in the heading")}
	requireRow(t, rowsOf(in), actionflow.RowIngest, actionflow.StateNeeded, "1 third-party")

	in.View.Findings["F1"] = change.FindingInfo{Tier: finding.TierE4, State: finding.StateOpen}
	requireRow(t, rowsOf(in), actionflow.RowFindings, actionflow.StateDone, "trivial PR")
}

func proven(blocking bool) *change.FindingProofInfo {
	return &change.FindingProofInfo{Head: head, Class: proof.ClassProven, Blocking: blocking}
}

func provenBlockingRun() actionflow.In {
	in := withAction(pullRequest(), func(a *change.ActionInfo) {
		fastOn(head, gate.VerdictPass)(a)
		proofOn(finished(5, proof.KindBlocked, change.ProofCounts{In: 2, Proven: 2, Blocking: 1, NeedsPerson: 1}))(a)
	})
	in.View.Findings["F2"] = change.FindingInfo{Tier: finding.TierE1, State: finding.StateOpen, Proof: proven(true)}
	in.View.Findings["F10"] = change.FindingInfo{Tier: finding.TierE2, State: finding.StateOpen, Proof: proven(true)}
	in.View.Findings["F1"] = change.FindingInfo{Tier: finding.TierE1, State: finding.StateOpen, Proof: proven(false)}
	in.View.Findings["F3"] = change.FindingInfo{Tier: finding.TierE4, State: finding.StateDismissed}
	return in
}

func checksOf(t *testing.T, rows map[actionflow.RowID]actionflow.Row) actionflow.PostChecks {
	t.Helper()
	for _, e := range rows[actionflow.RowPublish].Effects {
		if c, ok := e.(actionflow.PostChecks); ok {
			return c
		}
	}
	require.Fail(t, "publish posts no checks", rows[actionflow.RowPublish].Reason)
	return actionflow.PostChecks{}
}

func TestAProvenBlockingFindingIsBlockedInBothModesButOnlyEnforceHoldsTheMerge(t *testing.T) {
	cases := []struct {
		name     string
		mode     string
		required bool
		reason   string
		proof    proof.Conclusion
		fast     proof.Conclusion
	}{
		{"shadow", policy.ActionModeShadow, false, "would block", proof.ConclusionNeutral, proof.ConclusionNeutral},
		{"enforce, advisory proof", policy.ActionModeEnforce, false, "advisory", proof.ConclusionNeutral, proof.ConclusionSuccess},
		{"enforce, proof required", policy.ActionModeEnforce, true, "fails the proof check", proof.ConclusionFailure, proof.ConclusionSuccess},
		{"a mode nobody defined", "strict", false, "fails the proof check", proof.ConclusionFailure, proof.ConclusionSuccess},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rows := rowsOf(withMode(provenBlockingRun(), c.mode, c.required))
			r := requireRow(t, rows, actionflow.RowFindings, actionflow.StateBlocked, c.reason)
			require.Contains(t, r.Reason, "F2, F10", "refs are listed in number order")
			require.NotContains(t, r.Reason, "F1,", "a proven finding that does not block is not listed as blocking")
			checks := checksOf(t, rows)
			require.Equal(t, c.fast, checks.Fast.Conclusion)
			require.NotNil(t, checks.Proof)
			require.Equal(t, c.proof, checks.Proof.Conclusion)
			require.Contains(t, checks.Proof.Headline, "blocked")
		})
	}
}

func TestABlockingFindingIsNotHiddenByAnUnfinishedRunButStillNeedsItsOwnHead(t *testing.T) {
	in := provenBlockingRun()
	in.View.Action.Proof.Kind, in.View.Action.Proof.Reason = proof.KindUnknown, proof.ReasonBudget
	requireRow(t, rowsOf(in), actionflow.RowFindings, actionflow.StateBlocked, "blocking")
}

func TestFindingsAwaitingProofWaitOrAreNotCheckedAfterTheRunEnded(t *testing.T) {
	in := withAction(pullRequest(), fastOn(head, gate.VerdictPass))
	in.View.Findings["F1"] = change.FindingInfo{Tier: finding.TierE4, State: finding.StateOpen}
	in.View.Findings["F2"] = change.FindingInfo{Tier: finding.TierE4, State: finding.StateOpen,
		Proof: &change.FindingProofInfo{Head: head, Class: proof.ClassDropped, Dropped: proof.DropNoProof}}
	requireRow(t, rowsOf(in), actionflow.RowFindings, actionflow.StateWaiting, "not yet proven")

	run := withAction(in, proofOn(inFlight(3)))
	requireRow(t, rowsOf(run), actionflow.RowFindings, actionflow.StateWaiting, "not yet proven")

	ended := withAction(in, proofOn(finished(3, proof.KindProven, change.ProofCounts{In: 1, Proven: 1, NeedsPerson: 1})))
	r := requireRow(t, rowsOf(ended), actionflow.RowFindings, actionflow.StateUnknown, "after the proof run")
	require.NotEqual(t, actionflow.StateBlocked, r.State, "an unproven finding never blocks")

	stopped := finished(3, proof.KindUnknown, change.ProofCounts{In: 2, NotChecked: 1})
	stopped.Reason = proof.ReasonDeadline
	r = requireRow(t, rowsOf(withAction(in, proofOn(stopped))), actionflow.RowFindings, actionflow.StateUnknown, "timeout")
	require.Equal(t, proof.ReasonDeadline, r.Why)

	allDone := pullRequest()
	allDone.View.Findings["F1"] = change.FindingInfo{Tier: finding.TierE1, State: finding.StateOpen, Proof: proven(false)}
	allDone.View.Findings["F2"] = change.FindingInfo{Tier: finding.TierE4, State: finding.StateOpen,
		Proof: &change.FindingProofInfo{Head: head, Class: proof.ClassNotChecked, Reason: proof.ReasonNotProvable}}
	requireRow(t, rowsOf(allDone), actionflow.RowFindings, actionflow.StateDone, "1 need a person")
}

func TestBreakGlassIsOpenUntilAnotherPersonReviewsIt(t *testing.T) {
	unknownFast := withMode(withAction(pullRequest(), fastOn(head, gate.VerdictUnknown)), policy.ActionModeEnforce, false)
	checks := checksOf(t, rowsOf(withAction(unknownFast, func(a *change.ActionInfo) { a.Summary = nil })))
	require.Equal(t, proof.ConclusionActionRequired, checks.Fast.Conclusion, "unknown blocks until a person clears it")

	bypass := change.BreakGlassInfo{Head: head, Login: "lead", Reason: "payments outage, fix is in", SourceRef: "c1"}
	recorded := withAction(unknownFast, func(a *change.ActionInfo) { a.BreakGlass = []change.BreakGlassInfo{bypass} })
	rows := rowsOf(recorded)
	r := requireRow(t, rows, actionflow.RowBreakGlass, actionflow.StateWaiting, "@lead")
	require.Contains(t, r.Reason, "break-glass-reviewed")
	c := checksOf(t, rows)
	require.Equal(t, proof.ConclusionSuccess, c.Fast.Conclusion)
	require.Contains(t, c.Fast.Headline, "bypassed by @lead: payments outage, fix is in")

	bypass.ReviewedBy, bypass.ReviewRef = "bea", "c2"
	reviewed := withAction(unknownFast, func(a *change.ActionInfo) { a.BreakGlass = []change.BreakGlassInfo{bypass} })
	rows = rowsOf(reviewed)
	requireRow(t, rows, actionflow.RowBreakGlass, actionflow.StateDone, "reviewed by @bea")
	require.Equal(t, proof.ConclusionSuccess, checksOf(t, rows).Fast.Conclusion, "the bypass stands after its review")
}

func TestABypassThatTheMergeBasePolicyDoesNotAllowOrForAnOldHeadChangesNothing(t *testing.T) {
	start := withMode(withAction(pullRequest(), fastOn(head, gate.VerdictFail)), policy.ActionModeEnforce, false)
	for name, b := range map[string]change.BreakGlassInfo{
		"a login not in action.break_glass": {Head: head, Login: "mallory", Reason: "i say so, it is fine", SourceRef: "c1"},
		"a bypass of the old head":          {Head: head2, Login: "lead", Reason: "payments outage, fix is in", SourceRef: "c1"},
	} {
		t.Run(name, func(t *testing.T) {
			in := withAction(start, func(a *change.ActionInfo) { a.BreakGlass = []change.BreakGlassInfo{b} })
			rows := rowsOf(in)
			requireRow(t, rows, actionflow.RowBreakGlass, actionflow.StateDone, "ignored")
			require.Equal(t, proof.ConclusionFailure, checksOf(t, rows).Fast.Conclusion)
		})
	}
}

func TestAStateResetMakesEveryProducedFactUnknownUntilItIsMadeAgain(t *testing.T) {
	// The fold keeps observed facts and the cost records and drops the rest, so the view after a reset is this.
	in := pullRequest()
	in.View.Action = &change.ActionInfo{Reset: &change.ResetInfo{Reason: "bad MAC on the chain head", Count: 1},
		Costs: []change.CostRecorded{{WorkflowRun: 2, Model: "m", Cost: change.Cost{USDMicros: 400_000}}}}
	in.View.Findings["F1"] = change.FindingInfo{Tier: finding.TierE1, State: finding.StateOpen} // its proof was dropped
	rows := rowsOf(in)
	requireRow(t, rows, actionflow.RowState, actionflow.StateDone, "reset 1 time")
	f := requireRow(t, rows, actionflow.RowFast, actionflow.StateNeeded, "state was reset")
	require.Equal(t, []actionflow.Effect{actionflow.RunFast{Head: head}}, f.Effects)
	requireRow(t, rows, actionflow.RowProof, actionflow.StateWaiting, "state was reset")
	requireRow(t, rows, actionflow.RowFindings, actionflow.StateWaiting, "not yet proven")
}

func TestAStateThatFailsVerificationStopsEverythingBeforeTheLogIsReset(t *testing.T) {
	in := withAction(pullRequest(), func(a *change.ActionInfo) {
		fastOn(head, gate.VerdictPass)(a)
		proofOn(finished(5, proof.KindProven, change.ProofCounts{}))(a)
	})
	in.StateProblem = "bad MAC on pr-acme-pay-7.head"
	rows := rowsOf(in)
	s := requireRow(t, rows, actionflow.RowState, actionflow.StateNeeded, "bad MAC")
	require.Equal(t, []actionflow.Effect{actionflow.ResetState{Reason: "bad MAC on pr-acme-pay-7.head"}}, s.Effects)
	for _, row := range actionflow.RowIDs() {
		if row == actionflow.RowState {
			continue
		}
		r := requireRow(t, rows, row, actionflow.StateUnknown, "state failed verification")
		require.Equal(t, proof.ReasonStateUnverified, r.Why, row)
		require.Empty(t, r.Effects, row)
	}
	require.Equal(t, []actionflow.Effect{actionflow.ResetState{Reason: "bad MAC on pr-acme-pay-7.head"}}, actionflow.Effects(actionflow.Decide(in)))
}

func TestFailClosedWhenTheInputsCannotBeTrusted(t *testing.T) {
	cases := []struct {
		name  string
		in    actionflow.In
		state actionflow.State
		text  string
	}{
		{"the policy cannot be read", func() actionflow.In {
			in := pullRequest()
			in.PolicyProblem = "heimdall.yaml at the merge base does not parse"
			return in
		}(), actionflow.StateBlocked, "does not parse"},
		{"no pull request yet", actionflow.In{}, actionflow.StateWaiting, "no pull request"},
		{"the change is over", func() actionflow.In {
			in := pullRequest()
			in.View.Closed = &change.ClosedInfo{Merged: true, HeadSHA: head}
			return in
		}(), actionflow.StateDone, "over"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rows := rowsOf(c.in)
			require.Len(t, rows, len(actionflow.RowIDs()))
			for id, r := range rows {
				require.Equal(t, c.state, r.State, id)
				require.Contains(t, r.Reason, c.text, id)
			}
			require.Empty(t, actionflow.Effects(actionflow.Decide(c.in)))
		})
	}
}

func TestTheProofWorkflowChangedInThePullRequestIsUnknownUntilAnOwnerApproves(t *testing.T) {
	in := withAction(pullRequest(), proofOn(finished(5, proof.KindProven, change.ProofCounts{})))
	in.ProofWorkflowChanged = true
	r := requireRow(t, rowsOf(in), actionflow.RowProof, actionflow.StateNeeded, "workflow")
	require.Equal(t, []actionflow.Effect{actionflow.ReportProofUnknown{Head: head, Base: base, PolicyHash: hash,
		Reason: proof.ReasonWorkflowChanged, WorkflowRun: 0}}, r.Effects, "a proven result from that workflow is overwritten")

	recorded := withAction(in, func(a *change.ActionInfo) {
		p := finished(6, proof.KindUnknown, change.ProofCounts{})
		p.Reason = proof.ReasonWorkflowChanged
		proofOn(p)(a)
	})
	got := requireRow(t, rowsOf(recorded), actionflow.RowProof, actionflow.StateUnknown, "workflow")
	require.Equal(t, proof.ReasonWorkflowChanged, got.Why)

	in.PolicyOwnerApproved = true
	requireRow(t, rowsOf(in), actionflow.RowProof, actionflow.StateDone, "proven")
}

func TestThePublishRowPostsTheSummaryAndChecksWhenTheyAreStale(t *testing.T) {
	ready := withAction(pullRequest(), fastOn(head, gate.VerdictPass))
	r := requireRow(t, rowsOf(ready), actionflow.RowPublish, actionflow.StateNeeded, "no summary")
	require.Len(t, r.Effects, 2)
	require.Equal(t, actionflow.PostSummary{Head: head, Version: 3}, r.Effects[0])

	cases := []struct {
		name   string
		mutate func(*change.ActionInfo)
		reason string
	}{
		{"its version differs", summaryFor(head, 2), "version 2"},
		{"its head differs", summaryFor(head2, 3), "head " + string(head2)},
		{"its marker is not a marker", func(a *change.ActionInfo) {
			a.Summary = &change.SummaryInfo{Head: head, CommentID: 55, Marker: "<!-- hello -->"}
		}, "marker"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := withAction(ready, c.mutate)
			r := requireRow(t, rowsOf(in), actionflow.RowPublish, actionflow.StateNeeded, c.reason)
			require.Equal(t, actionflow.PostSummary{Head: head, Version: 3, CommentID: 55}, r.Effects[0], "the comment is updated in place")
		})
	}

	current := withAction(ready, summaryFor(head, 3))
	got := requireRow(t, rowsOf(current), actionflow.RowPublish, actionflow.StateDone, "version 3")
	require.Empty(t, got.Effects)

	negative := current
	negative.SummaryVersion = -1
	requireRow(t, rowsOf(negative), actionflow.RowPublish, actionflow.StateNeeded, "version")
}

func TestAProofResultThatArrivesMakesTheSummaryStaleAgain(t *testing.T) {
	in := withAction(pullRequest(), func(a *change.ActionInfo) {
		fastOn(head, gate.VerdictPass)(a)
		summaryFor(head, 3)(a)
	})
	requireRow(t, rowsOf(in), actionflow.RowPublish, actionflow.StateDone, "")
	in.SummaryVersion = 4 // the proof was reported: the log reached a new version
	in = withAction(in, proofOn(finished(5, proof.KindProven, change.ProofCounts{})))
	c := checksOf(t, rowsOf(in))
	require.NotNil(t, c.Proof)
	require.Equal(t, proof.ConclusionNeutral, c.Proof.Conclusion, "shadow mode")
	require.Equal(t, "proven", c.Proof.Headline)
}

func TestMissingListsEveryRowThatIsNotDoneWithItsWords(t *testing.T) {
	needs := actionflow.Missing(actionflow.Decide(pullRequest()))
	var got []actionflow.RowID
	for _, n := range needs {
		got = append(got, n.Row)
		require.NotEmpty(t, n.Text)
	}
	require.Equal(t, []actionflow.RowID{actionflow.RowFast, actionflow.RowProof, actionflow.RowSweep, actionflow.RowPublish}, got)
	require.Empty(t, actionflow.Missing(nil))
}
