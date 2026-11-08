package dashboard_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/dashboard"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/testkit/changelog"
)

const otherFindingID = "fnd_01ARZ3NDEKTSV4RRFFQ69G5FAW"

func reportLog(t testing.TB, ok bool) []change.Event {
	cardRef := "card:batch.md#internal/bank/client.go"
	location := finding.Location{File: "internal/bank/client.go", Lines: [2]int{12, 12}}
	reasons := []string{"1 blocking finding(s) are open: " + findingID}
	if ok {
		reasons = nil
	}
	return changelog.New(t, pay7).
		Add(change.EventChangeTriggered, changelog.Human("alice"), triggered(pay7)).
		Add(change.EventFindingFiled, changelog.Agent("priya-reviewer"), change.FindingFiled{
			FindingID: findingID, Ref: 1, Tier: finding.TierE3, Severity: finding.SeverityCritical, State: finding.StateOpen,
			By: "priya-reviewer", Claim: "maxBatch is 2000, so the bank drops lines.", Location: &location, MemoryRef: &cardRef, CardRefs: []string{"batch.md"},
		}).
		Add(change.EventFindingFiled, changelog.Agent("sam-reviewer"), change.FindingFiled{
			FindingID: otherFindingID, Ref: 2, Tier: finding.TierE4, Severity: finding.SeverityLow, State: finding.StateUnverified, By: "sam-reviewer", Claim: "The log line names an IBAN.",
		}).
		Add(change.EventReviewSubmitted, changelog.Agent("priya-reviewer"), change.ReviewSubmitted{
			Reviewer: "priya-reviewer", FindingRefs: []id.FindingRef{1}, PackDate: "2026-11-07",
			RuleChecks: []finding.RuleCheck{{Card: "batch.md", Verdict: finding.RuleViolated, Note: "2000 is above 500."}},
		}).
		Add(change.EventGateEvaluated, changelog.Agent("heimdall"), change.GateEvaluated{OK: ok, Reasons: reasons, SHA: sha, TreeHash: sha, PolicyVersion: 1}).
		Events()
}

// reportProps decodes the props of one element.
func reportProps[T any](t *testing.T, spec dashboard.Spec, key string) T {
	t.Helper()
	raw, err := json.Marshal(spec.Elements[key].Props)
	require.NoError(t, err)
	var out T
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func buildReport(t *testing.T, events []change.Event) dashboard.Spec {
	t.Helper()
	spec, found, err := service(t, history(t, map[id.ChangeID][]change.Event{pay7: events})).Report(context.Background(), pay7)
	require.NoError(t, err)
	require.True(t, found)
	return spec
}

func TestReportSaysWhatHoldsTheMergeInTheWordsPeopleUse(t *testing.T) {
	spec := buildReport(t, reportLog(t, false))

	verdict := reportProps[dashboard.VerdictProps](t, spec, "verdict")
	require.Equal(t, "blocked", verdict.Status)
	require.Equal(t, []string{"1 blocking finding(s) are open: F1"}, verdict.Reasons, "a person never sees a ULID")

	require.Equal(t, []string{"finding-F1"}, spec.Elements["section-holding"].Children)
	require.Equal(t, []string{"finding-F2"}, spec.Elements["section-open"].Children, "the gate did not name F2")

	held := reportProps[dashboard.FindingProps](t, spec, "finding-F1")
	require.True(t, held.Holds)
	require.Equal(t, "internal/bank/client.go:12", held.Location, "one line is cited without a range")
	require.Contains(t, held.Evidence, "A rule its owner confirmed: batch.md")
	require.Contains(t, reportProps[dashboard.FindingProps](t, spec, "finding-F2").Evidence, "cannot block alone")

	rules := reportProps[dashboard.RuleChecksProps](t, spec, "rules-priya-reviewer")
	require.Equal(t, "violated", rules.Rows[0].Verdict)
}

func TestReportOfAPassedGateHoldsNothing(t *testing.T) {
	spec := buildReport(t, reportLog(t, true))
	require.Equal(t, "passed", reportProps[dashboard.VerdictProps](t, spec, "verdict").Status)
	require.NotContains(t, spec.Elements, "section-holding")
}

func TestReportBeforeTheGateRunsIsPending(t *testing.T) {
	events := changelog.New(t, pay7).Add(change.EventChangeTriggered, changelog.Human("alice"), triggered(pay7)).Events()
	spec := buildReport(t, events)
	require.Equal(t, "pending", reportProps[dashboard.VerdictProps](t, spec, "verdict").Status)
	require.Contains(t, spec.Elements, "no-findings")
}

func TestEveryElementReachesTheRootAndTheBytesAreStable(t *testing.T) {
	spec := buildReport(t, reportLog(t, false))
	seen := map[string]bool{}
	var walk func(string)
	walk = func(key string) {
		element, ok := spec.Elements[key]
		require.True(t, ok, "%s has no element", key)
		seen[key] = true
		for _, child := range element.Children {
			walk(child)
		}
	}
	walk(spec.Root)
	require.Len(t, seen, len(spec.Elements), "an element no parent names would never show")

	first, err := dashboard.MarshalReport(spec)
	require.NoError(t, err)
	second, err := dashboard.MarshalReport(buildReport(t, reportLog(t, false)))
	require.NoError(t, err)
	require.JSONEq(t, string(first), string(second))
}

func TestReportOfAnUnknownChangeIsNotFound(t *testing.T) {
	_, found, err := service(t, history(t, nil)).Report(context.Background(), pay8)
	require.NoError(t, err)
	require.False(t, found)
}
