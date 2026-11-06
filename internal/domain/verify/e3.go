package verify

import "github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"

// MemoryState is what a re-query found for a memory reference a finding cites.
type MemoryState struct {
	Found      bool
	Confirmed  bool
	Superseded bool
}

// TierForE3 returns the tier of a finding whose evidence is a memory reference (TECH_SPEC 11.1). Only a reference that
// is found, confirmed and not superseded is E3 evidence; anything else leaves the finding at E4, which never blocks.
func TierForE3(m MemoryState) finding.Tier {
	if m.Found && m.Confirmed && !m.Superseded {
		return finding.TierE3
	}
	return finding.TierE4
}
