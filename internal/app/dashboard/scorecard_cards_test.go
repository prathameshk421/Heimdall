package dashboard_test

import (
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/dashboard"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/testkit/changelog"
	"github.com/stretchr/testify/require"
)

func TestScorecardCountsByCardAndCategory(t *testing.T) {
	t.Parallel()
	f1 := id.FindingID("fnd_01HZX3K9QW7M2P5R8T0V4YB6C1")
	f2 := id.FindingID("fnd_01HZX3K9QW7M2P5R8T0V4YB6C2")
	sys := change.Actor{Kind: change.ActorSystem, ID: "heimdall"}
	events := changelog.New(t, "KAN-6").
		Add(change.EventFindingFiled, changelog.Agent("reviewer-1"), change.FindingFiled{
			FindingID: f1, Ref: 1, Tier: finding.TierE1, Severity: finding.SeverityHigh, State: finding.StateOpen,
			By: "reviewer-1", Category: finding.CategorySecurity, CardRefs: []string{"ADR-004"},
		}).
		Add(change.EventFindingVerified, sys, change.FindingVerified{
			FindingID: f1, Ref: 1, Tier: finding.TierE1, Severity: finding.SeverityHigh, State: finding.StateOpen, By: "heimdall",
		}).
		Add(change.EventFindingStateChanged, changelog.Human("alice"), change.FindingStateChanged{
			FindingID: f1, Ref: 1, Tier: finding.TierE1, Severity: finding.SeverityHigh, State: finding.StateUpheld, By: "alice",
		}).
		Add(change.EventFindingFiled, changelog.Agent("reviewer-2"), change.FindingFiled{
			FindingID: f2, Ref: 2, Tier: finding.TierE4, Severity: finding.SeverityLow, State: finding.StateUnverified,
			By: "reviewer-2", Category: finding.CategoryStyle, CardRefs: []string{"STYLE-1"},
		}).
		Add(change.EventFindingStateChanged, changelog.Human("alice"), change.FindingStateChanged{
			FindingID: f2, Ref: 2, Tier: finding.TierE4, Severity: finding.SeverityLow, State: finding.StateDismissed, By: "alice",
		}).
		Events()

	sc := dashboard.ScorecardFromEvents(events)
	require.Equal(t, dashboard.CardScore{Filed: 1, Verified: 1, Upheld: 1}, sc.ByCard["ADR-004"])
	require.Equal(t, dashboard.CardScore{Filed: 1, Dismissed: 1}, sc.ByCard["STYLE-1"])
	require.Equal(t, dashboard.CardScore{Filed: 1, Verified: 1, Upheld: 1}, sc.ByCategory["security"])
	require.Equal(t, dashboard.CardScore{Filed: 1, Dismissed: 1}, sc.ByCategory["style"])
}
