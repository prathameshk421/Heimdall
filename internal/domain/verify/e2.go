package verify

import (
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/sarif"
)

// TierForE2 returns the tier of an E2 finding (a deterministic tool result, TECH_SPEC 11.1). The finding is confirmed
// (E2) only when the check fails on the head and the failure is at the location the finding names. Any other outcome
// leaves the finding at E4, which never blocks a merge.
func TierForE2(head Status, located bool) finding.Tier {
	if head == StatusFail && located {
		return finding.TierE2
	}
	return finding.TierE4
}

// ResolvedE2 reports whether a push resolves an E2 finding: the check no longer reports a diagnostic at the finding's
// location. A pass resolves it, and so does a failure somewhere else. A diagnostic still at the location, or a check
// that errored, does not.
func ResolvedE2(fixed Status, located bool) bool {
	switch fixed {
	case StatusPass:
		return true
	case StatusFail:
		return !located
	case StatusError, StatusSkipped:
		return false
	default:
		return false
	}
}

// Located reports whether a check result is in file with its line range touching first to last, inclusive. It is the
// location rule that confirms an E2 finding (HARNESS_DESIGN B7).
func Located(results []sarif.Result, file string, first, last int) bool {
	for _, r := range results {
		if r.File == file && r.Lines[0] <= last && r.Lines[1] >= first {
			return true
		}
	}
	return false
}
