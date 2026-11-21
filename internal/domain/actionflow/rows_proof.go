package actionflow

import (
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
)

// proofOnHead is the last proof run if it is about the current head, started or finished, and nil otherwise. A run
// of an old head is superseded: its result is never read for this head.
func (f facts) proofOnHead() *change.ProofInfo {
	if p := f.a.Proof; p != nil && p.Head == f.head {
		return p
	}
	return nil
}

// sameInputs reports whether a run ran on the current head, base and policy (ACTION_MODE 2.5: the same input runs
// once, a new input runs again).
func (f facts) sameInputs(p change.ProofInfo) bool {
	return p.Head == f.head && p.Base == f.base && p.PolicyHash == f.in.PolicyHash
}

// usableProof is a finished run on the current inputs, or nil.
func (f facts) usableProof() *change.ProofInfo {
	if p := f.a.Proof; p != nil && p.Done && f.sameInputs(*p) {
		return p
	}
	return nil
}

func outcomeOf(p change.ProofInfo) proof.Outcome {
	return proof.Outcome{Kind: p.Kind, Reason: p.Reason, Counts: p.Counts.Counts(), Cost: p.Cost.Spend()}
}

// proofRow decides the proof run: the trivial skip, the wait for the workflow GitHub starts, the result, or an
// unknown that the gate records itself. The gate cannot start the proof workflow, so a missing run is a wait.
func (f facts) proofRow() Row {
	switch {
	case f.trivial.Skip:
		return f.trivialProofRow()
	case f.in.ProofWorkflowChanged && !f.in.PolicyOwnerApproved:
		return f.ensureUnknown(RowProof, proof.ReasonWorkflowChanged,
			"the PR changes the proof workflow: its result is unknown until a policy owner approves")
	}
	p := f.proofOnHead()
	if p == nil {
		return f.noProofOnHead()
	}
	switch {
	case p.Base != f.base:
		return row(RowProof, StateWaiting, fmt.Sprintf("proof ran on base %s and the base is %s: waiting for a run on the current inputs", p.Base, f.base))
	case p.PolicyHash != f.in.PolicyHash:
		return row(RowProof, StateWaiting, "proof ran under another policy hash: waiting for a run on the current inputs")
	case !p.Done:
		return row(RowProof, StateWaiting, fmt.Sprintf("proof run %d in flight on head %s", p.WorkflowRun, f.head))
	}
	return f.finishedProofRow(*p)
}

// trivialProofRow is the proof row of a change the rule skips. It records the skip itself at $0, unless a run of the
// proof workflow is already doing it.
func (f facts) trivialProofRow() Row {
	p := f.proofOnHead()
	switch {
	case p != nil && p.Done && f.sameInputs(*p):
		return f.finishedProofRow(*p)
	case p != nil && !p.Done && f.sameInputs(*p):
		return row(RowProof, StateWaiting, fmt.Sprintf("proof run %d in flight on head %s; a trivial change reports its own skip", p.WorkflowRun, f.head))
	}
	return row(RowProof, StateNeeded, "trivial change: record the skip at $0 ("+f.trivial.Detail+")",
		SkipProof{Head: f.head, Base: f.base, PolicyHash: f.in.PolicyHash, Detail: f.trivial.Detail})
}

// noProofOnHead is the proof row before any run exists for the head.
func (f facts) noProofOnHead() Row {
	if f.monthlyStop() != "" {
		return f.ensureUnknown(RowProof, proof.ReasonMonthlyCap, "monthly cap reached: no run starts, the result is unknown")
	}
	if old := f.a.Proof; old != nil {
		return row(RowProof, StateWaiting, fmt.Sprintf("proof run %d is for old head %s; superseded by %s: its result is ignored",
			old.WorkflowRun, old.Head, f.head)).because(proof.ReasonSuperseded)
	}
	return row(RowProof, StateWaiting, fmt.Sprintf("the proof run for head %s has not started%s", f.head, f.resetNote()))
}

// finishedProofRow maps a finished run on the current inputs to a state. A known result (proven, blocked) is done. A
// result that is not a pass or a fail is unknown, with the reason that stopped it. A kind nobody defined is unknown:
// no result. A skip that the rule no longer allows is not believed.
func (f facts) finishedProofRow(p change.ProofInfo) Row {
	o := outcomeOf(p)
	switch o.Kind {
	case proof.KindProven, proof.KindBlocked:
		return row(RowProof, StateDone, o.Headline())
	case proof.KindSkippedTrivial:
		if f.trivial.Skip {
			return row(RowProof, StateDone, o.Headline())
		}
		return row(RowProof, StateUnknown, "recorded as trivial, but the rule says this change needs proof").because(proof.ReasonNoResult)
	case proof.KindNotReproduced:
		return row(RowProof, StateUnknown, o.Headline())
	case proof.KindUnknown:
		if why := o.Reason; why.CanStop() {
			return row(RowProof, StateUnknown, o.Headline()).because(why)
		}
	}
	return row(RowProof, StateUnknown, "unknown: "+proof.ReasonNoResult.Text()).because(proof.ReasonNoResult)
}

// ensureUnknown is a row that makes the log say "proof is unknown for Reason" on the current inputs. It asks for the
// record until the log holds it, so a proven result from a run the gate does not believe is overwritten.
func (f facts) ensureUnknown(id RowID, why proof.Reason, text string) Row {
	if p := f.a.Proof; p != nil && f.sameInputs(*p) && p.Done && p.Kind == proof.KindUnknown && p.Reason == why {
		return row(id, StateUnknown, text).because(why)
	}
	var run int64
	if p := f.proofOnHead(); p != nil && !p.Done {
		run = p.WorkflowRun // the run being closed
	}
	return row(id, StateNeeded, text,
		ReportProofUnknown{Head: f.head, Base: f.base, PolicyHash: f.in.PolicyHash, Reason: why, WorkflowRun: run})
}

// sweepRow closes a proof run that has not reported SweepAfter after its head. The sweep is a backstop for a lost
// artifact, a cancelled run and a result for other inputs. It defers to the proof row when that row records its own
// result, so the two never ask for different records.
func (f facts) sweepRow(proofRow Row) Row {
	switch {
	case f.trivial.Skip:
		return row(RowSweep, StateDone, "not needed: a trivial change has no proof run to wait for")
	case f.usableProof() != nil:
		return row(RowSweep, StateDone, "the proof run reported")
	case proofRow.State == StateNeeded:
		return row(RowSweep, StateWaiting, "the proof row records its own result")
	case f.in.HeadAt.IsZero() || f.in.Now.IsZero():
		return row(RowSweep, StateWaiting, "no clock reading: the sweep cannot tell how long the proof has waited")
	}
	age := f.in.Now.Sub(f.in.HeadAt)
	if age > SweepAfter {
		return f.ensureUnknown(RowSweep, proof.ReasonNoResult, fmt.Sprintf("proof has not reported %s after its head", SweepAfter))
	}
	return row(RowSweep, StateWaiting, fmt.Sprintf("proof has %s left to report", SweepAfter-age))
}

// monthlyStop is the monthly cap check before a run starts, or "" when the month's spend is not known: the proof job
// reads the budget itself, and the budget row says the spend is unknown.
func (f facts) monthlyStop() proof.Reason {
	if !f.in.MonthKnown {
		return ""
	}
	return proof.MonthlyStop(f.in.Action, f.in.MonthSpentUSD)
}

// budgetRow reports the cap that applies: a trivial change costs $0, a run is held to the per-PR budget, and a run
// that has not started is held to the monthly cap.
func (f facts) budgetRow() Row {
	if f.trivial.Skip {
		return row(RowBudget, StateDone, "no model call: $0")
	}
	if p := f.proofOnHead(); p != nil {
		return f.runSpendRow(*p)
	}
	return f.monthlyRow()
}

func (f facts) runSpendRow(p change.ProofInfo) Row {
	spend, b := f.runSpend(p), f.act.Budget
	text := fmt.Sprintf("$%.2f of $%.2f, %d of %d input tokens, %d of %d output tokens",
		spend.USD, b.PerPRUSD, spend.InputTokens, b.PerPRTokens.Input, spend.OutputTokens, b.PerPRTokens.Output)
	if spend.Exceeds(b) {
		return row(RowBudget, StateBlocked, "per-PR budget hit: "+text).because(proof.ReasonBudget)
	}
	return row(RowBudget, StateDone, "spent "+text+" of the per-PR budget")
}

// runSpend is what the run spent: its own report when it finished, else the cost records of its workflow run.
func (f facts) runSpend(p change.ProofInfo) proof.Spend {
	if p.Done {
		return p.Cost.Spend()
	}
	var c change.Cost
	for _, r := range f.a.Costs {
		if r.WorkflowRun == p.WorkflowRun {
			c.USDMicros += r.USDMicros
			c.PromptTokens += r.PromptTokens
			c.CompletionTokens += r.CompletionTokens
		}
	}
	return c.Spend()
}

func (f facts) monthlyRow() Row {
	if !f.in.MonthKnown {
		return row(RowBudget, StateUnknown, "the monthly spend is not known: the budget file could not be read").because(proof.ReasonInputMissing)
	}
	b := f.act.Budget
	if f.monthlyStop() != "" {
		return row(RowBudget, StateBlocked, fmt.Sprintf("monthly cap reached: $%.2f spent plus $%.2f for one PR is over $%.2f",
			f.in.MonthSpentUSD, b.PerPRUSD, b.MonthlyUSD)).because(proof.ReasonMonthlyCap)
	}
	return row(RowBudget, StateDone, fmt.Sprintf("within the monthly cap: $%.2f of $%.2f spent", f.in.MonthSpentUSD, b.MonthlyUSD))
}
