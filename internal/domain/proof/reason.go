package proof

import "slices"

// Reason says why a proof run is unknown, or why one candidate was not checked. It is a stable word that checks, logs
// and the summary match on, so a rewording of its text does not change it (ACTION_MODE 5.4).
type Reason string

// The reasons a run can end unknown. Each one is a fact that was needed and is not known.
const (
	ReasonBudget          Reason = "budget"           // the per-PR dollar or token cap was hit
	ReasonMonthlyCap      Reason = "monthly_cap"      // the month's spend plus one PR budget is over the repo cap
	ReasonDeadline        Reason = "deadline"         // the 15 minute wall time ran out
	ReasonSuperseded      Reason = "superseded"       // a newer head exists, so this result is for an old tree
	ReasonForkNoKey       Reason = "fork_no_key"      // a fork or Dependabot PR gets no model key
	ReasonCannotIsolate   Reason = "cannot_isolate"   // the runner cannot give PR code its own user and no network
	ReasonInputMissing    Reason = "input_missing"    // a declared input (SARIF, coverage) is not there
	ReasonWorkflowChanged Reason = "workflow_changed" // the PR edits the workflow that makes a declared input
	ReasonStateUnverified Reason = "state_unverified" // the state ref failed its chain or MAC check
	ReasonSandbox         Reason = "sandbox"          // sandbox setup or dependency fetch failed
	ReasonNoIntent        Reason = "no_intent"        // no PR text or linked issue gives an intent to test
	ReasonPlanRefused     Reason = "plan_refused"     // CheckPlan refused the test plan twice
	ReasonModelFailure    Reason = "model_failure"    // the model call failed, or its output could not be used
	ReasonNoResult        Reason = "no_result"        // the run reported nothing, or what it reported is malformed
)

// Two reasons that never leave a run unknown. A candidate carries one of them when it is not checked by design, so
// they are not stop reasons.
const (
	ReasonTrivial     Reason = "trivial"      // the PR is trivial, so third-party comments are not checked
	ReasonNotProvable Reason = "not_provable" // a style claim: no test or tool can prove it
)

// StopReasons lists every reason that can end a run unknown, in a fixed order.
func StopReasons() []Reason {
	return []Reason{
		ReasonBudget, ReasonMonthlyCap, ReasonDeadline, ReasonSuperseded, ReasonForkNoKey, ReasonCannotIsolate,
		ReasonInputMissing, ReasonWorkflowChanged, ReasonStateUnverified, ReasonSandbox, ReasonNoIntent,
		ReasonPlanRefused, ReasonModelFailure, ReasonNoResult,
	}
}

// Valid reports whether r is a reason this package defines.
func (r Reason) Valid() bool { return slices.Contains(allReasons(), r) }

// Text is the reason as a person reads it, for example "budget hit". An undefined reason reads as "no result".
func (r Reason) Text() string {
	if t, ok := reasonTexts()[r]; ok {
		return t
	}
	return reasonTexts()[ReasonNoResult]
}

// reasonTexts is a function so that no package-level state can be changed by a caller.
func reasonTexts() map[Reason]string {
	return map[Reason]string{
		ReasonBudget:          "budget hit",
		ReasonMonthlyCap:      "monthly cap reached",
		ReasonDeadline:        "timeout",
		ReasonSuperseded:      "superseded",
		ReasonForkNoKey:       "fork without key",
		ReasonCannotIsolate:   "runner cannot isolate PR code",
		ReasonInputMissing:    "input missing",
		ReasonWorkflowChanged: "workflow changed",
		ReasonStateUnverified: "state failed verification",
		ReasonSandbox:         "sandbox setup or dependency fetch failed",
		ReasonNoIntent:        "no intent",
		ReasonPlanRefused:     "test plan refused",
		ReasonModelFailure:    "model failure",
		ReasonNoResult:        "no result",
		ReasonTrivial:         "trivial PR",
		ReasonNotProvable:     "not provable by a test or tool",
	}
}

func allReasons() []Reason { return append(StopReasons(), ReasonTrivial, ReasonNotProvable) }

// CanStop reports whether r can end a whole run unknown.
func (r Reason) CanStop() bool { return slices.Contains(StopReasons(), r) }

// invalidatesResults reports whether a run that ended for r has results that cannot be trusted at all: they are for
// an old head, come from a state that failed its check, or come from a workflow the PR edited. A known failure from
// such a run is not shown as a failure.
func (r Reason) invalidatesResults() bool {
	return r == ReasonSuperseded || r == ReasonStateUnverified || r == ReasonWorkflowChanged
}

// notCheckedOK reports whether a candidate may carry r as its "not checked" reason. Only the reasons of ACTION_MODE
// 6.3 are allowed: by design (trivial, not provable), or for lack of budget, time, a key or isolation.
func (r Reason) notCheckedOK() bool {
	return slices.Contains([]Reason{
		ReasonTrivial, ReasonNotProvable, ReasonBudget, ReasonDeadline, ReasonForkNoKey, ReasonCannotIsolate,
	}, r)
}

// leavesRunUnknown reports whether a candidate not checked for r makes the whole run unknown. A style claim and a
// trivial PR are not checked on purpose. The others mean a fact is missing.
func (r Reason) leavesRunUnknown() bool {
	return r.notCheckedOK() && r != ReasonTrivial && r != ReasonNotProvable
}
