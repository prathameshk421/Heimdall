package verify_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/verify"
)

// A memory reference is E3 evidence only while it is found, confirmed and not superseded (TECH_SPEC 11.1).
func TestE3NeedsAFoundConfirmedNotSupersededReference(t *testing.T) {
	require.Equal(t, finding.TierE3, verify.TierForE3(verify.MemoryState{Found: true, Confirmed: true}))
	require.Equal(t, finding.TierE4, verify.TierForE3(verify.MemoryState{Found: false, Confirmed: true}), "not found")
	require.Equal(t, finding.TierE4, verify.TierForE3(verify.MemoryState{Found: true, Confirmed: false}), "observed, not confirmed")
	require.Equal(t, finding.TierE4, verify.TierForE3(verify.MemoryState{Found: true, Confirmed: true, Superseded: true}), "superseded")
}
