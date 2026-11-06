package gate

import (
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

// FindingState is what BlockingSet reads about one finding. Consensus is true when the finding is consensus-blocking
// under TECH_SPEC 11.2b, which is decided by the caller.
type FindingState struct {
	ID          id.FindingID
	Tier        finding.Tier
	Severity    finding.Severity
	State       finding.State
	Consensus   bool
	Fingerprint string
	Reporter    string
	Confidence  finding.Confidence
}

// BlockingSet returns the findings that hold a merge, in the order given (TECH_SPEC 11.1). A finding blocks only in a
// state that can block, at or above the policy's minimum severity and tier, or when it is consensus-blocking. E4 never
// blocks (invariant 3), whatever the policy or the consensus flag says.
func BlockingSet(findings []FindingState, minimum policy.Blocking) []id.FindingID {
	var out []id.FindingID
	for _, f := range findings {
		if blocks(f, minimum) {
			out = append(out, f.ID)
		}
	}
	return out
}

func blocks(f FindingState, minimum policy.Blocking) bool {
	if f.Tier == finding.TierE4 || !f.State.CanBlock() {
		return false
	}
	if f.Consensus {
		return true
	}
	if !f.Tier.AtLeast(minimum.MinTier) {
		return false
	}
	// A finding whose severity is missing or unknown is not below the minimum: it blocks, so no missing field can
	// let a finding through (TECH_SPEC 11.1).
	if !f.Severity.Valid() {
		return true
	}
	return f.Severity.AtLeast(minimum.MinSeverity)
}
