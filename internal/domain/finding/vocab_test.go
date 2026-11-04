package finding_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
)

func TestEveryEnumParsesItsOwnValuesAndRefusesOthers(t *testing.T) {
	type enum struct {
		name   string
		values []string
		parse  func(string) error
	}
	enums := []enum{
		{"tier", []string{"E1", "E2", "E3", "E4"}, func(s string) error { _, e := finding.ParseTier(s); return e }},
		{"severity", []string{"low", "medium", "high", "critical"}, func(s string) error { _, e := finding.ParseSeverity(s); return e }},
		{"confidence", []string{"high", "medium", "low"}, func(s string) error { _, e := finding.ParseConfidence(s); return e }},
		{"category", []string{"correctness", "security", "compat", "ops", "spec_drift", "tests", "style"}, func(s string) error { _, e := finding.ParseCategory(s); return e }},
		{"state", []string{"proposed", "open", "unverified", "disputed", "withdrawn", "escalated", "upheld", "dismissed", "spec_changed", "fix_claimed", "verifying", "resolved"}, func(s string) error { _, e := finding.ParseState(s); return e }},
	}
	for _, e := range enums {
		t.Run(e.name, func(t *testing.T) {
			for _, v := range e.values {
				require.NoError(t, e.parse(v), v)
			}
			for _, bad := range []string{"", "E5", "e1", "High", "none", " open", "open ", "critical!"} {
				if contains(e.values, bad) {
					continue
				}
				require.Error(t, e.parse(bad), "should reject %q", bad)
			}
		})
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func TestEnumsRefuseBadValuesWhenDecodedFromJSON(t *testing.T) {
	var ok struct {
		Tier       finding.Tier       `json:"tier"`
		Severity   finding.Severity   `json:"severity"`
		Confidence finding.Confidence `json:"confidence"`
		Category   finding.Category   `json:"category"`
		State      finding.State      `json:"state"`
	}
	good := `{"tier":"E2","severity":"high","confidence":"medium","category":"compat","state":"fix_claimed"}`
	require.NoError(t, json.Unmarshal([]byte(good), &ok))
	out, err := json.Marshal(ok)
	require.NoError(t, err)
	require.JSONEq(t, good, string(out))
	for _, bad := range []string{`{"tier":"E9"}`, `{"severity":"urgent"}`, `{"confidence":"sure"}`, `{"category":"misc"}`, `{"state":"done"}`, `{"tier":""}`} {
		var v struct {
			Tier       finding.Tier       `json:"tier"`
			Severity   finding.Severity   `json:"severity"`
			Confidence finding.Confidence `json:"confidence"`
			Category   finding.Category   `json:"category"`
			State      finding.State      `json:"state"`
		}
		require.Error(t, json.Unmarshal([]byte(bad), &v), bad)
	}
}

func TestSeverityOrderingAndTierStrength(t *testing.T) {
	require.True(t, finding.SeverityHigh.AtLeast(finding.SeverityMedium))
	require.True(t, finding.SeverityMedium.AtLeast(finding.SeverityMedium))
	require.False(t, finding.SeverityLow.AtLeast(finding.SeverityMedium))
	require.True(t, finding.SeverityCritical.AtLeast(finding.SeverityHigh))

	require.True(t, finding.TierE1.AtLeast(finding.TierE3), "E1 is the strongest: a repro test run on head")
	require.True(t, finding.TierE3.AtLeast(finding.TierE3))
	require.False(t, finding.TierE4.AtLeast(finding.TierE3), "E4 is reasoning only and never blocks (invariant 3)")
	require.False(t, finding.Tier("").AtLeast(finding.TierE4), "an invalid value is never at least anything")
	require.False(t, finding.Severity("").AtLeast(finding.SeverityLow))
}

func TestConfidenceWeightsAreTheSpecsNumbers(t *testing.T) {
	require.InDelta(t, 1.0, finding.ConfidenceHigh.Weight(), 0)
	require.InDelta(t, 0.5, finding.ConfidenceMedium.Weight(), 0)
	require.InDelta(t, 0.2, finding.ConfidenceLow.Weight(), 0)
}

func TestBlockingStatesAreTheSpecsSet(t *testing.T) {
	blocking := []finding.State{finding.StateOpen, finding.StateDisputed, finding.StateEscalated, finding.StateUpheld, finding.StateFixClaimed, finding.StateVerifying}
	for _, s := range blocking {
		require.True(t, s.CanBlock(), s)
	}
	for _, s := range []finding.State{finding.StateProposed, finding.StateUnverified, finding.StateWithdrawn, finding.StateDismissed, finding.StateSpecChanged, finding.StateResolved} {
		require.False(t, s.CanBlock(), s)
	}
}

func TestOnlyTheFourTerminalStatesAreTerminal(t *testing.T) {
	for _, s := range []finding.State{finding.StateWithdrawn, finding.StateDismissed, finding.StateSpecChanged, finding.StateResolved} {
		require.True(t, s.Terminal(), s)
	}
	for _, s := range []finding.State{finding.StateProposed, finding.StateOpen, finding.StateUnverified, finding.StateDisputed, finding.StateEscalated, finding.StateUpheld, finding.StateFixClaimed, finding.StateVerifying} {
		require.False(t, s.Terminal(), s)
	}
}
