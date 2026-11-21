package actionflow

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
)

// tally counts the live findings by what the last proof run said about each on the current head. A result for another
// head counts as no result: a new head invalidates the proof of the old one.
type tally struct {
	live        int
	awaiting    int
	blocking    []string // refs in number order
	needsPerson int
	dropped     int
	notChecked  int
}

func (f facts) tallyFindings() tally {
	var t tally
	refs := make([]string, 0, len(f.in.View.Findings))
	for ref := range f.in.View.Findings {
		refs = append(refs, ref)
	}
	slices.SortFunc(refs, compareRef)
	for _, ref := range refs {
		fi := f.in.View.Findings[ref]
		if fi.State.Terminal() {
			continue
		}
		t.live++
		t.add(ref, fi.Proof, f.head == "" || fi.Proof == nil || fi.Proof.Head != f.head)
	}
	return t
}

// add counts one finding. A class nobody defined counts as no result, never as a result.
func (t *tally) add(ref string, p *change.FindingProofInfo, stale bool) {
	switch {
	case stale:
		t.awaiting++
	case p.Class == proof.ClassProven && p.Blocking:
		t.blocking = append(t.blocking, ref)
	case p.Class == proof.ClassProven:
		t.needsPerson++
	case p.Class == proof.ClassDropped:
		t.dropped++
	case p.Class == proof.ClassNotChecked:
		t.notChecked++
	default:
		t.awaiting++
	}
}

// compareRef orders F2 before F10: by number when both parse, else as text.
func compareRef(a, b string) int {
	na, ea := strconv.Atoi(strings.TrimPrefix(a, "F"))
	nb, eb := strconv.Atoi(strings.TrimPrefix(b, "F"))
	if ea != nil || eb != nil {
		return strings.Compare(a, b)
	}
	return na - nb
}

// findingsRow reports what proof did to the findings. A proven finding that blocks makes the row blocked in every
// mode, as a known failure is never hidden by a missing fact. What the mode does with it is in the reason and in the
// checks the publish row posts.
func (f facts) findingsRow() Row {
	t := f.tallyFindings()
	switch {
	case t.live == 0:
		return row(RowFindings, StateDone, "no findings to prove")
	case len(t.blocking) > 0:
		return row(RowFindings, StateBlocked, fmt.Sprintf("%d proven blocking finding(s) %s: %s",
			len(t.blocking), strings.Join(t.blocking, ", "), f.blockNote()))
	case t.awaiting > 0:
		return f.awaitingRow(t.awaiting)
	}
	return row(RowFindings, StateDone, fmt.Sprintf("every finding has a result on this head: %d proven, %d dropped, %d not checked; %d need a person",
		t.needsPerson, t.dropped, t.notChecked, t.needsPerson))
}

// awaitingRow is the findings row when some have no result for this head. They wait for the proof run. When the run
// ended without a result for them, they are unknown and never block.
func (f facts) awaitingRow(n int) Row {
	p := f.usableProof()
	switch {
	case f.trivial.Skip:
		return row(RowFindings, StateDone, fmt.Sprintf("%d finding(s) not checked: trivial PR", n))
	case p == nil:
		return row(RowFindings, StateWaiting, fmt.Sprintf("%d finding(s) not yet proven on this head: the proof run has not reported%s", n, f.resetNote()))
	case p.Kind == proof.KindUnknown && p.Reason.CanStop():
		return row(RowFindings, StateUnknown, fmt.Sprintf("%d finding(s) not checked: %s", n, p.Reason.Text())).because(p.Reason)
	}
	return row(RowFindings, StateUnknown, fmt.Sprintf("%d finding(s) arrived after the proof run for this head: not proven, and an unproven finding never blocks", n))
}

// blockNote says what a proven blocking finding does in the current mode (ACTION_MODE 5.4). A mode nobody defined is
// read as enforce with the proof required, as package proof reads it.
func (f facts) blockNote() string {
	const holds = "it fails the proof check and holds the merge"
	switch f.in.Action.Mode {
	case "", policy.ActionModeShadow:
		return "would block in enforce mode; shadow mode blocks nothing"
	case policy.ActionModeEnforce:
		if !f.in.Action.ProofRequired {
			return "shown on the advisory proof check; the merge is not held"
		}
	}
	return holds
}

// validBypass is the first bypass of the current head by a person the policy at the merge base allows, or nil. The
// gate checks the person before it records a bypass; the rows check again, because the log is not the policy.
func (f facts) validBypass() *change.BreakGlassInfo {
	for i, b := range f.a.BreakGlass {
		if b.Head == f.head && f.mayBypass(string(b.Login)) {
			return &f.a.BreakGlass[i]
		}
	}
	return nil
}

func (f facts) mayBypass(login string) bool {
	return slices.ContainsFunc(f.act.BreakGlass, func(allowed string) bool { return strings.EqualFold(allowed, login) })
}

// breakGlassRow says whether a bypass of the fast check is open. It is open until another person reviews it
// (ACTION_MODE 5.4). A bypass of an old head, or by a login the policy does not name, changes nothing.
func (f facts) breakGlassRow() Row {
	if len(f.a.BreakGlass) == 0 {
		return row(RowBreakGlass, StateDone, "no break-glass recorded")
	}
	b := f.validBypass()
	switch {
	case b == nil:
		return row(RowBreakGlass, StateDone, fmt.Sprintf("%d break-glass record(s) ignored: not for this head, or the login is not in action.break_glass at the merge base",
			len(f.a.BreakGlass)))
	case b.Open():
		return row(RowBreakGlass, StateWaiting, fmt.Sprintf("bypass by @%s is open: another approver must post `/heimdall break-glass-reviewed`", b.Login))
	}
	return row(RowBreakGlass, StateDone, fmt.Sprintf("bypass by @%s reviewed by @%s", b.Login, b.ReviewedBy))
}
