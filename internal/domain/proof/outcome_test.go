package proof_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
)

// finished is a run that ended by itself, proved its plan tests and found nothing that blocks.
func finished() proof.Facts {
	return proof.Facts{
		Tests: proof.TestFacts{Ran: true, Accepted: 2, Rejected: 1},
		Findings: []proof.Finding{
			{Class: proof.ClassProven},
			{Class: proof.ClassDropped, Dropped: proof.DropRefuted},
		},
		Spend: proof.Spend{USD: 0.20, InputTokens: 100_000, OutputTokens: 5_000},
	}
}

func TestEvaluateOutcomeTable(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*proof.Facts)
		kind    proof.Kind
		reason  proof.Reason
		verdict gate.Verdict
	}{
		{"proven", func(*proof.Facts) {}, proof.KindProven, "", gate.VerdictPass},
		{"proven with no candidates", func(f *proof.Facts) { f.Findings = nil }, proof.KindProven, "", gate.VerdictPass},
		{"proven with findings by design not checked", func(f *proof.Facts) {
			f.Findings = append(f.Findings,
				proof.Finding{Class: proof.ClassNotChecked, Reason: proof.ReasonNotProvable},
				proof.Finding{Class: proof.ClassNotChecked, Reason: proof.ReasonTrivial})
		}, proof.KindProven, "", gate.VerdictPass},
		{"a proven finding that does not block", func(f *proof.Facts) {
			f.Findings = []proof.Finding{{Class: proof.ClassProven, Blocking: false}}
		}, proof.KindProven, "", gate.VerdictPass},

		{"blocked by a proven blocking finding", func(f *proof.Facts) {
			f.Findings = append(f.Findings, proof.Finding{Class: proof.ClassProven, Blocking: true})
		}, proof.KindBlocked, "", gate.VerdictFail},
		{"blocked even when no plan test was accepted", func(f *proof.Facts) {
			f.Tests.Accepted = 0
			f.Findings = []proof.Finding{{Class: proof.ClassProven, Blocking: true}}
		}, proof.KindBlocked, "", gate.VerdictFail},
		{"a known failure is not hidden by a budget stop", func(f *proof.Facts) {
			f.Stop = proof.ReasonBudget
			f.Findings = []proof.Finding{{Class: proof.ClassProven, Blocking: true}}
		}, proof.KindBlocked, "", gate.VerdictFail},
		{"a known failure is not hidden by a deadline stop", func(f *proof.Facts) {
			f.Stop = proof.ReasonDeadline
			f.Findings = []proof.Finding{{Class: proof.ClassProven, Blocking: true}}
		}, proof.KindBlocked, "", gate.VerdictFail},
		{"a result for an old head blocks nothing", func(f *proof.Facts) {
			f.Stop = proof.ReasonSuperseded
			f.Findings = []proof.Finding{{Class: proof.ClassProven, Blocking: true}}
		}, proof.KindUnknown, proof.ReasonSuperseded, gate.VerdictUnknown},
		{"a state that failed verification blocks nothing", func(f *proof.Facts) {
			f.Stop = proof.ReasonStateUnverified
			f.Findings = []proof.Finding{{Class: proof.ClassProven, Blocking: true}}
		}, proof.KindUnknown, proof.ReasonStateUnverified, gate.VerdictUnknown},
		{"a changed workflow blocks nothing", func(f *proof.Facts) {
			f.Stop = proof.ReasonWorkflowChanged
			f.Findings = []proof.Finding{{Class: proof.ClassProven, Blocking: true}}
		}, proof.KindUnknown, proof.ReasonWorkflowChanged, gate.VerdictUnknown},

		{"not reproduced: every test passed on base", func(f *proof.Facts) {
			f.Tests = proof.TestFacts{Ran: true, Accepted: 0, Rejected: 3}
		}, proof.KindNotReproduced, "", gate.VerdictUnknown},
		{"not reproduced: no test was written", func(f *proof.Facts) {
			f.Tests = proof.TestFacts{Ran: true}
		}, proof.KindNotReproduced, "", gate.VerdictUnknown},

		{"unknown: monthly cap", func(f *proof.Facts) { f.Stop = proof.ReasonMonthlyCap }, proof.KindUnknown, proof.ReasonMonthlyCap, gate.VerdictUnknown},
		{"unknown: budget stop", func(f *proof.Facts) { f.Stop = proof.ReasonBudget }, proof.KindUnknown, proof.ReasonBudget, gate.VerdictUnknown},
		{"unknown: deadline", func(f *proof.Facts) { f.Stop = proof.ReasonDeadline }, proof.KindUnknown, proof.ReasonDeadline, gate.VerdictUnknown},
		{"unknown: superseded", func(f *proof.Facts) { f.Stop = proof.ReasonSuperseded }, proof.KindUnknown, proof.ReasonSuperseded, gate.VerdictUnknown},
		{"unknown: fork without key", func(f *proof.Facts) { f.Stop = proof.ReasonForkNoKey }, proof.KindUnknown, proof.ReasonForkNoKey, gate.VerdictUnknown},
		{"unknown: runner cannot isolate", func(f *proof.Facts) { f.Stop = proof.ReasonCannotIsolate }, proof.KindUnknown, proof.ReasonCannotIsolate, gate.VerdictUnknown},
		{"unknown: input missing", func(f *proof.Facts) { f.Stop = proof.ReasonInputMissing }, proof.KindUnknown, proof.ReasonInputMissing, gate.VerdictUnknown},
		{"unknown: workflow changed", func(f *proof.Facts) { f.Stop = proof.ReasonWorkflowChanged }, proof.KindUnknown, proof.ReasonWorkflowChanged, gate.VerdictUnknown},
		{"unknown: state failed verification", func(f *proof.Facts) { f.Stop = proof.ReasonStateUnverified }, proof.KindUnknown, proof.ReasonStateUnverified, gate.VerdictUnknown},
		{"unknown: sandbox", func(f *proof.Facts) { f.Stop = proof.ReasonSandbox }, proof.KindUnknown, proof.ReasonSandbox, gate.VerdictUnknown},
		{"unknown: no intent", func(f *proof.Facts) { f.Stop = proof.ReasonNoIntent }, proof.KindUnknown, proof.ReasonNoIntent, gate.VerdictUnknown},
		{"unknown: plan refused", func(f *proof.Facts) { f.Stop = proof.ReasonPlanRefused }, proof.KindUnknown, proof.ReasonPlanRefused, gate.VerdictUnknown},
		{"unknown: model failure", func(f *proof.Facts) { f.Stop = proof.ReasonModelFailure }, proof.KindUnknown, proof.ReasonModelFailure, gate.VerdictUnknown},
		{"unknown: no result", func(f *proof.Facts) { f.Stop = proof.ReasonNoResult }, proof.KindUnknown, proof.ReasonNoResult, gate.VerdictUnknown},
		{"unknown: turn cap", func(f *proof.Facts) { f.Stop = proof.ReasonTurnCap }, proof.KindUnknown, proof.ReasonTurnCap, gate.VerdictUnknown},
		{"unknown: no proposal is not a stop reason", func(f *proof.Facts) { f.Stop = proof.ReasonNoProposal }, proof.KindUnknown, proof.ReasonNoResult, gate.VerdictUnknown},
		{"not reproduced: the review ended with no proposal", func(f *proof.Facts) {
			f.Tests = proof.TestFacts{Ran: true}
			f.Findings = nil
			f.Ended = proof.ReasonNoProposal
		}, proof.KindNotReproduced, "", gate.VerdictUnknown},
		{"unknown: an end that is not a note reason", func(f *proof.Facts) { f.Ended = proof.ReasonTurnCap }, proof.KindUnknown, proof.ReasonNoResult, gate.VerdictUnknown},
		{"unknown: a stop wins over finished tests", func(f *proof.Facts) { f.Stop = proof.ReasonDeadline; f.Tests.Accepted = 5 }, proof.KindUnknown, proof.ReasonDeadline, gate.VerdictUnknown},
		{"unknown: a stop that is not a stop reason", func(f *proof.Facts) { f.Stop = proof.ReasonTrivial }, proof.KindUnknown, proof.ReasonNoResult, gate.VerdictUnknown},
		{"unknown: a stop that no one knows", func(f *proof.Facts) { f.Stop = "gremlins" }, proof.KindUnknown, proof.ReasonNoResult, gate.VerdictUnknown},

		{"unknown: spend over the dollar cap", func(f *proof.Facts) { f.Spend.USD = 0.51 }, proof.KindUnknown, proof.ReasonBudget, gate.VerdictUnknown},
		{"unknown: input tokens over the cap", func(f *proof.Facts) { f.Spend.InputTokens = 800_001 }, proof.KindUnknown, proof.ReasonBudget, gate.VerdictUnknown},
		{"unknown: output tokens over the cap", func(f *proof.Facts) { f.Spend.OutputTokens = 40_001 }, proof.KindUnknown, proof.ReasonBudget, gate.VerdictUnknown},
		{"spend exactly at the cap finished", func(f *proof.Facts) {
			f.Spend = proof.Spend{USD: 0.50, InputTokens: 800_000, OutputTokens: 40_000}
		}, proof.KindProven, "", gate.VerdictPass},
		{"unknown: a negative spend", func(f *proof.Facts) { f.Spend.USD = -1 }, proof.KindUnknown, proof.ReasonBudget, gate.VerdictUnknown},
		{"unknown: a spend that is not a number", func(f *proof.Facts) { f.Spend.USD = math.NaN() }, proof.KindUnknown, proof.ReasonBudget, gate.VerdictUnknown},
		{"unknown: an infinite spend", func(f *proof.Facts) { f.Spend.USD = math.Inf(1) }, proof.KindUnknown, proof.ReasonBudget, gate.VerdictUnknown},
		{"a policy cap is used", func(f *proof.Facts) {
			f.Action = policy.Action{Budget: policy.ActionBudget{PerPRUSD: 0.10}}
		}, proof.KindUnknown, proof.ReasonBudget, gate.VerdictUnknown},
		{"a policy cap above the spend", func(f *proof.Facts) {
			f.Action = policy.Action{Budget: policy.ActionBudget{PerPRUSD: 2}}
			f.Spend.USD = 1.50
		}, proof.KindProven, "", gate.VerdictPass},

		{"unknown: a candidate not reached for budget", func(f *proof.Facts) {
			f.Findings = append(f.Findings, proof.Finding{Class: proof.ClassNotChecked, Reason: proof.ReasonBudget})
		}, proof.KindUnknown, proof.ReasonBudget, gate.VerdictUnknown},
		{"unknown: a candidate not reached for time", func(f *proof.Facts) {
			f.Findings = append(f.Findings, proof.Finding{Class: proof.ClassNotChecked, Reason: proof.ReasonDeadline})
		}, proof.KindUnknown, proof.ReasonDeadline, gate.VerdictUnknown},
		{"unknown: a candidate not checked, fork without key", func(f *proof.Facts) {
			f.Findings = append(f.Findings, proof.Finding{Class: proof.ClassNotChecked, Reason: proof.ReasonForkNoKey})
		}, proof.KindUnknown, proof.ReasonForkNoKey, gate.VerdictUnknown},

		{"unknown: the tests never ran and no one said why", func(f *proof.Facts) { f.Tests = proof.TestFacts{} }, proof.KindUnknown, proof.ReasonNoResult, gate.VerdictUnknown},
		{"unknown: a negative test count", func(f *proof.Facts) { f.Tests.Accepted = -1 }, proof.KindUnknown, proof.ReasonNoResult, gate.VerdictUnknown},
		{"unknown: a malformed finding class", func(f *proof.Facts) { f.Findings = []proof.Finding{{Class: "maybe"}} }, proof.KindUnknown, proof.ReasonNoResult, gate.VerdictUnknown},
		{"unknown: dropped with no drop reason", func(f *proof.Facts) { f.Findings = []proof.Finding{{Class: proof.ClassDropped}} }, proof.KindUnknown, proof.ReasonNoResult, gate.VerdictUnknown},
		{"unknown: not checked with no reason", func(f *proof.Facts) { f.Findings = []proof.Finding{{Class: proof.ClassNotChecked}} }, proof.KindUnknown, proof.ReasonNoResult, gate.VerdictUnknown},
		{"unknown: blocking on a finding that is not proven", func(f *proof.Facts) {
			f.Findings = []proof.Finding{{Class: proof.ClassDropped, Dropped: proof.DropNoProof, Blocking: true}}
		}, proof.KindUnknown, proof.ReasonNoResult, gate.VerdictUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			facts := finished()
			tc.mutate(&facts)
			got := proof.Evaluate(facts)
			require.Equal(t, tc.kind, got.Kind)
			require.Equal(t, tc.reason, got.Reason)
			require.Equal(t, tc.verdict, got.Verdict())
			require.NotEmpty(t, got.Headline())
			if tc.kind == proof.KindUnknown {
				require.NotEmpty(t, got.Reason, "an unknown always names its reason")
			}
		})
	}
}

func TestEvaluateIsNeverAPassWhenNothingIsProven(t *testing.T) {
	for _, kind := range []proof.Kind{proof.KindBlocked, proof.KindNotReproduced, proof.KindUnknown} {
		o := proof.Outcome{Kind: kind, Reason: proof.ReasonBudget}
		require.NotEqual(t, gate.VerdictPass, o.Verdict(), "%s", kind)
	}
}

func TestEvaluateFailsClosedOnAKindNobodyDefined(t *testing.T) {
	require.NotEqual(t, gate.VerdictPass, proof.Outcome{}.Verdict())
	require.NotEqual(t, gate.VerdictPass, proof.Outcome{Kind: "invented"}.Verdict())
}

func TestEvaluateCountsEveryFinding(t *testing.T) {
	facts := finished()
	facts.Findings = []proof.Finding{
		{Class: proof.ClassProven, Blocking: true},
		{Class: proof.ClassProven},
		{Class: proof.ClassProven},
		{Class: proof.ClassDropped, Dropped: proof.DropRefuted},
		{Class: proof.ClassDropped, Dropped: proof.DropRefuted},
		{Class: proof.ClassDropped, Dropped: proof.DropNoProof},
		{Class: proof.ClassDropped, Dropped: proof.DropOutdated},
		{Class: proof.ClassDropped, Dropped: proof.DropNoLocation},
		{Class: proof.ClassNotChecked, Reason: proof.ReasonNotProvable},
		{Class: proof.ClassNotChecked, Reason: proof.ReasonBudget},
	}
	got := proof.Evaluate(facts).Counts
	require.Equal(t, proof.Counts{
		In: 10, Proven: 3, Blocking: 1, NeedsPerson: 2,
		Refuted: 2, NoProof: 1, Outdated: 1, NoLocation: 1,
		NotChecked: 2,
	}, got)
	require.Equal(t, 5, got.Dropped())
}

func TestEvaluateReportsCost(t *testing.T) {
	facts := finished()
	facts.Spend = proof.Spend{USD: 0.2432, InputTokens: 1, OutputTokens: 2}
	got := proof.Evaluate(facts)
	require.InDelta(t, 0.2432, got.Cost.USD, 1e-9)
	require.Equal(t, 1, got.Cost.InputTokens)
	require.Equal(t, 2, got.Cost.OutputTokens)
}

func TestDocsOnlyCostsNothing(t *testing.T) {
	d := proof.Trivial([]proof.File{f("README.md", 5, 2), f("docs/a.md", 1, 0)}, policy.Action{})
	require.True(t, d.Skip)
	// The caller must not have called a model. Even a spend that was filled in by mistake is not reported.
	for _, spend := range []proof.Spend{{}, {USD: 0.40, InputTokens: 9, OutputTokens: 9}} {
		got := proof.Evaluate(proof.Facts{Trivial: d, Spend: spend})
		require.Equal(t, proof.KindSkippedTrivial, got.Kind)
		require.Equal(t, proof.Spend{}, got.Cost, "a docs-only change costs $0")
		require.Equal(t, gate.VerdictPass, got.Verdict())
		require.Equal(t, "skipped: trivial (rule)", got.Headline())
	}
}

func TestTrivialSkipIsOnlyHonouredWhenTheDecisionSaysSkip(t *testing.T) {
	d := proof.Trivial([]proof.File{f("internal/pay/refund.go", 5, 1)}, policy.Action{})
	require.False(t, d.Skip)
	got := proof.Evaluate(proof.Facts{Trivial: d})
	require.Equal(t, proof.KindUnknown, got.Kind, "a run that did nothing is not a skip")
	require.Equal(t, proof.ReasonNoResult, got.Reason)
}

func TestMonthlyStop(t *testing.T) {
	a := policy.Action{Budget: policy.ActionBudget{PerPRUSD: 0.50, MonthlyUSD: 10}}
	cases := []struct {
		name  string
		spent float64
		want  proof.Reason
	}{
		{"under the cap", 5, ""},
		{"spend plus one run is exactly the cap", 9.50, ""},
		{"spend plus one run is over the cap", 9.51, proof.ReasonMonthlyCap},
		{"already over the cap", 12, proof.ReasonMonthlyCap},
		{"a negative total is not believed", -1, proof.ReasonMonthlyCap},
		{"a total that is not a number is not believed", math.NaN(), proof.ReasonMonthlyCap},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, proof.MonthlyStop(a, tc.spent))
		})
	}
	t.Run("the defaults apply", func(t *testing.T) {
		require.Equal(t, proof.Reason(""), proof.MonthlyStop(policy.Action{}, 49.50))
		require.Equal(t, proof.ReasonMonthlyCap, proof.MonthlyStop(policy.Action{}, 49.51))
	})
}

func TestSpendExceeds(t *testing.T) {
	b := policy.Action{}.Effective().Budget
	require.False(t, proof.Spend{USD: 0.50}.Exceeds(b))
	require.True(t, proof.Spend{USD: 0.5000001}.Exceeds(b))
	require.True(t, proof.Spend{InputTokens: -1}.Exceeds(b))
	require.True(t, proof.Spend{OutputTokens: 40_001}.Exceeds(b))
}

// TestANoProposalEndIsNotedOnlyOnANotReproducedRun: the stall rule's end says why a run proved nothing, but it never
// turns a run into a pass, and it is dropped when the run proved something before it stalled.
func TestANoProposalEndIsNotedOnlyOnANotReproducedRun(t *testing.T) {
	f := finished()
	f.Tests = proof.TestFacts{Ran: true}
	f.Findings = nil
	f.Ended = proof.ReasonNoProposal
	got := proof.Evaluate(f)
	require.Equal(t, proof.KindNotReproduced, got.Kind)
	require.Equal(t, proof.ReasonNoProposal, got.Note)
	require.Empty(t, got.Reason, "only an unknown run has a reason")
	require.NotEqual(t, gate.VerdictPass, got.Verdict())

	proven := finished()
	proven.Ended = proof.ReasonNoProposal
	got = proof.Evaluate(proven)
	require.Equal(t, proof.KindProven, got.Kind)
	require.Empty(t, got.Note)

	stopped := finished()
	stopped.Ended = proof.ReasonNoProposal
	stopped.Stop = proof.ReasonTurnCap
	got = proof.Evaluate(stopped)
	require.Equal(t, proof.KindUnknown, got.Kind)
	require.Equal(t, proof.ReasonTurnCap, got.Reason)
	require.Empty(t, got.Note)
}
