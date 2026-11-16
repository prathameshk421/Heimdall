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
