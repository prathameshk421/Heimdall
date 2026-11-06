package gate_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

// A finding whose severity is missing or unknown is not below the minimum: it blocks, so no missing field lets a
// finding through (TECH_SPEC 11.1).
func TestFindingWithUnknownSeverityBlocks(t *testing.T) {
	f := gate.FindingState{
		ID:       id.FindingID("fnd_01HZX3K9QW7M2P5R8T0V4YB6CD"),
		Tier:     finding.TierE2,
		Severity: "",
		State:    finding.StateOpen,
	}
	minimum := policy.Blocking{MinSeverity: finding.SeverityCritical, MinTier: finding.TierE3}
	require.Equal(t, []id.FindingID{f.ID}, gate.BlockingSet([]gate.FindingState{f}, minimum))
}
