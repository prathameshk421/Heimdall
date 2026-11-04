package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/engine"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// The fixed values every fake uses. They are well-formed, so the events the fakes build pass the real validators.
const (
	ticket = id.ChangeID("PAY-142")
	repo   = change.Repo("aarush323/heimdall-fixture-pay")
	branch = "PAY-142-fix-refund"
	sha    = id.SHA("0123456789abcdef0123456789abcdef01234567")
	taskID = id.TaskID("tsk_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	runID  = id.RunID("run_01ARZ3NDEKTSV4RRFFQ69G5FAW")
	blob   = workflow.BlobRef("blob:sha256:" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
)

var (
	dayStart   = time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
	sysActor   = change.Actor{Kind: change.ActorSystem, ID: "heimdall"}
	humanActor = change.Actor{Kind: change.ActorHuman, ID: "ana"}
	agentActor = change.Actor{Kind: change.ActorAgent, ID: "test-author"}
	ghActor    = change.Actor{Kind: change.ActorGitHub, ID: "github"}
)

// fixedClock is the only clock the tests use; the engine itself reads no clock.
type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

var _ ports.Clock = fixedClock{}

// fakeStore keeps one change's events in memory. Append checks what the postgres store checks: the expected version,
// the event versions, that every event and effect validates, and that the hash chain links. A test that passes here
// would have passed the real store's checks too.
type fakeStore struct {
	events   []change.Event
	effects  []change.Effect
	appends  int
	conflict bool
}

var _ ports.EventStore = (*fakeStore)(nil)

func (s *fakeStore) Load(_ context.Context, _ id.ChangeID) ([]change.Event, error) {
	return slices.Clone(s.events), nil
}

func (s *fakeStore) Append(_ context.Context, cid id.ChangeID, expected int64, evs []change.Event, fx []change.Effect) (int64, error) {
	if s.conflict {
		return 0, fmt.Errorf("append %s: %w", cid, ports.ErrVersionConflict)
	}
	if head := int64(len(s.events)); head != expected {
		return 0, fmt.Errorf("append %s: change is at %d, not %d: %w", cid, head, expected, ports.ErrVersionConflict)
	}
	if len(evs) == 0 {
		return 0, errors.New("append: no events")
	}
	prev := change.Hash{}
	for i, e := range evs {
		if e.ChangeID != cid {
			return 0, fmt.Errorf("event %d is for change %s, not %s", i+1, e.ChangeID, cid)
		}
		if want := expected + int64(i) + 1; e.Version != want {
			return 0, fmt.Errorf("event %d has version %d, want %d", i+1, e.Version, want)
		}
		if err := e.Validate(); err != nil {
			return 0, fmt.Errorf("event %s: %w", e.Type, err)
		}
		if len(s.events) == 0 && i == 0 {
			e.PrevHash = nil
		} else {
			p := prev
			e.PrevHash = &p
		}
		h, err := change.Chain(prev, e)
		if err != nil {
			return 0, err
		}
		e.Hash = h
		prev = h
		s.events = append(s.events, e)
	}
	for _, f := range fx {
		if err := f.Validate(); err != nil {
			return 0, fmt.Errorf("effect %s: %w", f.Key, err)
		}
	}
	s.effects = append(s.effects, fx...)
	s.appends++
	return int64(len(s.events)), nil
}

// fakeStep is a step whose behaviour the test supplies. It counts how often the engine called it.
type fakeStep struct {
	typ     string
	done    func(c engine.ChangeView) bool
	start   func(ctx context.Context, c engine.ChangeView, with json.RawMessage) (engine.Result, error)
	timeout func(c engine.ChangeView) (engine.Result, error)
	starts  int
	offered []change.EventType
}

var _ engine.Step = (*fakeStep)(nil)

func (f *fakeStep) Type() string                  { return f.typ }
func (f *fakeStep) Done(c engine.ChangeView) bool { return f.done != nil && f.done(c) }
func (f *fakeStep) OnEvent(_ context.Context, _ engine.ChangeView, e change.Event) (engine.Result, error) {
	f.offered = append(f.offered, e.Type)
	return engine.Result{}, nil
}

func (f *fakeStep) Start(ctx context.Context, c engine.ChangeView, with json.RawMessage) (engine.Result, error) {
	f.starts++
	if f.start == nil {
		return engine.Result{}, nil
	}
	return f.start(ctx, c, with)
}

func (f *fakeStep) OnTimeout(_ context.Context, c engine.ChangeView) (engine.Result, error) {
	if f.timeout == nil {
		return engine.Result{}, errors.New("no timeout handler in this fake")
	}
	return f.timeout(c)
}

// harness builds events and the default workflow's fake steps. It holds the event counter, so it is per test.
type harness struct {
	t     *testing.T
	store *fakeStore
	clock fixedClock
	n     int
}

func newHarness(t *testing.T) *harness {
	return &harness{t: t, store: &fakeStore{}, clock: fixedClock{now: dayStart}}
}

// event builds an unnumbered event: the engine sets the version. The id is a counter, so runs are deterministic.
func (h *harness) event(typ change.EventType, actor change.Actor, p change.Payload) change.Event {
	h.t.Helper()
	raw, err := json.Marshal(p)
	require.NoError(h.t, err)
	h.n++
	return change.Event{
		ID:            change.EventID(fmt.Sprintf("00000000-0000-4000-8000-%012d", h.n)),
		ChangeID:      ticket,
		Type:          typ,
		Actor:         actor,
		OccurredAt:    h.clock.now,
		CorrelationID: change.CorrelationID("run-PAY-142"),
		Payload:       raw,
	}
}

// emit is a Result holding one event.
func (h *harness) emit(typ change.EventType, actor change.Actor, p change.Payload) engine.Result {
	return engine.Result{Events: []change.Event{h.event(typ, actor, p)}}
}

// incoming is one event that arrives from outside the engine, such as a GitHub webhook or an agent's submission.
func (h *harness) incoming(typ change.EventType, actor change.Actor, p change.Payload) []change.Event {
	return []change.Event{h.event(typ, actor, p)}
}

func (h *harness) triggered() []change.Event {
	return h.incoming(change.EventChangeTriggered, sysActor, change.Triggered{
		Ticket: ticket, Repo: repo, Assignee: "ana", Source: change.TriggerJira,
	})
}

// fake returns a step of the given type that is done when done holds and starts with start.
func fake(typ string, done func(c engine.ChangeView) bool, start func(ctx context.Context, c engine.ChangeView, with json.RawMessage) (engine.Result, error)) *fakeStep {
	return &fakeStep{typ: typ, done: done, start: start}
}

// hasOf returns a done function: the view holds an event of one of the types.
func hasOf(types ...change.EventType) func(c engine.ChangeView) bool {
	return func(c engine.ChangeView) bool { return c.Has(types...) }
}

// defaultSteps are fakes for the fourteen step types of TECH_SPEC 5.1. Each one emits the catalog event its real
// counterpart would, and each is done when that event (or the human or agent event that follows it) is in the view.
// mutation, verify and deliberate have no catalog event of their own in this build, so they are done with the review
// (see the package doc). Each Start emits only what the view lacks, so calling it again is harmless.
func (h *harness) defaultSteps() []engine.Step {
	return []engine.Step{
		fake("switch.open_room", hasOf(change.EventRoomOpened), func(ctx context.Context, c engine.ChangeView, _ json.RawMessage) (engine.Result, error) {
			if c.Has(change.EventRoomOpened) {
				return engine.Result{}, nil
			}
			return h.emit(change.EventRoomOpened, sysActor, change.RoomOpened{RoomID: "room-PAY-142"}), nil
		}),
		fake("github.ensure_branch", hasOf(change.EventBranchEnsured), func(ctx context.Context, c engine.ChangeView, _ json.RawMessage) (engine.Result, error) {
			if c.Has(change.EventBranchEnsured) {
				return engine.Result{}, nil
			}
			return h.emit(change.EventBranchEnsured, ghActor, change.BranchEnsured{Repo: repo, Branch: branch, BaseSHA: sha}), nil
		}),
		fake("agent.task", hasOf(change.EventTestsSubmitted), func(ctx context.Context, c engine.ChangeView, _ json.RawMessage) (engine.Result, error) {
			if c.Has(change.EventTaskAssigned) {
				return engine.Result{Timers: []engine.Timer{{Step: "write_tests", Due: dayStart.Add(2 * time.Hour)}}}, nil
			}
			res := h.emit(change.EventTaskAssigned, sysActor, change.TaskAssigned{
				TaskID: taskID, Agent: "test-author", Role: workflow.RoleTestAuthor,
				Deadline: dayStart.Add(2 * time.Hour), BundleRef: blob,
			})
			res.Timers = []engine.Timer{{Step: "write_tests", Due: dayStart.Add(2 * time.Hour)}}
			return res, nil
		}),
		fake("sandbox.test_quality", hasOf(change.EventTestsVerified), func(ctx context.Context, c engine.ChangeView, _ json.RawMessage) (engine.Result, error) {
			if c.Has(change.EventTestsVerified) || !c.Has(change.EventTestsSubmitted) {
				return engine.Result{}, nil
			}
			return h.emit(change.EventTestsVerified, sysActor, change.TestsVerified{
				RunIDs: []id.RunID{runID}, SHA: sha, TreeHash: sha, NeedsSignoff: false,
			}), nil
		}),
		fake("github.commit_tests", hasOf(change.EventTestsCommitted), func(ctx context.Context, c engine.ChangeView, _ json.RawMessage) (engine.Result, error) {
			if c.Has(change.EventTestsCommitted) {
				return engine.Result{}, nil
			}
			key, err := change.IdempotencyKey(ticket, int64(len(c.Events))+1, change.EffectGitHubBranch, "commit:"+string(sha))
			if err != nil {
				return engine.Result{}, err
			}
			res := h.emit(change.EventTestsCommitted, ghActor, change.TestsCommitted{SHA: sha, TreeHash: sha})
			res.Effects = []change.Effect{{
				Key: key, Kind: change.EffectGitHubBranch, Target: string(repo),
				Payload: json.RawMessage(`{"branch":"` + branch + `"}`),
			}}
			return res, nil
		}),
		fake("wait", hasOf(change.EventPROpened), nil),
		fake("rule.slop", hasOf(change.EventSlopEvaluated), func(ctx context.Context, c engine.ChangeView, _ json.RawMessage) (engine.Result, error) {
			if c.Has(change.EventSlopEvaluated) {
				return engine.Result{}, nil
			}
			return h.emit(change.EventSlopEvaluated, sysActor, change.SlopEvaluated{Pass: true}), nil
		}),
		h.pickStep(),
		fake("agent.fanout", hasOf(change.EventReviewSubmitted), func(ctx context.Context, c engine.ChangeView, _ json.RawMessage) (engine.Result, error) {
			return engine.Result{Timers: []engine.Timer{{Step: "review", Due: dayStart.Add(30 * time.Minute)}}}, nil
		}),
		fake("sandbox.verify_evidence", hasOf(change.EventReviewSubmitted), nil),
		fake("human.approve_tests", hasOf(change.EventTestsApproved), nil),
		fake("sandbox.mutation", hasOf(change.EventReviewSubmitted), nil),
		fake("deliberation.run", hasOf(change.EventReviewSubmitted), nil),
		fake("rule.merge_allowed", hasOf(change.EventGateEvaluated), func(ctx context.Context, c engine.ChangeView, _ json.RawMessage) (engine.Result, error) {
			return h.emit(change.EventGateEvaluated, sysActor, change.GateEvaluated{OK: true, SHA: sha, TreeHash: sha, PolicyVersion: 1}), nil
		}),
	}
}

// pickStep waits for reviewers.picked from a person; its timeout picks the default reviewers by timer.
func (h *harness) pickStep() *fakeStep {
	s := fake("human.pick_reviewers", hasOf(change.EventReviewersPicked), func(ctx context.Context, c engine.ChangeView, _ json.RawMessage) (engine.Result, error) {
		return engine.Result{Timers: []engine.Timer{{Step: "pick", Due: dayStart.Add(15 * time.Minute)}}}, nil
	})
	s.timeout = func(c engine.ChangeView) (engine.Result, error) {
		return h.emit(change.EventReviewersPicked, sysActor, change.ReviewersPicked{
			People: []change.ActorID{"ana"}, By: "heimdall", Method: change.PickTimer,
		}), nil
	}
	return s
}

// defaultWorkflow is the workflow the engine runs in these tests (TECH_SPEC 5.1, the Go value of T23).
func defaultWorkflow() workflow.Workflow { return workflow.Default() }

// roles and schemas are the registry the default workflow validates against.
var (
	registryRoles   = []workflow.Role{workflow.RoleTestAuthor, workflow.RoleReviewer, workflow.RoleResolver}
	registrySchemas = []string{"TestSubmission@1", "Review@1"}
)

// build makes an engine, failing the test when New refuses the config.
func (h *harness) build(cfg engine.Config) *engine.Engine {
	h.t.Helper()
	e, err := engine.New(cfg)
	require.NoError(h.t, err)
	return e
}

// defaultConfig is the default workflow on the fakes, with sensitive_domain false.
func (h *harness) defaultConfig(steps []engine.Step) engine.Config {
	return engine.Config{
		Workflow:   defaultWorkflow(),
		Steps:      steps,
		Store:      h.store,
		Roles:      registryRoles,
		Schemas:    registrySchemas,
		Conditions: map[string]engine.Condition{"sensitive_domain": func(engine.ChangeView) bool { return false }},
	}
}

// types lists the event types in the store, in version order.
func (h *harness) types() []change.EventType {
	out := make([]change.EventType, 0, len(h.store.events))
	for _, e := range h.store.events {
		out = append(out, e.Type)
	}
	return out
}

// has reports whether the store holds an event of the type.
func (h *harness) has(typ change.EventType) bool {
	return slices.Contains(h.types(), typ)
}

var _ = strings.TrimSpace
