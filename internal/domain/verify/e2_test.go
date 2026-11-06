package verify_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/verify"
)

// An E2 finding (a tool result such as oasdiff or go vet) is confirmed only when the check fails on the head at the
// location the finding names. Any other outcome leaves it at E4, which never blocks (A2, TECH_SPEC 11.1).
func TestE2IsConfirmedOnlyByAFailureAtTheNamedLocation(t *testing.T) {
	require.Equal(t, finding.TierE2, verify.TierForE2(verify.StatusFail, true))
	require.Equal(t, finding.TierE4, verify.TierForE2(verify.StatusFail, false), "a failure elsewhere does not confirm it")
	require.Equal(t, finding.TierE4, verify.TierForE2(verify.StatusPass, true), "a passing check does not confirm it")
	require.Equal(t, finding.TierE4, verify.TierForE2(verify.StatusError, true), "a check that errors is not a failure")
}

// A push resolves an E2 finding only when the same check passes after the fix.
func TestE2IsResolvedOnlyByAPassAfterTheFix(t *testing.T) {
	require.True(t, verify.ResolvedE2(verify.StatusPass))
	require.False(t, verify.ResolvedE2(verify.StatusFail))
	require.False(t, verify.ResolvedE2(verify.StatusError))
}
