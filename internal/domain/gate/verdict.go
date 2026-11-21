package gate

// Verdict is the three-state outcome of a decision. A clause is unmet either because a fact is false (fail) or because
// a fact is not known (unknown). The merge rule treats both as "not met", which is right for the merge but loses the
// reason. Action mode shows the reason: a fail names what is wrong, an unknown names what is missing (ACTION_MODE 1.2).
type Verdict string

// The three verdicts. Unknown never means "probably fine": no mode maps it to success.
const (
	VerdictPass    Verdict = "pass"
	VerdictFail    Verdict = "fail"
	VerdictUnknown Verdict = "unknown"
)

// String returns the verdict as it is written in a check summary.
func (v Verdict) String() string { return string(v) }

// Valid reports whether v is one of the three verdicts.
func (v Verdict) Valid() bool {
	switch v {
	case VerdictPass, VerdictFail, VerdictUnknown:
		return true
	}
	return false
}

// VerdictOf derives the verdict of a decision from its unmet clauses. A clause known to be false makes the verdict
// fail, even when other clauses are unknown: a known failure is never hidden by a missing fact. Only when every unmet
// clause is unknown is the verdict unknown. Nothing unmet passes, but only when the decision says OK: a decision that
// is not OK and names no clause cannot pass, because silence never helps.
func VerdictOf(d Decision) Verdict {
	if len(d.Unmet) == 0 {
		if d.OK {
			return VerdictPass
		}
		return VerdictFail
	}
	for _, c := range d.Unmet {
		if !c.Unknown {
			return VerdictFail
		}
	}
	return VerdictUnknown
}

// ApprovalReason applies the approval clause alone: an approval on head, not from the author, by an approver of the
// changed domains (TECH_SPEC 3.4). It returns "" when the clause holds, or the reason it does not. The fast check of
// Action mode reuses this clause instead of copying it.
func ApprovalReason(f Facts) string { return checkApproval(f) }
