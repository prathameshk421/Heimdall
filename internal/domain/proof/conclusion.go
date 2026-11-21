package proof

import (
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

// Conclusion is the conclusion posted on the heimdall/proof check run.
type Conclusion string

// The four conclusions Heimdall uses.
const (
	ConclusionSuccess        Conclusion = "success"
	ConclusionFailure        Conclusion = "failure"
	ConclusionNeutral        Conclusion = "neutral"
	ConclusionActionRequired Conclusion = "action_required"
)

// Conclusion maps the outcome to the check conclusion for the policy's mode (ACTION_MODE 5.4):
//
//	shadow                      every verdict is neutral
//	enforce, advisory           pass is success, fail and unknown are neutral
//	enforce, proof required     pass is success, fail is failure, unknown is action_required
//
// Success means the verdict is pass and nothing else. A mode that is not shadow or enforce is not shadow, because a
// typo must not turn the gate off: it is read as enforce with the proof required.
func (o Outcome) Conclusion(a policy.Action) Conclusion {
	mode, required := a.Mode, a.ProofRequired
	switch mode {
	case "", policy.ActionModeShadow:
		return ConclusionNeutral
	case policy.ActionModeEnforce:
	default:
		required = true
	}
	switch o.Verdict() {
	case gate.VerdictPass:
		return ConclusionSuccess
	case gate.VerdictFail:
		return ifRequired(required, ConclusionFailure)
	case gate.VerdictUnknown:
		return ifRequired(required, ConclusionActionRequired)
	}
	return ifRequired(required, ConclusionFailure)
}

// ifRequired returns c when the team requires the proof, and neutral otherwise.
func ifRequired(required bool, c Conclusion) Conclusion {
	if required {
		return c
	}
	return ConclusionNeutral
}
