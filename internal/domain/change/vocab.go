package change

// TriggerSource says how a change came to exist.
type TriggerSource string

// The trigger sources. manual is the fallback start by hand (`heimdall start KEY`, TECH_SPEC 2.5).
const (
	TriggerJira   TriggerSource = "jira"
	TriggerFile   TriggerSource = "file"
	TriggerPR     TriggerSource = "pr"
	TriggerManual TriggerSource = "manual"
)

// Valid reports whether s is a trigger source.
func (s TriggerSource) Valid() bool {
	return oneOf(s, TriggerJira, TriggerFile, TriggerPR, TriggerManual)
}

// PickMethod says how reviewers were chosen (TECH_SPEC 3.2 and 8.1).
type PickMethod string

// The pick methods.
const (
	PickDashboard PickMethod = "dashboard"
	PickCommand   PickMethod = "command"
	PickTimer     PickMethod = "timer"
	PickDefault   PickMethod = "default"
)

// Valid reports whether m is a pick method.
func (m PickMethod) Valid() bool {
	return oneOf(m, PickDashboard, PickCommand, PickTimer, PickDefault)
}

// DecisionKind is a human decision in the grammar of TECH_SPEC 11.5.
type DecisionKind string

// The decision kinds, one per command of the grammar.
const (
	DecisionReview        DecisionKind = "review"
	DecisionUphold        DecisionKind = "uphold"
	DecisionDismiss       DecisionKind = "dismiss"
	DecisionSpecChange    DecisionKind = "spec_change"
	DecisionOverride      DecisionKind = "override"
	DecisionWaiveReviewer DecisionKind = "waive_reviewer"
	DecisionReject        DecisionKind = "reject"
	DecisionApproveTests  DecisionKind = "approve_tests"
	DecisionIncident      DecisionKind = "incident"
	DecisionStart         DecisionKind = "start"
	DecisionChallenge     DecisionKind = "challenge"
	DecisionRemember      DecisionKind = "remember"
	DecisionForget        DecisionKind = "forget"
	DecisionResume        DecisionKind = "resume"
	DecisionAnswer        DecisionKind = "answer"
)

// Valid reports whether k is a decision kind.
func (k DecisionKind) Valid() bool {
	return oneOf(k, DecisionReview, DecisionUphold, DecisionDismiss, DecisionSpecChange, DecisionOverride,
		DecisionWaiveReviewer, DecisionReject, DecisionApproveTests, DecisionIncident, DecisionStart,
		DecisionChallenge, DecisionRemember, DecisionForget, DecisionResume, DecisionAnswer)
}

// Verdict is the resolver's answer to a case (TECH_SPEC 11.3).
type Verdict string

// The verdicts.
const (
	VerdictUphold     Verdict = "uphold"
	VerdictDismiss    Verdict = "dismiss"
	VerdictAmend      Verdict = "amend"
	VerdictSpecChange Verdict = "spec_change"
	VerdictNeedsHuman Verdict = "needs_human"
)

// Valid reports whether v is a verdict.
func (v Verdict) Valid() bool {
	return oneOf(v, VerdictUphold, VerdictDismiss, VerdictAmend, VerdictSpecChange, VerdictNeedsHuman)
}

// CIConclusion is the final result of one check run on a commit (GitHub check_run conclusions).
type CIConclusion string

// The conclusions a completed check run can have.
const (
	CISuccess        CIConclusion = "success"
	CIFailure        CIConclusion = "failure"
	CINeutral        CIConclusion = "neutral"
	CICancelled      CIConclusion = "cancelled"
	CISkipped        CIConclusion = "skipped"
	CITimedOut       CIConclusion = "timed_out"
	CIActionRequired CIConclusion = "action_required"
)

// Valid reports whether c is a conclusion.
func (c CIConclusion) Valid() bool {
	return oneOf(c, CISuccess, CIFailure, CINeutral, CICancelled, CISkipped, CITimedOut, CIActionRequired)
}
