// Package changelog builds event logs for tests. It writes valid envelopes and payloads in version order, and it
// holds an in-memory History for the read side. TEST-ONLY (D121): only test files and the test kit import it.
package changelog

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// Human returns a human actor named name.
func Human(name string) change.Actor {
	return change.Actor{Kind: change.ActorHuman, ID: change.ActorID(name)}
}

// Agent returns an agent actor named name.
func Agent(name string) change.Actor {
	return change.Actor{Kind: change.ActorAgent, ID: change.ActorID(name)}
}

// Log is the event log of one change, built in version order.
type Log struct {
	t      testing.TB
	cid    id.ChangeID
	events []change.Event
}

// New starts an empty log for change cid.
func New(t testing.TB, cid id.ChangeID) *Log {
	return &Log{t: t, cid: cid}
}

// Add appends an event of type typ with payload p. It requires p to validate, so a fixture cannot drift from the
// catalog.
func (l *Log) Add(typ change.EventType, actor change.Actor, p change.Payload) *Log {
	l.t.Helper()
	require.NoError(l.t, p.Validate(), "payload of %s", typ)
	raw, err := json.Marshal(p)
	require.NoError(l.t, err)
	return l.Raw(typ, actor, raw)
}

// Raw appends an event whose payload is not checked. It builds the logs that cannot be read.
func (l *Log) Raw(typ change.EventType, actor change.Actor, raw json.RawMessage) *Log {
	v := int64(len(l.events) + 1)
	l.events = append(l.events, change.Event{
		ID:         change.EventID(fmt.Sprintf("00000000-0000-4000-8000-%012d", v)),
		ChangeID:   l.cid,
		Version:    v,
		Type:       typ,
		Actor:      actor,
		OccurredAt: occurredAt(v),
		Payload:    raw,
	})
	return l
}

// Events returns a copy of the events in version order.
func (l *Log) Events() []change.Event {
	return slices.Clone(l.events)
}

// occurredAt is the time of version v: one minute per version after a fixed start, so fixtures are reproducible.
func occurredAt(v int64) time.Time {
	return time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC).Add(time.Duration(v) * time.Minute)
}

// History is an in-memory read side of the event log. It has the methods that app/dashboard reads.
type History struct {
	mu   sync.Mutex
	logs map[id.ChangeID][]change.Event
}

// NewHistory returns an empty History.
func NewHistory() *History {
	return &History{logs: make(map[id.ChangeID][]change.Event)}
}

// Put stores the whole log of one change, replacing any earlier log of it.
func (h *History) Put(cid id.ChangeID, events []change.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.logs[cid] = slices.Clone(events)
}

// Append adds e to the end of the log of cid.
func (h *History) Append(cid id.ChangeID, e change.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.logs[cid] = append(h.logs[cid], e)
}

// ChangeIDs lists the changes that have events, in order.
func (h *History) ChangeIDs(_ context.Context) ([]id.ChangeID, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ids := make([]id.ChangeID, 0, len(h.logs))
	for cid, evs := range h.logs {
		if len(evs) > 0 {
			ids = append(ids, cid)
		}
	}
	slices.Sort(ids)
	return ids, nil
}

// Load returns a copy of the events of cid in version order. An unknown change has no events.
func (h *History) Load(_ context.Context, cid id.ChangeID) ([]change.Event, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.logs[cid]), nil
}
