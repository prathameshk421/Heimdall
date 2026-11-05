package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/engine"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/ids"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// fakeCursorTable is an in-memory cursor table. A source that was never set is ports.ErrNotFound, as in postgres.
type fakeCursorTable struct {
	values map[string]string
	err    error
	saved  []string
}

func (f *fakeCursorTable) GetCursor(_ context.Context, source string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	v, ok := f.values[source]
	if !ok {
		return "", fmt.Errorf("cursor %s: %w", source, ports.ErrNotFound)
	}
	return v, nil
}

func (f *fakeCursorTable) SetCursor(_ context.Context, source, value string) error {
	if f.values == nil {
		f.values = map[string]string{}
	}
	f.values[source] = value
	f.saved = append(f.saved, source+"="+value)
	return nil
}

func TestCursorStoreLoadMapsNotFoundToTheEmptyCursor(t *testing.T) {
	c := cursorStore{table: &fakeCursorTable{}, source: "jira:KAN"}
	cur, err := c.Load(context.Background())
	require.NoError(t, err, "a source with no cursor yet is the first poll, not a failure")
	require.Equal(t, ports.Cursor(""), cur)
}

func TestCursorStoreLoadReturnsTheSavedCursorForItsSource(t *testing.T) {
	table := &fakeCursorTable{values: map[string]string{"jira:KAN": "2026-11-05T10:00:00Z", "jira:OTHER": "other"}}
	c := cursorStore{table: table, source: "jira:KAN"}
	cur, err := c.Load(context.Background())
	require.NoError(t, err)
	require.Equal(t, ports.Cursor("2026-11-05T10:00:00Z"), cur)
}

func TestCursorStoreLoadKeepsOtherErrors(t *testing.T) {
	boom := errors.New("connection reset")
	c := cursorStore{table: &fakeCursorTable{err: boom}, source: "jira:KAN"}
	_, err := c.Load(context.Background())
	require.ErrorIs(t, err, boom)
	require.NotErrorIs(t, err, ports.ErrNotFound, "a database failure must not look like a first poll")
}

func TestCursorStoreSaveWritesUnderItsSource(t *testing.T) {
	table := &fakeCursorTable{}
	c := cursorStore{table: table, source: "jira:KAN"}
	require.NoError(t, c.Save(context.Background(), ports.Cursor("2026-11-05T10:05:00Z")))
	require.Equal(t, []string{"jira:KAN=2026-11-05T10:05:00Z"}, table.saved)
}

// memStore is an in-memory ports.EventStore with the optimistic version check of the Postgres adapter.
type memStore struct {
	mu     sync.Mutex
	events map[id.ChangeID][]change.Event
	fx     []change.Effect
}

func newMemStore() *memStore {
	return &memStore{events: map[id.ChangeID][]change.Event{}}
}

func (m *memStore) Append(_ context.Context, cid id.ChangeID, expected int64, evs []change.Event, fx []change.Effect) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if int64(len(m.events[cid])) != expected {
		return 0, fmt.Errorf("append to %s: %w", cid, ports.ErrVersionConflict)
	}
	m.events[cid] = append(m.events[cid], evs...)
	m.fx = append(m.fx, fx...)
	return int64(len(m.events[cid])), nil
}

func (m *memStore) Load(_ context.Context, cid id.ChangeID) ([]change.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.events[cid]), nil
}

// newSliceEngine builds the engine of the slice on store, as assemble does.
func newSliceEngine(t *testing.T, store ports.EventStore) *engine.Engine {
	t.Helper()
	wf, steps, err := sliceSteps()
	require.NoError(t, err)
	eng, err := engine.New(engine.Config{
		Workflow: wf, Steps: steps, Store: store, Clock: clock.System{}, IDs: ids.New(), MaxAttempts: maxAttempts,
	})
	require.NoError(t, err)
	return eng
}

// triggeredEvent is the change.triggered event the poller builds for a ticket.
func triggeredEvent(t *testing.T, key string) change.Event {
	t.Helper()
	gen := ids.New()
	raw, err := json.Marshal(change.Triggered{
		Ticket:   id.ChangeID(key),
		Repo:     change.Repo("aarush323/PBL_TOS_RISK_ANALYZER"),
		Assignee: change.ActorID("557058:ana"),
		Source:   change.TriggerJira,
	})
	require.NoError(t, err)
	eid := gen.NewEvent()
	ev := change.Event{
		ID:            eid,
		ChangeID:      id.ChangeID(key),
		Version:       1,
		Type:          change.EventChangeTriggered,
		Actor:         change.Actor{Kind: change.ActorJira, ID: "jira"},
		OccurredAt:    clock.System{}.Now().UTC(),
		CorrelationID: change.CorrelationID(eid.String()),
		Payload:       raw,
	}
	require.NoError(t, ev.Validate())
	return ev
}

func TestStarterStartsTheChangeAndCommitsItsRoomEffect(t *testing.T) {
	store := newMemStore()
	s := starter{eng: newSliceEngine(t, store)}
	require.NoError(t, s.Start(context.Background(), triggeredEvent(t, "KAN-1")))

	evs, err := store.Load(context.Background(), "KAN-1")
	require.NoError(t, err)
	require.Len(t, evs, 1, "the first event only: room.opened comes from the executor")
	require.Equal(t, change.EventChangeTriggered, evs[0].Type)
	require.Len(t, store.fx, 1)
	require.Equal(t, change.EffectRoomOpen, store.fx[0].Kind, "the switch.open_room step commits its effect with the trigger")
}

func TestStarterReturnsVersionConflictForAnExistingChange(t *testing.T) {
	store := newMemStore()
	s := starter{eng: newSliceEngine(t, store)}
	require.NoError(t, s.Start(context.Background(), triggeredEvent(t, "KAN-1")))

	err := s.Start(context.Background(), triggeredEvent(t, "KAN-1"))
	require.ErrorIs(t, err, ports.ErrVersionConflict, "the poller counts this as already triggered")
}
