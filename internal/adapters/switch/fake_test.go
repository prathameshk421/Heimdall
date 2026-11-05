package switchchat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

const (
	testAgentID     = "agent-coord-1"
	testAgentName   = "heimdall-coordinator"
	testRegToken    = "reg-token-value"
	testAPIKey      = "api-key-value"
	testRegSecret   = "switch_registration_token"
	testKeySecret   = "switch_api_key"
	testConnBridge  = "bridge-1"
	testHeartbeat   = 2
	testRoomUUID    = "3f1c2a9e-0000-4000-8000-000000000001"
	testOtherRoom   = "3f1c2a9e-0000-4000-8000-000000000002"
	testHumanSender = "@switch-mattermost-bridge-1-dev:localhost"
)

// fakeClock is a ports.Clock that only moves when a test moves it.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// fakeSecrets serves file-style secrets by name. A missing name is an error, as config.Config.Secret is.
type fakeSecrets map[string]string

func (s fakeSecrets) Secret(name string) (string, error) {
	v, ok := s[name]
	if !ok {
		return "", fmt.Errorf("secret %q: no such file", name)
	}
	return v, nil
}

// recorded is one request the fake Switch received.
type recorded struct {
	Method      string
	Path        string
	Query       string
	Auth        string
	Accept      string
	LastEventID string
	Connection  string
	Body        map[string]any
}

// fakeEntry is one read_context entry, or a stream message, in the fake's history.
type fakeEntry struct {
	ID     string
	Room   string
	Sender string
	Body   string
	TSms   int64
}

// fakeSwitch is an httptest server that speaks the subset of Switch's agent bridge the adapter uses. Tests set the
// hooks; the defaults answer what a healthy coordinator sees.
type fakeSwitch struct {
	t   *testing.T
	srv *httptest.Server

	mu       sync.Mutex
	reqs     []recorded
	rooms    []map[string]any
	history  []fakeEntry
	sent     []map[string]any
	beatCode int
	// messageCode is the status the message route answers with. Tests set it to exercise the failure paths.
	messageCode int
	beatCh      chan struct{}
	streamFn    func(w http.ResponseWriter, r *http.Request, n int)
	ops         map[string]func(args map[string]any) (int, any)
	pages       func(args map[string]any) (int, any)
	opens       int
	beats       int
	gens        []int64
	nextRoom    int
}

func newFakeSwitch(t *testing.T) *fakeSwitch {
	t.Helper()
	f := &fakeSwitch{t: t, ops: map[string]func(map[string]any) (int, any){}, beatCh: make(chan struct{}, 1000)}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	f.installDefaults()
	return f
}

func (f *fakeSwitch) installDefaults() {
	f.ops["list_managed_agents"] = func(map[string]any) (int, any) { return http.StatusOK, []any{} }
	f.ops["list_rooms"] = func(map[string]any) (int, any) {
		f.mu.Lock()
		defer f.mu.Unlock()
		out := make([]any, 0, len(f.rooms))
		for _, r := range f.rooms {
			out = append(out, r)
		}
		return http.StatusOK, out
	}
	f.ops["create_room"] = func(args map[string]any) (int, any) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.nextRoom++
		id := fmt.Sprintf("3f1c2a9e-0000-4000-8000-%012d", 100+f.nextRoom)
		f.rooms = append(f.rooms, map[string]any{"room_id": id, "name": args["name"], "archived": false})
		return http.StatusOK, map[string]any{"id": id, "name": args["name"], "caller_is_member": true}
	}
	f.ops["invite_agent_to_room"] = func(map[string]any) (int, any) { return http.StatusOK, map[string]any{"ok": true} }
	f.ops["attach_reference_to_room"] = func(map[string]any) (int, any) {
		return http.StatusOK, map[string]any{"ok": true}
	}
	f.ops["create_reference"] = func(map[string]any) (int, any) {
		return http.StatusOK, map[string]any{"id": "ref-1", "name": "PAY-142"}
	}
	f.ops["read_context"] = func(args map[string]any) (int, any) { return http.StatusOK, f.readContext(args) }
	f.beatCode = http.StatusOK
	f.messageCode = http.StatusOK
}

// readContext answers from history: entries in the room at or after since, oldest first, one thread each.
func (f *fakeSwitch) readContext(args map[string]any) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	room, _ := args["room_id"].(string)
	var since int64 = -1
	if s, ok := args["since"].(string); ok && s != "" {
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			f.t.Errorf("since is not RFC 3339: %q", s)
		}
		since = t.UnixMilli()
	}
	var threads []any
	for _, e := range f.history {
		if e.Room != room || (since >= 0 && e.TSms < since) {
			continue
		}
		threads = append(threads, map[string]any{
			"root": map[string]any{"id": e.ID, "kind": "message", "sender": e.Sender, "sender_name": "dev",
				"body": e.Body, "timestamp": e.TSms, "attachments": []any{}},
			"replies": []any{},
		})
	}
	return map[string]any{"threads": threads, "truncated": false, "oldest_timestamp": nil}
}

// serve records the request, then answers it by route.
func (f *fakeSwitch) serve(w http.ResponseWriter, r *http.Request) {
	rec := f.recordRequest(r)
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/agents/register-known":
		f.handleRegister(w, rec)
	case !f.authorised(rec.Auth):
		writeJSON(w, http.StatusUnauthorized, map[string]any{"detail": "bad key"})
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/agents/"+testAgentID+"/ops/"):
		f.handleOp(w, r, rec)
	case r.Method == http.MethodPost && r.URL.Path == "/agents/"+testAgentID+"/message":
		f.handleMessage(w, rec)
	case r.Method == http.MethodPost && r.URL.Path == "/agents/"+testAgentID+"/connection/beat":
		f.handleBeat(w, rec)
	case r.Method == http.MethodGet && r.URL.Path == "/agents/"+testAgentID+"/events":
		f.handleEvents(w, r)
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"detail": "no route " + r.URL.Path})
	}
}

// recordRequest keeps what the request carried, the JSON body included, and returns it.
func (f *fakeSwitch) recordRequest(r *http.Request) recorded {
	raw, _ := io.ReadAll(r.Body)
	rec := recorded{
		Method:      r.Method,
		Path:        r.URL.Path,
		Query:       r.URL.RawQuery,
		Auth:        strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "),
		Accept:      r.Header.Get("Accept"),
		LastEventID: r.Header.Get("Last-Event-ID"),
		Connection:  r.Header.Get("X-Switch-Connection-Id"),
	}
	if len(raw) > 0 {
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err == nil {
			rec.Body = body
		}
	}
	f.mu.Lock()
	f.reqs = append(f.reqs, rec)
	f.mu.Unlock()
	return rec
}

func (f *fakeSwitch) handleRegister(w http.ResponseWriter, rec recorded) {
	if rec.Auth != testRegToken {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"detail": "bad token"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": testAgentID, "api_key": testAPIKey})
}

func (f *fakeSwitch) handleOp(w http.ResponseWriter, r *http.Request, rec recorded) {
	name := strings.TrimPrefix(r.URL.Path, "/agents/"+testAgentID+"/ops/")
	hook, ok := f.ops[name]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"detail": "unknown operation"})
		return
	}
	if name == "read_context" && f.pages != nil {
		code, out := f.pages(rec.Body)
		writeJSON(w, code, map[string]any{"result": out})
		return
	}
	code, out := hook(rec.Body)
	writeJSON(w, code, map[string]any{"result": out})
}

func (f *fakeSwitch) handleMessage(w http.ResponseWriter, rec recorded) {
	f.mu.Lock()
	f.sent = append(f.sent, rec.Body)
	code := f.messageCode
	f.mu.Unlock()
	writeJSON(w, code, map[string]any{"ok": code == http.StatusOK, "event_id": "evt-1"})
}

func (f *fakeSwitch) handleBeat(w http.ResponseWriter, rec recorded) {
	f.mu.Lock()
	f.beats++
	if g, ok := rec.Body["generation"].(float64); ok {
		f.gens = append(f.gens, int64(g))
	}
	code := f.beatCode
	f.mu.Unlock()
	select {
	case f.beatCh <- struct{}{}:
	default:
	}
	writeJSON(w, code, map[string]any{"ok": code == http.StatusOK})
}

func (f *fakeSwitch) handleEvents(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.opens++
	n := f.opens
	hook := f.streamFn
	f.mu.Unlock()
	if hook == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"detail": "no stream scripted"})
		return
	}
	hook(w, r, n)
}

func (f *fakeSwitch) authorised(key string) bool { return key == testAPIKey }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// frame writes one SSE frame. A zero id writes no id line, as control frames have none.
func frame(w http.ResponseWriter, id int64, event string, data any) {
	raw, _ := json.Marshal(data)
	if id > 0 {
		_, _ = fmt.Fprintf(w, "id: %d\n", id)
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw)
	if fl, ok := w.(http.Flusher); ok {
		fl.Flush()
	}
}

// connectionState is the first frame of every stream, carrying the generation the beats must echo.
func connectionState(connID string, gen int64, rooms ...string) map[string]any {
	return map[string]any{
		"connection_id": connID, "agent_id": testAgentID, "generation": gen,
		"scope": "all", "filter": "all", "rooms": rooms, "cursor": 0,
		"protocol": 9, "heartbeat_interval_seconds": testHeartbeat,
	}
}

// messageFrame is a room message as the stream sends it.
func messageFrame(room, msgID, sender, body string, tsMS int64) map[string]any {
	return map[string]any{
		"type": "message", "room_id": room, "bridge_id": testConnBridge, "channel_type": "channel_public",
		"payload": map[string]any{
			"addressed": false, "sender": sender, "sender_name": "dev", "sender_kind": "user",
			"message_id": msgID, "body": body, "timestamp": tsMS, "thread_id": nil, "attachments": []any{},
		},
	}
}

// requests returns a copy of what the fake received, safe to read while a stream is running.
func (f *fakeSwitch) requests() []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recorded(nil), f.reqs...)
}

func (f *fakeSwitch) sentMessages() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.sent...)
}

func (f *fakeSwitch) opCalls(name string) []map[string]any {
	var out []map[string]any
	for _, r := range f.requests() {
		if r.Method == http.MethodPost && r.Path == "/agents/"+testAgentID+"/ops/"+name {
			out = append(out, r.Body)
		}
	}
	return out
}

// newTestClient builds a client against the fake with a fake clock and a Wait that returns at once.
func newTestClient(t *testing.T, f *fakeSwitch) (*Client, *fakeClock) {
	t.Helper()
	clock := &fakeClock{now: time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)}
	c, err := New(Config{
		BaseURL:            f.srv.URL,
		AgentID:            testAgentID,
		AgentName:          testAgentName,
		Secrets:            fakeSecrets{testKeySecret: testAPIKey, testRegSecret: testRegToken},
		APIKeySecret:       testKeySecret,
		RegistrationSecret: testRegSecret,
		HTTPClient:         f.srv.Client(),
		Clock:              clock,
		Wait:               immediateWait,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, clock
}

// immediateWait returns at once unless the context is done. Tests never sleep.
func immediateWait(ctx context.Context, _ time.Duration) error {
	return ctx.Err()
}

func roomID(t *testing.T, s string) change.RoomID {
	t.Helper()
	r, err := change.ParseRoomID(s)
	if err != nil {
		t.Fatalf("room id %q: %v", s, err)
	}
	return r
}

func atoi(t *testing.T, s string) int64 {
	t.Helper()
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatalf("not an integer %q: %v", s, err)
	}
	return n
}

var errTestStop = errors.New("test stop")
