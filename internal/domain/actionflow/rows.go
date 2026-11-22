package actionflow

import (
	"fmt"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
)

// RowID names one row of the table. The ids are stable: the summary, the log and a test cite a row by its id.
type RowID string

// The rows, in table order (ACTION_MODE 8, AM13). The design names fast, proof, publish and sweep. The rest are the
// facts those rows read, each with its own state so that "what is missing" can name it.
const (
	RowState      RowID = "state"       // the log is trusted, or it was reset
	RowFast       RowID = "fast"        // heimdall/fast for the head
	RowIngest     RowID = "ingest"      // third-party comments become candidates
	RowProof      RowID = "proof"       // heimdall/proof for the head: skip, wait, or a result
	RowFindings   RowID = "findings"    // candidates and findings, proven or not, and which block
	RowBudget     RowID = "budget"      // the per-PR budget and the monthly cap
	RowBreakGlass RowID = "break_glass" // a bypass of the fast check, open until reviewed
	RowSweep      RowID = "sweep"       // a proof run that never reported
	RowPublish    RowID = "publish"     // the summary comment and the two checks
)

// RowIDs lists the rows in table order. Decide returns one Row for each, in this order.
func RowIDs() []RowID {
	return []RowID{RowState, RowFast, RowIngest, RowProof, RowFindings, RowBudget, RowBreakGlass, RowSweep, RowPublish}
}

// State is where a row stands for the current head.
type State string

// The five states.
const (
	// StateDone: the work is finished and the fact is known for this head.
	StateDone State = "done"
	// StateNeeded: the row has work for the gate now. Effects holds it.
	StateNeeded State = "needed"
	// StateWaiting: something outside the gate must happen first: a workflow run, a push, a person.
	StateWaiting State = "waiting"
	// StateBlocked: the row stopped on purpose (a budget, an unusable policy) or holds a finding that blocks.
	StateBlocked State = "blocked"
	// StateUnknown: a fact is not known and the gate cannot learn it now. It never reads as done.
	StateUnknown State = "unknown"
)

// Row is the decision of one row: its state and why, and the effects to request. Reason is never empty. Why is the
// stable word of proof.Reason when the state is unknown or blocked for a reason that package names.
type Row struct {
	ID      RowID
	State   State
	Reason  string
	Why     proof.Reason
	Effects []Effect
}

// SweepAfter is how long a head may wait for its proof result before the sweep records unknown: no result
// (ACTION_MODE 5.4).
const SweepAfter = 20 * time.Minute

// In is everything the rows read. The use case builds it; rows never do I/O. The view is read, never changed.
type In struct {
	// View is the fold of the change's log. Its PR holds the head and base the gate read from GitHub.
	View change.View
	// Action is the action area of the policy at the merge base. The rows fill its defaults themselves.
	Action policy.Action
	// PolicyProblem says why the policy at the merge base cannot be used. While it is set, every row is blocked.
	PolicyProblem string
	// StateProblem says why the state log failed verification: a bad MAC or a broken chain (ACTION_MODE 2.7). While it
	// is set, the only work is to reset the log, and every other row is unknown.
	StateProblem string
	// PolicyHash is the sha256 of the policy at the merge base. A proof result for another hash is not current.
	PolicyHash string
	// Files are the files the PR changes, with their counted lines, for the trivial rule.
	Files []proof.File
	// Candidates are the third-party comments, already turned into candidates for the current head by package
	// external. A comment dropped as outdated or with no location is not a candidate: the log names only counts.
	Candidates []change.ExternalIngested
	// MonthKnown is false when the month's spend could not be read from the state ref. MonthSpentUSD is that spend.
	MonthKnown    bool
	MonthSpentUSD float64
	// ProofWorkflowChanged says the PR changes the proof workflow at the head. PolicyOwnerApproved says a policy owner
	// approved that on the head. A proof from a workflow the PR edited is not believed (ACTION_MODE 2.7).
	ProofWorkflowChanged bool
	PolicyOwnerApproved  bool
	// SummaryVersion is the version the summary must carry now. The use case counts the log events the summary reads and
	// leaves out summary.posted, so writing the summary does not make it stale.
	SummaryVersion int
	// HeadAt is when the current head was pushed. Now is the time of this run. The rows read no clock.
	HeadAt time.Time
	Now    time.Time
}

// Need is one thing the change still waits for, as Missing reports it.
type Need struct {
	Row   RowID
	State State
	Text  string
}

// Decide returns the row of every id in table order, for the current head. It is a pure function of In: the same In
// gives the same rows. It fails closed. An unreadable policy, a log that failed verification, a missing PR and an
// ended change each give every row the same state and no effect, except that a failed log asks for its reset.
func Decide(in In) []Row {
	f := newFacts(in)
	if rows, halted := f.halt(); halted {
		return rows
	}
	proofRow := f.proofRow()
	return []Row{f.stateRow(), f.fastRow(), f.ingestRow(), proofRow, f.findingsRow(), f.budgetRow(), f.breakGlassRow(),
		f.sweepRow(proofRow), f.publishRow()}
}

// Effects are the effects of every row, in table order.
func Effects(rows []Row) []Effect {
	var out []Effect
	for _, r := range rows {
		out = append(out, r.Effects...)
	}
	return out
}

// Missing lists every row that is not done, in table order, with its reason in words.
func Missing(rows []Row) []Need {
	var out []Need
	for _, r := range rows {
		if r.State != StateDone {
			out = append(out, Need{Row: r.ID, State: r.State, Text: r.Reason})
		}
	}
	return out
}

// facts is what the rows share: the input, the effective policy and the values every row needs.
type facts struct {
	in      In
	act     policy.Action // with defaults filled
	a       *change.ActionInfo
	head    id.SHA
	base    id.SHA
	trivial proof.Decision
}

func newFacts(in In) facts {
	f := facts{in: in, act: in.Action.Effective(), a: in.View.Action, trivial: proof.Trivial(in.Files, in.Action)}
	if f.a == nil {
		f.a = &change.ActionInfo{} // no Action event yet: read as empty, never changed
	}
	if pr := in.View.PR; pr != nil {
		f.head, f.base = pr.HeadSHA, pr.Base
	}
	return f
}

// halt returns the rows of a change that no row can decide on.
func (f facts) halt() ([]Row, bool) {
	switch {
	case f.in.StateProblem != "":
		return f.stateUntrusted(), true
	case f.in.PolicyProblem != "":
		return uniform(StateBlocked, "the policy at the merge base cannot be used: "+f.in.PolicyProblem), true
	case f.in.View.PR == nil:
		return uniform(StateWaiting, "no pull request yet"), true
	case f.in.View.Over():
		return uniform(StateDone, "the change is over: no row has work"), true
	}
	return nil, false
}

// stateUntrusted is the table for a log that failed verification: reset it, and trust no produced fact meanwhile.
func (f facts) stateUntrusted() []Row {
	rows := uniform(StateUnknown, "state failed verification: no fact Heimdall made can be trusted until the log is reset")
	for i := range rows {
		rows[i].Why = proof.ReasonStateUnverified
		if rows[i].ID == RowState {
			rows[i] = row(RowState, StateNeeded, "state failed verification: "+f.in.StateProblem, ResetState{Reason: f.in.StateProblem})
		}
	}
	return rows
}

func uniform(st State, reason string) []Row {
	var rows []Row
	for _, id := range RowIDs() {
		rows = append(rows, row(id, st, reason))
	}
	return rows
}

func row(id RowID, st State, reason string, effects ...Effect) Row {
	return Row{ID: id, State: st, Reason: reason, Effects: effects}
}

func (r Row) because(why proof.Reason) Row {
	r.Why = why
	return r
}

func (f facts) stateRow() Row {
	r := f.a.Reset
	if r == nil {
		return row(RowState, StateDone, "the log verified and was never reset")
	}
	return row(RowState, StateDone, fmt.Sprintf("state was reset %d time(s), last: %s; facts made before it are unknown and are made again",
		r.Count, r.Reason))
}

// resetNote is a clause that says why a produced fact is missing when the log was reset, and "" otherwise.
func (f facts) resetNote() string {
	if f.a.Reset == nil {
		return ""
	}
	return "; the state was reset (" + f.a.Reset.Reason + ")"
}
