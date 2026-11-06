package dashboard

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

type fakeSource struct {
	mu     sync.Mutex
	events map[id.ChangeID][]change.Event
}

func (f *fakeSource) ListChanges(context.Context) ([]id.ChangeID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []id.ChangeID
	for cid := range f.events {
		out = append(out, cid)
	}
	return out, nil
}

func (f *fakeSource) Load(_ context.Context, cid id.ChangeID) ([]change.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]change.Event(nil), f.events[cid]...), nil
}

func (f *fakeSource) add(e change.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events[e.ChangeID] = append(f.events[e.ChangeID], e)
}

func event(t *testing.T, version int64, typ change.EventType, p change.Payload) change.Event {
	t.Helper()
	raw, err := json.Marshal(p)
	require.NoError(t, err)
	return change.Event{ID: change.EventID("ev-" + string(typ) + "-" + string(rune('0'+version))), ChangeID: "PAY-9", Version: version, Type: typ, Payload: raw}
}

func newHandler(t *testing.T, src Source, interval time.Duration) *Handler {
	t.Helper()
	h, err := New(src, slog.New(slog.DiscardHandler), interval)
	require.NoError(t, err)
	return h
}

// A blocked change shows its blocker reason on the page, from its last gate result (A7).
func TestPageShowsABlockedChangeWithItsReason(t *testing.T) {
	src := &fakeSource{events: map[id.ChangeID][]change.Event{}}
	src.add(event(t, 1, change.EventChangeTriggered, change.Triggered{Ticket: "PAY-9", Repo: "acme/pay", Assignee: "ana", Source: change.TriggerJira}))
	src.add(event(t, 2, change.EventGateEvaluated, change.GateEvaluated{OK: false, Reasons: []string{"blocking: 1 finding is open: F1"}}))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:5555"
	rec := httptest.NewRecorder()
	newHandler(t, src, time.Hour).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "PAY-9")
	require.Contains(t, body, "blocked")
	require.Contains(t, body, "blocking: 1 finding is open: F1")
}

// A new event on a change reaches an open stream within one look (A7).
func TestStreamSendsAChangeWhenItsLogGrows(t *testing.T) {
	src := &fakeSource{events: map[id.ChangeID][]change.Event{}}
	src.add(event(t, 1, change.EventChangeTriggered, change.Triggered{Ticket: "PAY-9", Repo: "acme/pay", Assignee: "ana", Source: change.TriggerJira}))
	srv := httptest.NewServer(newHandler(t, src, 10*time.Millisecond))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/events", nil)
	require.NoError(t, err)
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	lines := bufio.NewScanner(res.Body)

	require.True(t, nextChangeEvent(lines, "PAY-9"), "the first look sends the change")
	src.add(event(t, 2, change.EventGateEvaluated, change.GateEvaluated{OK: true}))
	require.True(t, nextChangeEvent(lines, "PAY-9"), "the new event is sent without a reload")
}

// nextChangeEvent reads the stream until a change event for cid arrives or the context ends.
func nextChangeEvent(lines *bufio.Scanner, cid string) bool {
	for lines.Scan() {
		if line := lines.Text(); strings.HasPrefix(line, "data: ") && strings.TrimPrefix(line, "data: ") == cid {
			return true
		}
	}
	return false
}

// The dashboard is loopback only, the same rule as the listener: a remote caller is refused, not served.
func TestDashboardRefusesANonLoopbackCaller(t *testing.T) {
	src := &fakeSource{events: map[id.ChangeID][]change.Event{}}
	for _, path := range []string{"/", "/events"} {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
		req.RemoteAddr = "203.0.113.7:4444"
		rec := httptest.NewRecorder()
		newHandler(t, src, time.Hour).ServeHTTP(rec, req)
		require.Equal(t, http.StatusForbidden, rec.Code, path)
	}
}
