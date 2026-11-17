package change

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// GateOutcome is what the merge rule says about the current head, and how it is published: the event's fields and the
// heimdall/gate check.
type GateOutcome struct {
	OK            bool
	Pending       bool
	Reasons       []string
	Unmet         []gate.Clause
	Status        CheckStatus
	Conclusion    CIConclusion
	Summary       string
	TreeHash      id.SHA
	PolicyVersion int
}

// EvaluateGate applies the merge rule to the view and the outside facts. It is pure: the gate row calls it, and so
// does anything that wants to show the decision.
func EvaluateGate(v View, o Outside) GateOutcome {
	facts := GateFacts(v, o)
	d := gate.Evaluate(facts)
	out := GateOutcome{OK: d.OK, Pending: GatePending(v), Unmet: d.Unmet, Reasons: gate.Reasons(d.Unmet),
		TreeHash: eventTreeHash(facts), PolicyVersion: policyVersion(o)}
	out.Summary = gate.Summary(d.OK, out.Pending, d.Unmet, MaxCheckSummary)
	switch {
	case d.OK:
		out.Status, out.Conclusion = CheckStatusCompleted, CISuccess
	case out.Pending:
		out.Status = CheckStatusInProgress
	default:
		out.Status, out.Conclusion = CheckStatusCompleted, CIFailure
	}
	return out
}

// Same reports whether the last recorded gate result already says this on this head.
func (g GateOutcome) Same(v View) bool {
	last := v.PR.Gate
	if last == nil || last.SHA != v.PR.HeadSHA || last.OK != g.OK || len(last.Reasons) != len(g.Reasons) {
		return false
	}
	for i := range g.Reasons {
		if last.Reasons[i] != g.Reasons[i] {
			return false
		}
	}
	return true
}

// digest names the result in a job key: the first 12 hex characters of sha256 over ok and the reasons.
func (g GateOutcome) digest() string {
	h := sha256.New()
	if g.OK {
		h.Write([]byte("ok"))
	} else {
		h.Write([]byte("no"))
	}
	for _, r := range g.Reasons {
		h.Write([]byte{0})
		h.Write([]byte(r))
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// policyVersion is the version the event names. A source that does not know it cannot make the version zero: the event
// requires 1 or more, and the gate is red anyway when the policy is unknown.
func policyVersion(o Outside) int {
	if o.PolicyVersion >= 1 {
		return o.PolicyVersion
	}
	if o.PolicyAtHead.Version >= 1 {
		return o.PolicyAtHead.Version
	}
	return 1
}

// eventTreeHash is the tree the event records: the pinned test tree when there is one, the head's test tree next, and
// the head commit last (the event needs a git hash even when the tests are not pinned yet).
func eventTreeHash(f gate.Facts) id.SHA {
	for _, s := range []string{f.Testset.TreeHash, f.HeadTestTreeHash, string(f.HeadSHA)} {
		if _, err := id.ParseSHA(s); err == nil {
			return id.SHA(s)
		}
	}
	return ""
}
