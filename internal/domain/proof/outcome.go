package proof

import (
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

// Kind is how a proof run ended. It is the run-level answer; each candidate has its own Class.
type Kind string

// The five ways a run ends.
const (
	// KindSkippedTrivial: the trivial rule decided no proof was needed. No model ran and the run cost $0.
	KindSkippedTrivial Kind = "skipped_trivial"
	// KindProven: the run finished, at least one plan test failed on base and passed on head, and no proven finding
	// blocks.
	KindProven Kind = "proven"
	// KindBlocked: a finding was proven and it blocks under the policy.
	KindBlocked Kind = "blocked"
	// KindNotReproduced: the run finished, but no plan test failed on base and passed on head. Nothing shows that the
	// change does what it says, so it is not a pass, and it is not a failure either.
	KindNotReproduced Kind = "not_reproduced"
	// KindUnknown: a fact the run needed is not known. Reason says which.
	KindUnknown Kind = "unknown"
)

// Class is what became of one candidate finding (ACTION_MODE 6.3).
type Class string

// The three classes. Refuted and "no proof" are drop reasons, so the summary can count them apart.
const (
	ClassProven     Class = "proven"
	ClassDropped    Class = "dropped"
	ClassNotChecked Class = "not_checked"
)

// DropReason is why a candidate was dropped.
type DropReason string

// The four drop reasons of ACTION_MODE 6.2 and 6.3.
const (
	DropRefuted    DropReason = "refuted"
	DropNoProof    DropReason = "no_proof"
	DropOutdated   DropReason = "outdated"
	DropNoLocation DropReason = "no_location"
)

// Finding is the result for one candidate, ours or a third party's. Blocking is set by the caller, who applies
// gate.BlockingSet, and only a proven finding can block.
type Finding struct {
	Class    Class
	Dropped  DropReason // set when Class is ClassDropped
	Reason   Reason     // set when Class is ClassNotChecked
	Blocking bool
}

// TestFacts is what the red-first step found. Accepted tests failed on base with an assertion and passed on head three
// times. Rejected tests proved nothing and were dropped. Ran is false when the step did not finish.
type TestFacts struct {
	Ran      bool
	Accepted int
	Rejected int
}

// Facts is everything Evaluate reads. Time and money come in as values. Nothing here is read from the clock.
type Facts struct {
	// Action is the action area at the merge base. Its budget caps the run.
	Action policy.Action
	// Trivial is the decision of Trivial for this change. A zero value is "not trivial".
	Trivial Decision
	// Stop is why the run ended before it finished, or "". It must be one of StopReasons.
	Stop     Reason
	Tests    TestFacts
	Findings []Finding
	Spend    Spend
}

// Counts is the tally the summary prints, for example "40 in, 4 proven, 30 dropped (12 refuted, 15 no proof, 3
// outdated), 6 not checked". NeedsPerson counts proven findings that do not block: a person decides what to do.
type Counts struct {
	In          int
	Proven      int
	Blocking    int
	NeedsPerson int
	Refuted     int
	NoProof     int
	Outdated    int
	NoLocation  int
	NotChecked  int
}

// Dropped is the number of dropped candidates, whatever the reason.
func (c Counts) Dropped() int { return c.Refuted + c.NoProof + c.Outdated + c.NoLocation }

// Outcome is the result of a proof run. Reason is set only when Kind is KindUnknown.
type Outcome struct {
	Kind   Kind
	Reason Reason
	Counts Counts
	Cost   Spend
}

// Evaluate turns the facts of a run into its outcome. It never fails and it never passes by default. In order:
//
//  1. A trivial skip is the outcome, at $0.
//  2. A malformed fact makes the run unknown: no result.
//  3. A run whose results cannot be trusted (superseded, state unverified, workflow changed) is unknown.
//  4. A proven finding that blocks makes the run blocked. A budget, time or sandbox stop does not hide it, as
//     gate.VerdictOf does not hide a known failure behind an unknown clause.
//  5. Any other stop, a spend above the cap, or a candidate not checked for lack of budget, time, key or isolation
//     makes the run unknown.
//  6. Plan tests that never ran make it unknown. No accepted test makes it not reproduced. Otherwise it is proven.
func Evaluate(f Facts) Outcome {
	if f.Trivial.Skip {
		return Outcome{Kind: KindSkippedTrivial}
	}
	counts, ok := tally(f.Findings)
	out := Outcome{Counts: counts, Cost: f.Spend}
	switch {
	case !ok || !validStop(f.Stop) || f.Tests.Accepted < 0 || f.Tests.Rejected < 0:
		return out.unknown(ReasonNoResult)
	case f.Stop.invalidatesResults():
		return out.unknown(f.Stop)
	case counts.Blocking > 0:
		out.Kind = KindBlocked
		return out
	}
	if r, stopped := stoppedFor(f); stopped {
		return out.unknown(r)
	}
	switch {
	case !f.Tests.Ran:
		return out.unknown(ReasonNoResult)
	case f.Tests.Accepted == 0:
		out.Kind = KindNotReproduced
	default:
		out.Kind = KindProven
	}
	return out
}

// stoppedFor returns why the run is not complete: it stopped, it spent more than its cap, or a candidate was not
// reached for lack of budget, time, a key or isolation.
func stoppedFor(f Facts) (Reason, bool) {
	if f.Stop != "" {
		return f.Stop, true
	}
	if f.Spend.Exceeds(f.Action.Effective().Budget) {
		return ReasonBudget, true
	}
	return unfinished(f.Findings)
}

func (o Outcome) unknown(r Reason) Outcome {
	o.Kind = KindUnknown
	o.Reason = r
	return o
}

func validStop(r Reason) bool { return r == "" || r.CanStop() }

// tally counts the findings. ok is false when a record is malformed: a class that does not exist, a dropped candidate
// with no drop reason, a candidate not checked for a reason it may not carry, or a blocking flag on a finding that is
// not proven.
func tally(findings []Finding) (c Counts, ok bool) {
	c.In = len(findings)
	for _, f := range findings {
		switch f.Class {
		case ClassProven:
			c.Proven++
			if f.Blocking {
				c.Blocking++
			} else {
				c.NeedsPerson++
			}
		case ClassDropped:
			if f.Blocking || !c.addDrop(f.Dropped) {
				return Counts{}, false
			}
		case ClassNotChecked:
			if f.Blocking || !f.Reason.notCheckedOK() {
				return Counts{}, false
			}
			c.NotChecked++
		default:
			return Counts{}, false
		}
	}
	return c, true
}

func (c *Counts) addDrop(r DropReason) bool {
	switch r {
	case DropRefuted:
		c.Refuted++
	case DropNoProof:
		c.NoProof++
	case DropOutdated:
		c.Outdated++
	case DropNoLocation:
		c.NoLocation++
	default:
		return false
	}
	return true
}

// unfinished returns the reason of the first candidate not checked for a missing fact.
func unfinished(findings []Finding) (Reason, bool) {
	for _, f := range findings {
		if f.Class == ClassNotChecked && f.Reason.leavesRunUnknown() {
			return f.Reason, true
		}
	}
	return "", false
}

// Verdict maps the kind to the three-state verdict. Only a skip and a proven run pass. A kind that is not defined
// fails, so a value nobody expected is never a pass.
func (o Outcome) Verdict() gate.Verdict {
	switch o.Kind {
	case KindSkippedTrivial, KindProven:
		return gate.VerdictPass
	case KindNotReproduced, KindUnknown:
		return gate.VerdictUnknown
	case KindBlocked:
		return gate.VerdictFail
	}
	return gate.VerdictFail
}

// Headline is the first line of the proof summary and of the check output.
func (o Outcome) Headline() string {
	switch o.Kind {
	case KindSkippedTrivial:
		return "skipped: trivial (rule)"
	case KindProven:
		return "proven"
	case KindBlocked:
		return "blocked: a proven finding blocks the merge"
	case KindNotReproduced:
		return "not reproduced: no test failed on base and passed on head"
	case KindUnknown:
		return "unknown: " + o.Reason.Text()
	}
	return "unknown: " + ReasonNoResult.Text()
}
