package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

func TestWorkflowEventBackfillQueuesConfirmedEventsOncePerAuthorizedTeam(t *testing.T) {
	decision := eventForMemory(t, change.EventDecisionRecorded, change.ActorHuman, `{"kind":"uphold","target":"F1","login":"alice","reason":"verified","source_ref":"PR-3"}`)
	observed := eventForMemory(t, change.EventTaskProgress, change.ActorAgent, `{"text":"in progress"}`)
	store := &memoryHistory{events: map[id.ChangeID][]change.Event{"PAY-3": {decision, observed}}}
	queue := &memoryEpisodeQueue{}
	service, err := NewWorkflowMemory(queue, store, fixedTeams{"payments"}, redactMemory{}, memoryClock{time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)}, emptyRooms{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.Backfill(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.Queued != 1 || len(queue.episodes) != 1 {
		t.Fatalf("first backfill = %#v, episodes=%d", first, len(queue.episodes))
	}
	if queue.episodes[0].Status != ports.EpisodeConfirmed || queue.episodes[0].GroupID != "payments" {
		t.Fatalf("episode trust/scope = %#v", queue.episodes[0])
	}
	second, err := service.Backfill(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.Queued != 1 || len(queue.episodes) != 1 {
		t.Fatalf("replay duplicated episodes: %#v, %#v", second, queue.episodes)
	}
}

func TestWorkflowMemoryBackfillCreatesRedactedObservedRoomDigestAtTerminalEvent(t *testing.T) {
	trigger := eventForMemory(t, change.EventChangeTriggered, change.ActorJira, `{"ticket":"PAY-3","repo":"aarush323/pay-demo","assignee":"alice","source":"jira"}`)
	trigger.Payload = []byte(`{"ticket":"PAY-3","repo":"aarush323/pay-demo","assignee":"alice","source":"jira"}`)
	trigger.ID = "evt_01JQW3T8E7P3S5D4N6C2B8A003"
	room := eventForMemory(t, change.EventRoomOpened, change.ActorSystem, `{"room_id":"room-3"}`)
	room.ID = "evt_01JQW3T8E7P3S5D4N6C2B8A004"
	terminal := eventForMemory(t, change.EventChangeMerged, change.ActorSystem, `{"sha":"a01b2c3d4e5f60718293a4b5c6d7e8f901234567"}`)
	terminal.ID = "evt_01JQW3T8E7P3S5D4N6C2B8A005"
	store := &memoryHistory{events: map[id.ChangeID][]change.Event{"PAY-3": {trigger, room, terminal}}}
	queue := &memoryEpisodeQueue{}
	reader := &memoryRoomReader{messages: []ports.Message{{ID: "msg_3", Room: "room-3", Sender: "alice", Text: "Decision includes CANARY_TOKEN=abc", At: time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)}}}
	service, err := NewWorkflowMemory(queue, store, fixedTeams{"payments"}, redactingMemory{}, memoryClock{time.Now()}, reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Backfill(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(queue.episodes) != 1 {
		t.Fatalf("digest episodes = %d, want 1", len(queue.episodes))
	}
	got := queue.episodes[0]
	if got.Source != "change-digest" || got.Status != ports.EpisodeObserved || got.GroupID != "payments" {
		t.Fatalf("digest trust/scope = %#v", got)
	}
	if !strings.Contains(got.Body, "[removed]") || strings.Contains(got.Body, "CANARY_TOKEN=abc") {
		t.Fatalf("digest contains unredacted room content: %s", got.Body)
	}
	if reader.room != "room-3" {
		t.Fatalf("digest read room %q", reader.room)
	}
}

func eventForMemory(t *testing.T, typ change.EventType, kind change.ActorKind, payload string) change.Event {
	t.Helper()
	var raw json.RawMessage = []byte(payload)
	return change.Event{ID: "evt_01JQW3T8E7P3S5D4N6C2B8A001", ChangeID: "PAY-3", Version: 1, Type: typ, Actor: change.Actor{Kind: kind, ID: "alice"}, OccurredAt: time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC), CorrelationID: "corr_01JQW3T8E7P3S5D4N6C2B8A002", Payload: raw}
}

type memoryHistory struct {
	events map[id.ChangeID][]change.Event
}

func (m *memoryHistory) ChangeIDs(context.Context) ([]id.ChangeID, error) {
	out := make([]id.ChangeID, 0, len(m.events))
	for key := range m.events {
		out = append(out, key)
	}
	return out, nil
}
func (m *memoryHistory) Load(_ context.Context, key id.ChangeID) ([]change.Event, error) {
	return m.events[key], nil
}

type memoryEpisodeQueue struct{ episodes []ports.Episode }

func (m *memoryEpisodeQueue) Enqueue(_ context.Context, episode ports.Episode) error {
	for _, old := range m.episodes {
		if old.Key == episode.Key {
			if old != episode {
				return context.Canceled
			}
			return nil
		}
	}
	m.episodes = append(m.episodes, episode)
	return nil
}
func (*memoryEpisodeQueue) PendingEpisodes(context.Context, int) ([]ports.Episode, error) {
	return nil, nil
}
func (*memoryEpisodeQueue) MarkEpisodeDelivered(context.Context, change.EffectKey, time.Time) error {
	return nil
}
func (*memoryEpisodeQueue) InvalidateEpisode(context.Context, change.EffectKey, time.Time) error {
	return nil
}

type fixedTeams []string

func (f fixedTeams) TeamsForEvent(context.Context, change.Event, change.Repo, id.SHA, []string) ([]string, error) {
	return f, nil
}

type redactMemory struct{}

func (redactMemory) Redact(text string) (string, error) { return text, nil }

type memoryClock struct{ now time.Time }

func (c memoryClock) Now() time.Time { return c.now }

type emptyRooms struct{}

func (emptyRooms) Read(context.Context, change.RoomID, ports.Cursor) ([]ports.Message, ports.Cursor, error) {
	return nil, "", nil
}

type memoryRoomReader struct {
	messages []ports.Message
	room     change.RoomID
}

func (m *memoryRoomReader) Read(_ context.Context, room change.RoomID, _ ports.Cursor) ([]ports.Message, ports.Cursor, error) {
	m.room = room
	return m.messages, "end", nil
}

type redactingMemory struct{}

func (redactingMemory) Redact(text string) (string, error) {
	return strings.ReplaceAll(text, "CANARY_TOKEN=abc", "[removed]"), nil
}

func (m *memoryEpisodeQueue) DeferEpisode(context.Context, change.EffectKey, time.Time) error {
	return nil
}

// failingTeams cannot read the policy of one change, as when its repository has no heimdall.yaml.
type failingTeams struct{ broken id.ChangeID }

func (f failingTeams) TeamsForEvent(_ context.Context, event change.Event, _ change.Repo, _ id.SHA, _ []string) ([]string, error) {
	if event.ChangeID == f.broken {
		return nil, errors.New("read workflow memory policy at base: not found")
	}
	return []string{"payments"}, nil
}

func TestBackfillOfOneBrokenChangeDoesNotKeepTheOthersOutOfMemory(t *testing.T) {
	good := eventForMemory(t, change.EventDecisionRecorded, change.ActorHuman, `{"kind":"uphold","target":"F1","login":"alice","reason":"verified","source_ref":"PR-3"}`)
	bad := good
	bad.ID, bad.ChangeID = "11111111-1111-4111-8111-111111111111", "OLD-1"
	store := &memoryHistory{events: map[id.ChangeID][]change.Event{"PAY-3": {good}, "OLD-1": {bad}}}
	queue := &memoryEpisodeQueue{}
	service, err := NewWorkflowMemory(queue, store, failingTeams{broken: "OLD-1"}, redactMemory{}, memoryClock{time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)}, emptyRooms{})
	if err != nil {
		t.Fatal(err)
	}
	stats, err := service.Backfill(context.Background())
	if err == nil || !strings.Contains(err.Error(), "OLD-1") {
		t.Fatalf("the broken change must still be reported, got %v", err)
	}
	if stats.Queued != 1 || len(queue.episodes) != 1 || queue.episodes[0].GroupID != "payments" {
		t.Fatalf("the healthy change must still be queued: %#v %#v", stats, queue.episodes)
	}
}
