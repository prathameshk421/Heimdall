package actionflow

import (
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

// fastCurrent is the fast result for the current head, or nil. A result for another head says nothing about this one:
// a new head invalidates the fast result of the old head. A verdict nobody defined is no result.
func (f facts) fastCurrent() *change.FastInfo {
	fast := f.a.Fast
	if fast == nil || fast.Head != f.head || !fast.Verdict.Valid() {
		return nil
	}
	return fast
}

func (f facts) fastRow() Row {
	run := RunFast{Head: f.head}
	fast := f.a.Fast
	switch {
	case fast == nil:
		return row(RowFast, StateNeeded, "no fast check result yet"+f.resetNote(), run)
	case fast.Head != f.head:
		return row(RowFast, StateNeeded, fmt.Sprintf("the fast result is for old head %s and the head is %s", fast.Head, f.head), run)
	case f.fastCurrent() == nil:
		return row(RowFast, StateNeeded, fmt.Sprintf("the fast result has no usable verdict (%q)", fast.Verdict), run)
	}
	return row(RowFast, StateDone, fmt.Sprintf("fast check %s on head %s", fast.Verdict, f.head))
}

// ingestRow turns the third-party candidates into log records. A candidate is pending when the log has no record of
// its comment for this head with the same content, so an edited comment is recorded again and the record replaces
// the old one. A candidate for another head, or one the log would refuse, is ignored and never loops.
func (f facts) ingestRow() Row {
	var pending []change.ExternalIngested
	recorded, ignored := 0, 0
	for _, c := range f.in.Candidates {
		switch rec, ok := f.a.External[c.CommentID]; {
		case c.Head != f.head || c.Validate() != nil:
			ignored++
		case ok && rec == change.ExternalInfo(c):
			recorded++
		default:
			pending = append(pending, c)
		}
	}
	note := ""
	if ignored > 0 {
		note = fmt.Sprintf("; %d ignored (another head, or not recordable)", ignored)
	}
	switch {
	case len(pending) > 0:
		return row(RowIngest, StateNeeded, fmt.Sprintf("%d third-party comment(s) to ingest%s", len(pending), note),
			IngestExternal{Head: f.head, Candidates: pending})
	case recorded > 0:
		return row(RowIngest, StateDone, fmt.Sprintf("%d third-party comment(s) recorded%s", recorded, note))
	case ignored > 0:
		return row(RowIngest, StateDone, "no third-party candidate to record"+note)
	}
	return row(RowIngest, StateDone, "no third-party candidates")
}
