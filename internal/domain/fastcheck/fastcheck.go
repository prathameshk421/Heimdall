package fastcheck

import (
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
)

// ClauseID names one clause of the fast check. The ids are stable: a check summary, an event and a test refer to the
// same clause by the same name. They are the clause names of ACTION_MODE 5.1.
type ClauseID string

// The clauses of the fast check, in the order ACTION_MODE 5.1 lists them.
const (
	ClauseSecrets       ClauseID = "secrets"
	ClauseAnalyzers     ClauseID = "analyzers"
	ClausePinnedTests   ClauseID = "pinned_tests"
	ClauseTestIntegrity ClauseID = "test_integrity"
	ClauseDiffCoverage  ClauseID = "diff_coverage"
	ClauseApproval      ClauseID = "approval"
	ClauseMemoryRules   ClauseID = "memory_rules"
	ClauseHeimdallFiles ClauseID = "heimdall_files"
)

// Status is what one clause says. Met and Unmet are known. Unknown means a fact the clause needs is missing, and it
// never reads as met. NotChecked means the policy does not ask for the clause, so it neither helps nor blocks.
type Status string

// The four statuses of a clause.
const (
	StatusMet        Status = "met"
	StatusUnmet      Status = "unmet"
	StatusUnknown    Status = "unknown"
	StatusNotChecked Status = "not_checked"
)

// Clause is the outcome of one clause. Reason is always set: it says what holds, what is wrong or what is missing,
// in words a person can act on. It never carries a secret value.
type Clause struct {
	ID     ClauseID
	Status Status
	Reason string
}

// Result is the whole fast check: every clause, the gate decision built from the unmet and unknown ones, and the
// verdict of that decision (gate.VerdictOf).
type Result struct {
	Clauses  []Clause
	Decision gate.Decision
	Verdict  gate.Verdict
}

// Evaluate applies the eight clauses to the facts. It reads the action policy of the merge base through
// policy.Action.Effective, so an absent area means the defaults. It lists every clause, not only the unmet ones, so
// the summary can say what was checked. A clause that is met or not checked adds nothing to the decision.
func Evaluate(f Facts) Result {
	f.Action = f.Action.Effective()
	clauses := []Clause{
		checkSecrets(f),
		checkAnalyzers(f),
		checkPinnedTests(f),
		checkTestIntegrity(f),
		checkDiffCoverage(f),
		checkApproval(f),
		checkMemoryRules(f),
		checkHeimdallFiles(f),
	}
	var unmet []gate.Clause
	for _, c := range clauses {
		switch c.Status {
		case StatusUnmet:
			unmet = append(unmet, gate.Clause{ID: gate.ClauseID(c.ID), Reason: c.Reason})
		case StatusUnknown:
			unmet = append(unmet, gate.Clause{ID: gate.ClauseID(c.ID), Reason: c.Reason, Unknown: true})
		case StatusMet, StatusNotChecked:
			// Neither adds to the decision.
		}
	}
	d := gate.Decision{OK: len(unmet) == 0, Unmet: unmet}
	return Result{Clauses: clauses, Decision: d, Verdict: gate.VerdictOf(d)}
}

func met(id ClauseID, reason string) Clause { return Clause{ID: id, Status: StatusMet, Reason: reason} }
func unmet(id ClauseID, reason string) Clause {
	return Clause{ID: id, Status: StatusUnmet, Reason: reason}
}
func unknown(id ClauseID, reason string) Clause {
	return Clause{ID: id, Status: StatusUnknown, Reason: reason}
}
func notChecked(id ClauseID, reason string) Clause {
	return Clause{ID: id, Status: StatusNotChecked, Reason: reason}
}
