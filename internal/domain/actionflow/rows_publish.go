package actionflow

import (
	"fmt"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/fastcheck"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/summary"
)

// publishRow posts the summary comment and the two checks. It waits for the fast result of the head. It is done when
// the summary carries the version and head it must; any other summary is stale and is written again in place. The
// checks go out with the summary, so a new result updates both. Every write is safe to repeat (ACTION_MODE 2.5).
func (f facts) publishRow() Row {
	fast := f.fastCurrent()
	if fast == nil {
		return row(RowPublish, StateWaiting, "the fast check has no result for the head yet")
	}
	stale := f.summaryStale()
	if stale == "" {
		return row(RowPublish, StateDone, fmt.Sprintf("summary is current: version %d on head %s", f.in.SummaryVersion, f.head))
	}
	var comment int64
	if s := f.a.Summary; s != nil {
		comment = s.CommentID
	}
	return row(RowPublish, StateNeeded, stale,
		PostSummary{Head: f.head, Version: f.in.SummaryVersion, CommentID: comment}, f.checks(*fast))
}

// summaryStale says why the summary must be written, or "" when it is current. A marker that cannot be read, and a
// version that cannot be true, are stale: the gate writes again.
func (f facts) summaryStale() string {
	s := f.a.Summary
	if s == nil {
		return "no summary comment yet"
	}
	m, ok := summary.ParseMarker(s.Marker)
	switch {
	case !ok:
		return "the summary marker cannot be read"
	case m.Head != string(f.head):
		return fmt.Sprintf("the summary is for head %s and the head is %s", m.Head, f.head)
	case f.in.SummaryVersion < 0 || m.Version != f.in.SummaryVersion:
		return fmt.Sprintf("the summary is at version %d and the log is at version %d", m.Version, f.in.SummaryVersion)
	}
	return ""
}

func (f facts) checks(fast change.FastInfo) PostChecks {
	pc := PostChecks{Head: f.head, Fast: f.fastCheck(fast)}
	if p := f.usableProof(); p != nil {
		o := outcomeOf(*p)
		pc.Proof = &Check{Conclusion: o.Conclusion(f.in.Action), Headline: o.Headline()}
	}
	return pc
}

// fastCheck is the heimdall/fast check (ACTION_MODE 5.4). A bypass that the policy allows turns an enforce-mode
// failure or action_required into success with its reason. In shadow mode nothing blocks, so the conclusion stays
// neutral and the headline only says the bypass exists.
func (f facts) fastCheck(fast change.FastInfo) Check {
	c := Check{Conclusion: fastConclusion(f.in.Action, fast.Verdict), Headline: fastHeadline(f.in.Action, fast)}
	b := f.validBypass()
	switch {
	case b == nil:
	case c.Conclusion == proof.ConclusionNeutral:
		c.Headline += fmt.Sprintf(" (bypass by @%s recorded)", b.Login)
	case c.Conclusion != proof.ConclusionSuccess:
		c = Check{Conclusion: proof.ConclusionSuccess, Headline: fmt.Sprintf("bypassed by @%s: %s", b.Login, b.Reason)}
	}
	return c
}

// fastConclusion maps the verdict to the conclusion. Shadow is neutral. Enforce maps pass, fail and unknown to
// success, failure and action_required, which blocks a merge. A mode nobody defined is read as enforce, so a typo does
// not switch the gate off, and a verdict nobody defined is a failure.
func fastConclusion(a policy.Action, v gate.Verdict) proof.Conclusion {
	if a.Mode == "" || a.Mode == policy.ActionModeShadow {
		return proof.ConclusionNeutral
	}
	switch v {
	case gate.VerdictPass:
		return proof.ConclusionSuccess
	case gate.VerdictUnknown:
		return proof.ConclusionActionRequired
	case gate.VerdictFail:
		return proof.ConclusionFailure
	}
	return proof.ConclusionFailure
}

// fastHeadline is the first line of the fast check: "would block because ..." in shadow mode (ACTION_MODE 5.4).
func fastHeadline(a policy.Action, fast change.FastInfo) string {
	shadow := a.Mode == "" || a.Mode == policy.ActionModeShadow
	switch fast.Verdict {
	case gate.VerdictPass:
		return pick(shadow, "would pass", "passed")
	case gate.VerdictFail:
		return pick(shadow, "would block because ", "failed because ") + clauseList(fast.Clauses, fastcheck.StatusUnmet)
	case gate.VerdictUnknown:
		return pick(shadow, "would block: unknown ", "unknown: ") + clauseList(fast.Clauses, fastcheck.StatusUnknown)
	}
	return "no usable verdict"
}

func pick(first bool, a, b string) string {
	if first {
		return a
	}
	return b
}

// clauseList names the clauses with the status, in the order the check lists them.
func clauseList(cs []change.FastClause, st fastcheck.Status) string {
	var ids []string
	for _, c := range cs {
		if c.Status == st {
			ids = append(ids, string(c.ID))
		}
	}
	return strings.Join(ids, ", ")
}
