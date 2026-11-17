package reconcile

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/liveness"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// memStore is an in-memory event store, outbox and open-changes list. beforeAppend, when set, runs once before the next
// Append checks the version, so a test can make another writer win.
type memStore struct {
	mu           sync.Mutex
	events       map[id.ChangeID][]change.Event
	effects      map[change.EffectKey]change.Effect
	order        []change.EffectKey
	loads        map[id.ChangeID]int
	dead         map[id.ChangeID][]change.DeadJob
	beforeAppend func()
}

func newMemStore() *memStore {
	return &memStore{events: map[id.ChangeID][]change.Event{}, effects: map[change.EffectKey]change.Effect{},
		loads: map[id.ChangeID]int{}, dead: map[id.ChangeID][]change.DeadJob{}}
}

func (s *memStore) Load(_ context.Context, c id.ChangeID) ([]change.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loads[c]++
	return append([]change.Event(nil), s.events[c]...), nil
}

func (s *memStore) Append(_ context.Context, c id.ChangeID, expected int64, evs []change.Event, fx []change.Effect) (int64, error) {
	if hook := s.takeHook(); hook != nil {
		hook()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if int64(len(s.events[c])) != expected {
		return 0, fmt.Errorf("at %d, not %d: %w", len(s.events[c]), expected, ports.ErrVersionConflict)
	}
	for i, e := range evs {
		if e.Version != expected+int64(i)+1 || e.ChangeID != c {
			return 0, fmt.Errorf("bad numbering at %d", i)
		}
		if err := e.Validate(); err != nil {
			return 0, err
		}
	}
	s.events[c] = append(s.events[c], evs...)
	return int64(len(s.events[c])), s.putEffects(fx)
}

func (s *memStore) takeHook() func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := s.beforeAppend
	s.beforeAppend = nil
	return h
}

func (s *memStore) putEffects(fx []change.Effect) error {
	for _, f := range fx {
		if err := f.Validate(); err != nil {
			return err
		}
		if old, ok := s.effects[f.Key]; ok {
			if old.Kind != f.Kind || old.Target != f.Target || string(old.Payload) != string(f.Payload) {
				return fmt.Errorf("key %s with other content", f.Key)
			}
			continue
		}
		s.effects[f.Key] = f
		s.order = append(s.order, f.Key)
	}
	return nil
}

func (s *memStore) EnsureEffects(_ context.Context, c id.ChangeID, fx []change.Effect) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.events[c]) == 0 {
		return fmt.Errorf("no events")
	}
	return s.putEffects(fx)
}

func (s *memStore) OpenChanges(context.Context) ([]id.ChangeID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []id.ChangeID
	for c := range s.events {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

func (s *memStore) DeadEffects(_ context.Context, c id.ChangeID) ([]change.DeadJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dead[c], nil
}

func (s *memStore) keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.order))
	for _, k := range s.order {
		out = append(out, string(k))
	}
	return out
}

func (s *memStore) types(c id.ChangeID) []change.EventType {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []change.EventType
	for _, e := range s.events[c] {
		out = append(out, e.Type)
	}
	return out
}

func (s *memStore) count(c id.ChangeID, t change.EventType) int {
	n := 0
	for _, x := range s.types(c) {
		if x == t {
			n++
		}
	}
	return n
}

func (s *memStore) payloads(t *testing.T, c id.ChangeID, typ change.EventType) []change.Payload {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []change.Payload
	for _, e := range s.events[c] {
		if e.Type == typ {
			p, err := change.Decode(e)
			require.NoError(t, err)
			out = append(out, p)
		}
	}
	return out
}

type fakeInputs struct{}

func (fakeInputs) Policy(context.Context, change.Repo, id.SHA) (policy.Policy, error) {
	return policy.Policy{People: map[string]policy.Person{"ann": {GitHub: "ann"}}}, nil
}
func (fakeInputs) Outside(context.Context, change.View) (change.Outside, error) {
	return change.Outside{}, nil
}
func (fakeInputs) Profile(context.Context, change.Repo) (change.Profile, error) {
	return change.Profile{}, nil
}
func (fakeInputs) Bundle(context.Context, string, id.ChangeID, []change.Event) ([]byte, error) {
	return []byte(`{"bundle":true}`), nil
}

type fakeCalls struct{ calls []ports.ToolCall }

func (f fakeCalls) RecordCall(context.Context, ports.ToolCall) error { return nil }
func (f fakeCalls) RecentCalls(context.Context, id.AgentName, int) ([]ports.ToolCall, error) {
	return f.calls, nil
}
func (f fakeCalls) TicketCalls(context.Context, id.ChangeID, int) ([]ports.ToolCall, error) {
	return f.calls, nil
}

type fakeHealth struct {
	health liveness.Health
	probes int
}

func (f *fakeHealth) Probe(context.Context) (liveness.Health, error) {
	f.probes++
	return f.health, nil
}

type fakeIDs struct {
	mu sync.Mutex
	n  int
}

func (f *fakeIDs) next() int { f.mu.Lock(); defer f.mu.Unlock(); f.n++; return f.n }
func (f *fakeIDs) NewEvent() change.EventID {
	return change.EventID(fmt.Sprintf("3f1c9a52-8d2b-4e7a-9c11-%012d", f.next()))
}
func (f *fakeIDs) NewTask() id.TaskID {
	return id.TaskID(fmt.Sprintf("tsk_01HZX3K9QW7M2P5R8T0V4Y%04d", f.next()))
}
func (f *fakeIDs) NewFinding() id.FindingID { return "fnd_01HZX3K9QW7M2P5R8T0V4YB6CD" }
func (f *fakeIDs) NewRun() id.RunID         { return "run_01HZX3K9QW7M2P5R8T0V4YB6CD" }
func (f *fakeIDs) NewCase() id.CaseID       { return "case_01HZX3K9QW7M2P5R8T0V4YB6CD" }

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time  { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fakeClock) set(t time.Time) { c.mu.Lock(); defer c.mu.Unlock(); c.now = t }

var t0 = time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)

// rig is a reconciler on fakes.
type rig struct {
	store  *memStore
	clock  *fakeClock
	health *fakeHealth
	calls  *fakeCalls
	rec    *Reconciler
	seq    int
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{store: newMemStore(), clock: &fakeClock{now: t0}, health: &fakeHealth{health: liveness.Healthy}, calls: &fakeCalls{}}
	rec, err := New(Config{Store: r.store, Effects: r.store, Open: r.store, Dead: r.store, Inputs: fakeInputs{}, Calls: r.calls,
		Health: r.health, IDs: &fakeIDs{}, Clock: r.clock, Logger: slog.New(slog.DiscardHandler),
		Agents: func(kind, subject string) (id.AgentName, bool) {
			if kind == "tests" {
				return "test-author", true
			}
			return id.AgentName(subject), true
		},
		Settings: change.Settings{TestAttempts: 2, PickWait: time.Minute, Reviewers: []change.ActorID{"rev-a"},
			Timeouts: map[string]time.Duration{"tests": 2 * time.Hour, "review": 30 * time.Minute, "verdict": 30 * time.Minute}},
		Windows: liveness.Windows{Ack: 3 * time.Minute, Stall: 10 * time.Minute},
		Wait:    func(context.Context, time.Duration) error { return context.Canceled }})
	require.NoError(t, err)
	r.rec = rec
	return r
}

// add appends events to a change as another writer would, at the time of the clock.
func (r *rig) add(t *testing.T, c id.ChangeID, typ change.EventType, payload any, blobs ...string) {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	r.seq++
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	n := len(r.store.events[c]) + 1
	e := change.Event{ID: change.EventID(fmt.Sprintf("00000000-0000-4000-8000-%012d", r.seq)), ChangeID: c, Version: int64(n), Type: typ,
		Actor: change.Actor{Kind: change.ActorSystem, ID: "test"}, OccurredAt: r.clock.Now(), CorrelationID: "corr", Payload: raw}
	for _, b := range blobs {
		e.BlobRefs = append(e.BlobRefs, workflow.BlobRef("blob:sha256:"+b))
	}
	r.store.events[c] = append(r.store.events[c], e)
}

func (r *rig) run(t *testing.T, c id.ChangeID) {
	t.Helper()
	require.NoError(t, r.rec.Run(context.Background(), c))
}
