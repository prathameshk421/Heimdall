package knowledge

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const refundClaim = "The refund cap ignores refunds that were already made."

func filedEvent(t *testing.T) change.Event {
	t.Helper()
	two, err := id.ParseFindingRef("F2")
	require.NoError(t, err)
	return workflowMemoryEvent(t, 1, change.EventFindingFiled, change.ActorAgent, "rhea", change.FindingFiled{
		FindingID: "fnd_01J8Z3K9QW7M2P5R8T0V4YB6CD", Ref: two, Tier: finding.TierE4, Severity: finding.SeverityHigh,
		State: finding.StateUnverified, By: "rhea", Claim: refundClaim, Category: finding.CategoryCorrectness,
		Location: &finding.Location{File: "internal/refunds/service.go", Lines: [2]int{4, 9}},
	})
}

func upholdEvent(t *testing.T, n int) change.Event {
	t.Helper()
	return workflowMemoryEvent(t, n, change.EventDecisionRecorded, change.ActorHuman, "prathamesh",
		change.DecisionRecorded{Kind: change.DecisionUphold, Target: "F2", Login: "prathamesh", Reason: "the cap is wrong", SourceRef: "chat:1"})
}

func TestNarrateNamesThePersonTheFindingTheFileAndTheReason(t *testing.T) {
	filed := filedEvent(t)
	facts := FactsFromEvents("PAY-142", []change.Event{filed})
	got := Narrate(upholdEvent(t, 2), facts)
	require.Equal(t, "On 2026-11-06, prathamesh upheld finding F2 on internal/refunds/service.go: "+refundClaim+" Reason: the cap is wrong.", got)
}

func TestNarrateWithoutFactsUsesWhatTheEventHas(t *testing.T) {
	require.Equal(t, "On 2026-11-06, prathamesh upheld finding F2. Reason: the cap is wrong.", Narrate(upholdEvent(t, 2), nil))
}

func TestNarrateUsesTheClaimOfAResolvedStateEvent(t *testing.T) {
	two, _ := id.ParseFindingRef("F2")
	state := workflowMemoryEvent(t, 3, change.EventFindingStateChanged, change.ActorAgent, "rhea", change.FindingStateChanged{
		FindingID: "fnd_01J8Z3K9QW7M2P5R8T0V4YB6CD", Ref: two, Tier: finding.TierE2, Severity: finding.SeverityHigh,
		State: finding.StateResolved, By: "rhea"})
	got := Narrate(state, FactsFromEvents("PAY-142", []change.Event{filedEvent(t)}))
	require.Equal(t, "On 2026-11-06, finding F2 on internal/refunds/service.go reached the state resolved: "+refundClaim, got)
}

func TestIngestorPutsTheSummaryFirstAndKeepsTheBodyValidJSON(t *testing.T) {
	queue := &workflowEpisodeQueue{items: map[change.EffectKey]ports.Episode{}}
	ing, err := NewWorkflowIngestor(queue, &workflowScopeSpy{groups: []string{"payments"}}, identityMemoryScanner{})
	require.NoError(t, err)
	events := []change.Event{filedEvent(t), upholdEvent(t, 2)}
	_, err = ing.Record(context.Background(), events)
	require.NoError(t, err)
	body := queue.items[change.EffectKey("workflow:v2:"+string(events[1].ID)+":payments")].Body
	require.Contains(t, body[:20], `"summary"`)
	var decoded struct{ Summary string }
	require.NoError(t, json.Unmarshal([]byte(body), &decoded))
	require.Contains(t, decoded.Summary, refundClaim)
	require.Contains(t, decoded.Summary, "internal/refunds/service.go")
}

type loaderFake struct {
	events []change.Event
	calls  int
}

func (l *loaderFake) Load(context.Context, id.ChangeID) ([]change.Event, error) {
	l.calls++
	return l.events, nil
}

func TestIngestorLoadsTheChangeOnceWhenTheClaimIsNotInTheBatch(t *testing.T) {
	queue := &workflowEpisodeQueue{items: map[change.EffectKey]ports.Episode{}}
	loader := &loaderFake{events: []change.Event{filedEvent(t)}}
	ing, err := NewWorkflowIngestor(queue, &workflowScopeSpy{groups: []string{"payments"}}, identityMemoryScanner{})
	require.NoError(t, err)
	ing.WithHistory(loader)
	events := []change.Event{upholdEvent(t, 2), upholdEvent(t, 3)}
	_, err = ing.Record(context.Background(), events)
	require.NoError(t, err)
	require.Equal(t, 1, loader.calls)
	require.Contains(t, queue.items[change.EffectKey("workflow:v2:"+string(events[0].ID)+":payments")].Body, refundClaim)
}
