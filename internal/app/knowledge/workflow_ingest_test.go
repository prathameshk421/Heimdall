package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/stretchr/testify/require"
)

func TestWorkflowIngestorPersistsConfirmedAndObservedSourcesIdempotently(t *testing.T) {
	queue := &workflowEpisodeQueue{items: map[change.EffectKey]ports.Episode{}}
	scopes := &workflowScopeSpy{groups: []string{"payments"}}
	findingOne, err := id.ParseFindingRef("F1")
	require.NoError(t, err)
	findingTwo, err := id.ParseFindingRef("F2")
	require.NoError(t, err)
	ingestor, err := NewWorkflowIngestor(queue, scopes, identityMemoryScanner{})
	require.NoError(t, err)
	events := []change.Event{
		workflowMemoryEvent(t, 1, change.EventDecisionRecorded, change.ActorHuman, "alice", change.DecisionRecorded{Kind: change.DecisionUphold, Target: "F1", Login: "alice", Reason: "verified at head", SourceRef: "chat:decision-1"}),
		workflowMemoryEvent(t, 2, change.EventChangeMerged, change.ActorSystem, "engine", change.Merged{SHA: "0123456789012345678901234567890123456789"}),
		workflowMemoryEvent(t, 3, change.EventFindingStateChanged, change.ActorAgent, "reviewer", change.FindingStateChanged{FindingID: "fnd_01J8Z3K9QW7M2P5R8T0V4YB6CD", Ref: findingOne, Tier: finding.TierE2, Severity: finding.SeverityHigh, State: finding.StateResolved, By: "reviewer"}),
		workflowMemoryEvent(t, 4, change.EventFindingStateChanged, change.ActorAgent, "reviewer", change.FindingStateChanged{FindingID: "fnd_01J8Z3K9QW7M2P5R8T0V4YB6CE", Ref: findingTwo, Tier: finding.TierE2, Severity: finding.SeverityHigh, State: finding.StateOpen, By: "reviewer"}),
	}
	count, err := ingestor.Record(context.Background(), events)
	require.NoError(t, err)
	require.Equal(t, 4, count, "confirmed sources are copied to shared; resolved findings stay within their team")
	require.Len(t, queue.items, 4)
	require.Equal(t, events[:3], scopes.seen, "events without a memory projection do not trigger scope resolution")
	require.Equal(t, ports.EpisodeConfirmed, queue.items[change.EffectKey("workflow:v2:"+string(events[0].ID)+":payments")].Status)
	require.Equal(t, ports.EpisodeConfirmed, queue.items[change.EffectKey("workflow:v2:"+string(events[0].ID)+":shared")].Status)
	require.Equal(t, ports.EpisodeObserved, queue.items[change.EffectKey("workflow:v2:"+string(events[1].ID)+":payments")].Status)
	require.Contains(t, queue.items[change.EffectKey("workflow:v2:"+string(events[1].ID)+":payments")].Body, "heimdall:event:"+string(events[1].ID))

	count, err = ingestor.Record(context.Background(), events)
	require.NoError(t, err)
	require.Equal(t, 4, count)
	require.Len(t, queue.items, 4, "replay uses stable keys and does not create duplicate source records")
}

func TestWorkflowIngestorFailsClosedWithoutServerDerivedGroups(t *testing.T) {
	ingestor, err := NewWorkflowIngestor(&workflowEpisodeQueue{items: map[change.EffectKey]ports.Episode{}}, &workflowScopeSpy{}, identityMemoryScanner{})
	require.NoError(t, err)
	event := workflowMemoryEvent(t, 1, change.EventChangeMerged, change.ActorSystem, "engine", change.Merged{SHA: "0123456789012345678901234567890123456789"})
	_, err = ingestor.Record(context.Background(), []change.Event{event})
	require.ErrorContains(t, err, "no server-derived memory group")
}

func TestWorkflowBackfillReplaysSafelyAfterRestart(t *testing.T) {
	queue := &workflowEpisodeQueue{items: map[change.EffectKey]ports.Episode{}}
	ingestor, err := NewWorkflowIngestor(queue, &workflowScopeSpy{groups: []string{"payments"}}, identityMemoryScanner{})
	require.NoError(t, err)
	events := []change.Event{
		workflowMemoryEvent(t, 1, change.EventChangeMerged, change.ActorSystem, "engine", change.Merged{SHA: "0123456789012345678901234567890123456789"}),
		workflowMemoryEvent(t, 2, change.EventChangeRejected, change.ActorHuman, "alice", change.Rejected{Reason: "policy"}),
	}
	history := &workflowHistoryFake{events: events}
	cursor := &workflowCursorFake{}
	count, err := ingestor.Backfill(context.Background(), history, cursor, 1)
	require.NoError(t, err)
	require.Equal(t, 2, count)
	require.Equal(t, int64(2), cursor.value)

	// A restarted consumer reads only events added after its saved sequence.
	count, err = ingestor.Backfill(context.Background(), history, cursor, 1)
	require.NoError(t, err)
	require.Zero(t, count)
	require.Len(t, queue.items, 2)
}

type workflowHistoryFake struct{ events []change.Event }

func (h *workflowHistoryFake) EventsAfter(_ context.Context, after int64, limit int) ([]change.Event, int64, error) {
	start := int(after)
	if start >= len(h.events) {
		return nil, after, nil
	}
	end := min(start+limit, len(h.events))
	return append([]change.Event(nil), h.events[start:end]...), int64(end), nil
}

type workflowCursorFake struct{ value int64 }

func (c *workflowCursorFake) LoadWorkflowCursor(context.Context) (int64, error) { return c.value, nil }
func (c *workflowCursorFake) SaveWorkflowCursor(_ context.Context, value int64) error {
	c.value = value
	return nil
}

type workflowScopeSpy struct {
	groups []string
	seen   []change.Event
}

func (s *workflowScopeSpy) GroupsForEvent(_ context.Context, event change.Event) ([]string, error) {
	s.seen = append(s.seen, event)
	return append([]string(nil), s.groups...), nil
}

type identityMemoryScanner struct{}

func (identityMemoryScanner) Scan(raw string) (string, []RedactionSummary, error) {
	return raw, nil, nil
}

type workflowEpisodeQueue struct {
	items map[change.EffectKey]ports.Episode
}

func (q *workflowEpisodeQueue) EnqueueEpisode(_ context.Context, episode ports.Episode) error {
	if existing, ok := q.items[episode.Key]; ok {
		if existing != episode {
			return errors.New("episode replay changed content")
		}
		return nil
	}
	q.items[episode.Key] = episode
	return nil
}

func (*workflowEpisodeQueue) PendingEpisodes(context.Context, int) ([]ports.Episode, error) {
	return nil, nil
}
func (*workflowEpisodeQueue) MarkEpisodeDelivered(context.Context, change.EffectKey, time.Time) error {
	return nil
}
func (*workflowEpisodeQueue) InvalidateEpisode(context.Context, change.EffectKey, time.Time) error {
	return nil
}

func workflowMemoryEvent(t *testing.T, n int, typ change.EventType, actor change.ActorKind, actorID string, payload any) change.Event {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	event := change.Event{
		ID:            change.EventID(fmt.Sprintf("20000000-0000-4000-8000-%012d", n)),
		ChangeID:      "PAY-142",
		Version:       1,
		Type:          typ,
		Actor:         change.Actor{Kind: actor, ID: change.ActorID(actorID)},
		OccurredAt:    time.Date(2026, 11, 6, 1, 0, 0, 0, time.UTC),
		CorrelationID: "corr-1",
		Payload:       raw,
		BlobRefs:      []workflow.BlobRef{},
	}
	require.NoError(t, event.Validate())
	return event
}

func (q *workflowEpisodeQueue) DeferEpisode(context.Context, change.EffectKey, time.Time) error {
	return nil
}
