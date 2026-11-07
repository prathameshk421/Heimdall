package agenttools_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/agenttools"
	appreview "github.com/prathameshk421/flip_the_switch_hackathon/internal/app/review"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/deliberation"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testset"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

var (
	testChangeID  = id.ChangeID("PAY-142")
	testTaskID    = id.TaskID("tsk_01J8Z3K9QW7M2P5R8T0V4YB6CD")
	testAgent     = id.AgentName("bob")
	testBaseSHA   = id.SHA("a01b2c3d4e5f60718293a4b5c6d7e8f901234567")
	testHeadSHA   = id.SHA("9f1c2b3a4e5d60718293a4b5c6d7e8f901234567")
	testBundleRef = workflow.BlobRef("blob:sha256:" + strings.Repeat("a", 64))
	testCaseID    = id.CaseID("case_01J8Z3K9QW7M2P5R8T0V4YB6CD")
	t0            = time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
)

// --- In-memory Fakes ---

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock(start time.Time) *fakeClock {
	return &fakeClock{now: start}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type fakeIDs struct {
	mu      sync.Mutex
	counter int
}

func (f *fakeIDs) next(prefix string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counter++
	return fmt.Sprintf("%s_%026d", prefix, f.counter)
}

func (f *fakeIDs) NewFinding() id.FindingID {
	return id.FindingID(f.next("fnd"))
}

func (f *fakeIDs) NewRun() id.RunID {
	return id.RunID(f.next("run"))
}

func (f *fakeIDs) NewCase() id.CaseID {
	return id.CaseID(f.next("case"))
}

func (f *fakeIDs) NewTask() id.TaskID {
	return id.TaskID(f.next("tsk"))
}

func (f *fakeIDs) NewEvent() change.EventID {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counter++
	return change.EventID(fmt.Sprintf("%08x-0000-4000-8000-%012x", f.counter, f.counter))
}

type fakeTaskIndex struct {
	mu    sync.Mutex
	tasks map[string]ports.ActiveTaskRef
}

func newFakeTaskIndex() *fakeTaskIndex {
	return &fakeTaskIndex{tasks: make(map[string]ports.ActiveTaskRef)}
}

func (f *fakeTaskIndex) set(agent id.AgentName, role workflow.Role, ref ports.ActiveTaskRef) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tasks[string(agent)+":"+string(role)] = ref
}

func (f *fakeTaskIndex) ActiveTask(_ context.Context, agent id.AgentName, role workflow.Role) (ports.ActiveTaskRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ref, ok := f.tasks[string(agent)+":"+string(role)]
	if !ok {
		return ports.ActiveTaskRef{}, ports.ErrNotFound
	}
	return ref, nil
}

type fakeBlobStore struct {
	mu    sync.Mutex
	blobs map[workflow.BlobRef][]byte
}

func newFakeBlobStore() *fakeBlobStore {
	return &fakeBlobStore{blobs: make(map[workflow.BlobRef][]byte)}
}

func (f *fakeBlobStore) Put(_ context.Context, _ ports.BlobClass, r io.Reader) (workflow.BlobRef, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	ref := workflow.BlobRef("blob:sha256:" + hex.EncodeToString(sum[:]))
	f.mu.Lock()
	f.blobs[ref] = data
	f.mu.Unlock()
	return ref, nil
}

func (f *fakeBlobStore) Get(_ context.Context, ref workflow.BlobRef) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.blobs[ref]
	if !ok {
		return nil, ports.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

type fakeEventStore struct {
	mu           sync.Mutex
	streams      map[id.ChangeID][]change.Event
	conflictNext int
}

func newFakeEventStore() *fakeEventStore {
	return &fakeEventStore{streams: make(map[id.ChangeID][]change.Event)}
}

func (f *fakeEventStore) Load(_ context.Context, cid id.ChangeID) ([]change.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	src := f.streams[cid]
	out := make([]change.Event, len(src))
	copy(out, src)
	return out, nil
}

func (f *fakeEventStore) Append(_ context.Context, cid id.ChangeID, expected int64, evs []change.Event, _ []change.Effect) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.conflictNext > 0 {
		f.conflictNext--
		return 0, ports.ErrVersionConflict
	}
	cur := f.streams[cid]
	if int64(len(cur)) != expected {
		return 0, ports.ErrVersionConflict
	}
	f.streams[cid] = append(cur, evs...)
	return int64(len(f.streams[cid])), nil
}

type fakeAsker struct {
	mu       sync.Mutex
	calls    []string
	senderID change.SenderID
	msgID    change.MessageID
	err      error
}

func (f *fakeAsker) Ask(_ context.Context, _ id.ChangeID, question string) (change.QuestionID, change.SenderID, change.MessageID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, question)
	if f.err != nil {
		return "", "", "", f.err
	}
	return change.QuestionID("q-1"), f.senderID, f.msgID, nil
}

// --- Test Helper Setup ---

type testHarness struct {
	svc    *agenttools.Service
	events *fakeEventStore
	index  *fakeTaskIndex
	blobs  *fakeBlobStore
	clock  *fakeClock
	ids    *fakeIDs
	asker  *fakeAsker
}

type fakeReviewRefs struct{}

type semanticReviewValidator struct{ refs appreview.Refs }

func (v semanticReviewValidator) Submit(ctx context.Context, sub agenttools.ReviewSubmission) (agenttools.ReviewResult, error) {
	got, err := appreview.Submit(ctx, v.refs, appreview.Submission{Change: sub.Change, Caller: sub.Caller, FilesChanged: sub.FilesChanged, Review: sub.Review})
	if err != nil {
		return agenttools.ReviewResult{}, err
	}
	out := agenttools.ReviewResult{}
	for _, problem := range got.Problems {
		out.Problems = append(out.Problems, agenttools.ReviewProblem{Message: problem.Message})
	}
	for _, filed := range got.Findings {
		out.Findings = append(out.Findings, agenttools.ReviewFinding{Input: filed.Input, Tier: filed.Tier})
	}
	return out, nil
}

func (fakeReviewRefs) FileExists(_ context.Context, _ workflow.ChangeRef, path string) (bool, error) {
	return path == "internal/refunds/refunds.go", nil
}
func (fakeReviewRefs) LineCount(_ context.Context, _ workflow.ChangeRef, path string) (int, error) {
	if path == "internal/refunds/refunds.go" {
		return 100, nil
	}
	return 0, nil
}
func (fakeReviewRefs) RunExists(_ context.Context, _ workflow.ChangeRef, _ id.AgentName, tool string) (bool, error) {
	return tool == "go vet", nil
}
func (fakeReviewRefs) MemoryExists(_ context.Context, _ workflow.ChangeRef, _ id.AgentName, ref string) (bool, error) {
	return ref == "decision:refund-limit", nil
}
func (fakeReviewRefs) PackExists(context.Context, workflow.ChangeRef, id.AgentName, string) (bool, error) {
	return false, nil
}

func newTestHarness(t *testing.T) *testHarness {
	t.Helper()
	events := newFakeEventStore()
	index := newFakeTaskIndex()
	blobs := newFakeBlobStore()
	clock := newFakeClock(t0)
	ids := &fakeIDs{}
	asker := &fakeAsker{
		senderID: change.SenderID("sender-switch"),
		msgID:    change.MessageID("msg-123"),
	}

	refs := fakeReviewRefs{}
	svc, err := agenttools.New(agenttools.Config{
		Events:     events,
		Index:      index,
		Blobs:      blobs,
		Clock:      clock,
		IDs:        ids,
		Asker:      asker,
		ReviewRefs: refs, ReviewValidator: semanticReviewValidator{refs: refs},
	})
	require.NoError(t, err)

	return &testHarness{
		svc:    svc,
		events: events,
		index:  index,
		blobs:  blobs,
		clock:  clock,
		ids:    ids,
		asker:  asker,
	}
}

func makeBaseStream(t *testing.T, h *testHarness, role workflow.Role, agent id.AgentName) {
	t.Helper()
	now := t0
	firstID := h.ids.NewEvent()
	corr := change.CorrelationID(firstID.String())

	trigPayload, _ := json.Marshal(change.Triggered{
		Ticket: testChangeID,
		Repo:   "acme/pay-demo",
	})
	e1 := change.Event{
		ID:            firstID,
		ChangeID:      testChangeID,
		Version:       1,
		Type:          change.EventChangeTriggered,
		Actor:         change.Actor{Kind: change.ActorSystem, ID: "trigger"},
		OccurredAt:    now,
		CorrelationID: corr,
		Payload:       trigPayload,
	}

	roomPayload, _ := json.Marshal(change.RoomOpened{
		RoomID: "room-pay-142",
	})
	e2 := change.Event{
		ID:            h.ids.NewEvent(),
		ChangeID:      testChangeID,
		Version:       2,
		Type:          change.EventRoomOpened,
		Actor:         change.Actor{Kind: change.ActorSystem, ID: "engine"},
		OccurredAt:    now.Add(time.Second),
		CorrelationID: corr,
		CausationID:   &e1.ID,
		Payload:       roomPayload,
	}

	taskPayload, _ := json.Marshal(change.TaskAssigned{
		TaskID:    testTaskID,
		CaseID:    testCaseID,
		Agent:     agent,
		Role:      role,
		Deadline:  now.Add(30 * time.Minute),
		BundleRef: testBundleRef,
	})
	e3 := change.Event{
		ID:            h.ids.NewEvent(),
		ChangeID:      testChangeID,
		Version:       3,
		Type:          change.EventTaskAssigned,
		Actor:         change.Actor{Kind: change.ActorSystem, ID: "engine"},
		OccurredAt:    now.Add(2 * time.Second),
		CorrelationID: corr,
		CausationID:   &e2.ID,
		BlobRefs:      []workflow.BlobRef{testBundleRef},
		Payload:       taskPayload,
	}

	evs := []change.Event{e1, e2, e3}
	_, err := h.events.Append(context.Background(), testChangeID, 0, evs, nil)
	require.NoError(t, err)
}

// --- Tests ---

func TestNewValidatesDependencies(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)

	_, err := agenttools.New(agenttools.Config{})
	require.Error(t, err)
	for _, dep := range []string{"Events", "Index", "Blobs", "Clock", "IDs", "Asker"} {
		require.Contains(t, err.Error(), dep)
	}

	// Missing only one
	cfg := agenttools.Config{
		Events: h.events,
		Index:  h.index,
		Blobs:  h.blobs,
		Clock:  h.clock,
		IDs:    h.ids,
		Asker:  nil,
	}
	_, err = agenttools.New(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Asker")
}

func TestCurrentHappyPath(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeBaseStream(t, h, workflow.RoleTestAuthor, testAgent)

	h.index.set(testAgent, workflow.RoleTestAuthor, ports.ActiveTaskRef{
		ChangeID: testChangeID,
		TaskID:   testTaskID,
	})

	assign, err := h.svc.Current(context.Background(), testAgent, workflow.RoleTestAuthor)
	require.NoError(t, err)
	require.Equal(t, testChangeID, assign.ChangeID)
	require.Equal(t, testTaskID, assign.TaskID)
	require.Equal(t, testAgent, assign.Agent)
	require.Equal(t, workflow.RoleTestAuthor, assign.Role)
	require.Equal(t, testBundleRef, assign.BundleRef)
	require.False(t, assign.Acked)
	require.Equal(t, t0.Add(2*time.Second), assign.AssignedAt)
	require.Equal(t, t0.Add(30*time.Minute), assign.Deadline)
}

func TestCurrentNoActiveTask(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)

	_, err := h.svc.Current(context.Background(), testAgent, workflow.RoleTestAuthor)
	require.ErrorIs(t, err, agenttools.ErrNoTask)
}

func TestCurrentWrongAgentOrRole(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeBaseStream(t, h, workflow.RoleTestAuthor, testAgent)

	// Index points to task assigned to bob, but called for alice
	h.index.set(id.AgentName("alice"), workflow.RoleTestAuthor, ports.ActiveTaskRef{
		ChangeID: testChangeID,
		TaskID:   testTaskID,
	})

	_, err := h.svc.Current(context.Background(), id.AgentName("alice"), workflow.RoleTestAuthor)
	require.ErrorIs(t, err, agenttools.ErrWrongAgent)
}

func TestAckHappyPathAndIdempotent(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeBaseStream(t, h, workflow.RoleTestAuthor, testAgent)

	assign := agenttools.Assignment{
		ChangeID:   testChangeID,
		TaskID:     testTaskID,
		Agent:      testAgent,
		Role:       workflow.RoleTestAuthor,
		AssignedAt: t0.Add(2 * time.Second),
		Deadline:   t0.Add(30 * time.Minute),
	}

	// 1. Ack within window
	ackTime := t0.Add(10 * time.Second)
	err := h.svc.Ack(context.Background(), assign, ackTime)
	require.NoError(t, err)

	evs, err := h.events.Load(context.Background(), testChangeID)
	require.NoError(t, err)
	require.Len(t, evs, 4)
	require.Equal(t, change.EventTaskAcked, evs[3].Type)

	// 2. Second Ack is idempotent: appends nothing
	err = h.svc.Ack(context.Background(), assign, ackTime.Add(time.Second))
	require.NoError(t, err)

	evs2, err := h.events.Load(context.Background(), testChangeID)
	require.NoError(t, err)
	require.Len(t, evs2, 4) // unchanged
}

func TestAckLateRefusal(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeBaseStream(t, h, workflow.RoleTestAuthor, testAgent)

	assign := agenttools.Assignment{
		ChangeID:   testChangeID,
		TaskID:     testTaskID,
		Agent:      testAgent,
		Role:       workflow.RoleTestAuthor,
		AssignedAt: t0.Add(2 * time.Second),
		Deadline:   t0.Add(30 * time.Minute),
	}

	// Ack after 61 seconds from AssignedAt
	lateAckTime := assign.AssignedAt.Add(61 * time.Second)
	err := h.svc.Ack(context.Background(), assign, lateAckTime)
	require.ErrorIs(t, err, agenttools.ErrAckWindowExpired)
}

func TestTimedOutAssignmentIsUnavailableEvenWhenTaskIndexIsStale(t *testing.T) {
	h := newTestHarness(t)
	makeBaseStream(t, h, workflow.RoleTestAuthor, testAgent)
	h.index.set(testAgent, workflow.RoleTestAuthor, ports.ActiveTaskRef{ChangeID: testChangeID, TaskID: testTaskID})
	events, err := h.events.Load(context.Background(), testChangeID)
	require.NoError(t, err)
	payload, err := json.Marshal(change.TaskTimedOut{TaskID: testTaskID, Agent: testAgent, Role: workflow.RoleTestAuthor, Reason: "ack deadline elapsed"})
	require.NoError(t, err)
	last := events[len(events)-1]
	deadline := change.Event{ID: h.ids.NewEvent(), ChangeID: testChangeID, Version: int64(len(events) + 1), Type: change.EventTaskTimedOut, Actor: change.Actor{Kind: change.ActorSystem, ID: "engine"}, OccurredAt: t0.Add(time.Minute), CorrelationID: last.CorrelationID, CausationID: &last.ID, Payload: payload}
	_, err = h.events.Append(context.Background(), testChangeID, int64(len(events)), []change.Event{deadline}, nil)
	require.NoError(t, err)
	_, err = h.svc.Current(context.Background(), testAgent, workflow.RoleTestAuthor)
	require.ErrorIs(t, err, agenttools.ErrNoTask)
	assignment := agenttools.Assignment{ChangeID: testChangeID, TaskID: testTaskID, Agent: testAgent, Role: workflow.RoleTestAuthor, AssignedAt: t0.Add(2 * time.Second), Deadline: t0.Add(time.Hour)}
	require.ErrorIs(t, h.svc.Progress(context.Background(), assignment, "late", t0.Add(61*time.Second)), agenttools.ErrNoTask)
}

func TestAckWrongAgentRefusal(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeBaseStream(t, h, workflow.RoleTestAuthor, testAgent)

	assign := agenttools.Assignment{
		ChangeID:   testChangeID,
		TaskID:     testTaskID,
		Agent:      id.AgentName("eve"),
		Role:       workflow.RoleTestAuthor,
		AssignedAt: t0.Add(2 * time.Second),
		Deadline:   t0.Add(30 * time.Minute),
	}

	err := h.svc.Ack(context.Background(), assign, t0.Add(10*time.Second))
	require.ErrorIs(t, err, agenttools.ErrWrongAgent)
}

func validSubmission() testset.Submission {
	return testset.Submission{
		Files: []testset.File{
			{
				Path:    "tests/heimdall/PAY-142/refund_test.go",
				Content: "package refunds\n\nimport \"testing\"\n\nfunc TestRefund(t *testing.T) {}\n",
			},
		},
		AcceptanceCriteria: map[string][]string{
			"AC1": {"TestRefund"},
		},
		ExpectedPaths: []string{"internal/refunds/refunds.go"},
		Notes:         "Assuming default timeout",
	}
}

func TestSubmitTestsHappyPathAndIdempotent(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeBaseStream(t, h, workflow.RoleTestAuthor, testAgent)

	assign := agenttools.Assignment{
		ChangeID:   testChangeID,
		TaskID:     testTaskID,
		Agent:      testAgent,
		Role:       workflow.RoleTestAuthor,
		Deadline:   t0.Add(30 * time.Minute),
		AssignedAt: t0.Add(2 * time.Second),
		Task: workflow.TaskEnvelope{
			Change: workflow.ChangeRef{ID: testChangeID, BaseSHA: testBaseSHA, HeadSHA: testHeadSHA},
			Inputs: workflow.Inputs{
				AcceptanceCriteria: []workflow.CriterionRef{{ID: "AC1", Ref: testBundleRef}},
			},
		},
	}

	sub := validSubmission()
	err := h.svc.SubmitTests(context.Background(), assign, sub, false)
	require.NoError(t, err)

	evs, err := h.events.Load(context.Background(), testChangeID)
	require.NoError(t, err)
	require.Len(t, evs, 4)
	require.Equal(t, change.EventTestsSubmitted, evs[3].Type)

	// Verify idempotency with exact same submission
	err = h.svc.SubmitTests(context.Background(), assign, sub, false)
	require.NoError(t, err)

	evs2, err := h.events.Load(context.Background(), testChangeID)
	require.NoError(t, err)
	require.Len(t, evs2, 4)
}

// A submission for a change the operator already closed is refused before anything is stored, so the log never holds
// an event the reducer refuses (the live KAN-9 and KAN-11 streams did, and room discovery stopped on them).
func TestPrepareTestsSubmissionRefusesAClosedChange(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeBaseStream(t, h, workflow.RoleTestAuthor, testAgent)
	rejected, err := json.Marshal(change.Rejected{Reason: "closed by the operator"})
	require.NoError(t, err)
	_, err = h.events.Append(context.Background(), testChangeID, 3, []change.Event{{
		ID: h.ids.NewEvent(), ChangeID: testChangeID, Version: 4, Type: change.EventChangeRejected,
		Actor: change.Actor{Kind: change.ActorHuman, ID: "operator"}, OccurredAt: t0.Add(5 * time.Second),
		CorrelationID: "corr-1", Payload: rejected,
	}}, nil)
	require.NoError(t, err)

	assign := agenttools.Assignment{
		ChangeID: testChangeID, TaskID: testTaskID, Agent: testAgent, Role: workflow.RoleTestAuthor,
		Deadline: t0.Add(30 * time.Minute), AssignedAt: t0.Add(2 * time.Second),
		Task: workflow.TaskEnvelope{
			Change: workflow.ChangeRef{ID: testChangeID, BaseSHA: testBaseSHA, HeadSHA: testHeadSHA},
			Inputs: workflow.Inputs{AcceptanceCriteria: []workflow.CriterionRef{{ID: "AC1", Ref: testBundleRef}}},
		},
	}
	_, prepared, _, err := h.svc.PrepareTestsSubmission(context.Background(), assign, validSubmission(), false)
	require.ErrorIs(t, err, agenttools.ErrReducerRefusal)
	require.False(t, prepared)
}

func TestSubmitTestsLateSemantics(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeBaseStream(t, h, workflow.RoleTestAuthor, testAgent)

	assign := agenttools.Assignment{
		ChangeID:   testChangeID,
		TaskID:     testTaskID,
		Agent:      testAgent,
		Role:       workflow.RoleTestAuthor,
		Deadline:   t0.Add(30 * time.Minute),
		AssignedAt: t0.Add(2 * time.Second),
		Task: workflow.TaskEnvelope{
			Change: workflow.ChangeRef{ID: testChangeID, BaseSHA: testBaseSHA, HeadSHA: testHeadSHA},
			Inputs: workflow.Inputs{
				AcceptanceCriteria: []workflow.CriterionRef{{ID: "AC1", Ref: testBundleRef}},
			},
		},
	}

	sub := validSubmission()
	err := h.svc.SubmitTests(context.Background(), assign, sub, true) // late = true
	require.ErrorIs(t, err, agenttools.ErrLateSubmission)

	// But task.timed_out event MUST be recorded (record and refuse)
	evs, err := h.events.Load(context.Background(), testChangeID)
	require.NoError(t, err)
	require.Len(t, evs, 4)
	require.Equal(t, change.EventTaskTimedOut, evs[3].Type)
}

func TestSubmitTestsWrongRoleRefusal(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeBaseStream(t, h, workflow.RoleReviewer, testAgent)

	assign := agenttools.Assignment{
		ChangeID: testChangeID,
		TaskID:   testTaskID,
		Agent:    testAgent,
		Role:     workflow.RoleReviewer, // not test-author
		Deadline: t0.Add(30 * time.Minute),
	}

	err := h.svc.SubmitTests(context.Background(), assign, validSubmission(), false)
	require.ErrorIs(t, err, agenttools.ErrWrongRole)
}

func makeReviewStream(t *testing.T, h *testHarness) {
	t.Helper()
	now := t0
	firstID := h.ids.NewEvent()
	corr := change.CorrelationID(firstID.String())

	trigPayload, _ := json.Marshal(change.Triggered{
		Ticket: testChangeID,
		Repo:   "acme/pay-demo",
	})
	e1 := change.Event{
		ID:            firstID,
		ChangeID:      testChangeID,
		Version:       1,
		Type:          change.EventChangeTriggered,
		Actor:         change.Actor{Kind: change.ActorSystem, ID: "trigger"},
		OccurredAt:    now,
		CorrelationID: corr,
		Payload:       trigPayload,
	}

	roomPayload, _ := json.Marshal(change.RoomOpened{
		RoomID: "room-pay-142",
	})
	e2 := change.Event{
		ID:            h.ids.NewEvent(),
		ChangeID:      testChangeID,
		Version:       2,
		Type:          change.EventRoomOpened,
		Actor:         change.Actor{Kind: change.ActorSystem, ID: "engine"},
		OccurredAt:    now.Add(time.Second),
		CorrelationID: corr,
		CausationID:   &e1.ID,
		Payload:       roomPayload,
	}

	task1Payload, _ := json.Marshal(change.TaskAssigned{
		TaskID:    id.TaskID("tsk_01J8Z3K9QW7M2P5R8T0V4TESTA"),
		Agent:     id.AgentName("test-dev"),
		Role:      workflow.RoleTestAuthor,
		Deadline:  now.Add(30 * time.Minute),
		BundleRef: testBundleRef,
	})
	e3 := change.Event{
		ID:            h.ids.NewEvent(),
		ChangeID:      testChangeID,
		Version:       3,
		Type:          change.EventTaskAssigned,
		Actor:         change.Actor{Kind: change.ActorSystem, ID: "engine"},
		OccurredAt:    now.Add(2 * time.Second),
		CorrelationID: corr,
		CausationID:   &e2.ID,
		BlobRefs:      []workflow.BlobRef{testBundleRef},
		Payload:       task1Payload,
	}

	subPayload, _ := json.Marshal(change.TestsSubmitted{
		Refs:     []string{"tests/heimdall/PAY-142/refund_test.go"},
		ACMap:    map[string][]string{"AC1": {"TestRefund"}},
		SHA:      testBaseSHA,
		TreeHash: testBaseSHA,
	})
	e4 := change.Event{
		ID:            h.ids.NewEvent(),
		ChangeID:      testChangeID,
		Version:       4,
		Type:          change.EventTestsSubmitted,
		Actor:         change.Actor{Kind: change.ActorAgent, ID: "test-dev"},
		OccurredAt:    now.Add(3 * time.Second),
		CorrelationID: corr,
		CausationID:   &e3.ID,
		BlobRefs:      []workflow.BlobRef{testBundleRef},
		Payload:       subPayload,
	}

	verifPayload, _ := json.Marshal(change.TestsVerified{
		RunIDs:       []id.RunID{id.RunID("run_01J8Z3K9QW7M2P5R8T0V4YB6CD")},
		SHA:          testBaseSHA,
		TreeHash:     testBaseSHA,
		NeedsSignoff: false,
	})
	e5 := change.Event{
		ID:            h.ids.NewEvent(),
		ChangeID:      testChangeID,
		Version:       5,
		Type:          change.EventTestsVerified,
		Actor:         change.Actor{Kind: change.ActorSystem, ID: "sandbox"},
		OccurredAt:    now.Add(4 * time.Second),
		CorrelationID: corr,
		CausationID:   &e4.ID,
		Payload:       verifPayload,
	}

	commitPayload, _ := json.Marshal(change.TestsCommitted{
		SHA:      testBaseSHA,
		TreeHash: testBaseSHA,
	})
	e6 := change.Event{
		ID:            h.ids.NewEvent(),
		ChangeID:      testChangeID,
		Version:       6,
		Type:          change.EventTestsCommitted,
		Actor:         change.Actor{Kind: change.ActorSystem, ID: "engine"},
		OccurredAt:    now.Add(5 * time.Second),
		CorrelationID: corr,
		CausationID:   &e5.ID,
		Payload:       commitPayload,
	}

	prPayload, _ := json.Marshal(change.PROpened{
		Repo:    "acme/pay-demo",
		Number:  1,
		HeadSHA: testHeadSHA,
		Author:  "author",
		Ticket:  testChangeID,
	})
	e7 := change.Event{
		ID:            h.ids.NewEvent(),
		ChangeID:      testChangeID,
		Version:       7,
		Type:          change.EventPROpened,
		Actor:         change.Actor{Kind: change.ActorGitHub, ID: "author"},
		OccurredAt:    now.Add(6 * time.Second),
		CorrelationID: corr,
		CausationID:   &e6.ID,
		Payload:       prPayload,
	}

	slopPayload, _ := json.Marshal(change.SlopEvaluated{Pass: true})
	e8 := change.Event{
		ID:            h.ids.NewEvent(),
		ChangeID:      testChangeID,
		Version:       8,
		Type:          change.EventSlopEvaluated,
		Actor:         change.Actor{Kind: change.ActorSystem, ID: "rule"},
		OccurredAt:    now.Add(7 * time.Second),
		CorrelationID: corr,
		CausationID:   &e7.ID,
		Payload:       slopPayload,
	}

	pickPayload, _ := json.Marshal(change.ReviewersPicked{
		People: []change.ActorID{change.ActorID(testAgent)},
		By:     "lead",
		Method: change.PickDefault,
	})
	e9 := change.Event{
		ID:            h.ids.NewEvent(),
		ChangeID:      testChangeID,
		Version:       9,
		Type:          change.EventReviewersPicked,
		Actor:         change.Actor{Kind: change.ActorSystem, ID: "engine"},
		OccurredAt:    now.Add(8 * time.Second),
		CorrelationID: corr,
		CausationID:   &e8.ID,
		Payload:       pickPayload,
	}

	taskPayload, _ := json.Marshal(change.TaskAssigned{
		TaskID:    testTaskID,
		Agent:     testAgent,
		Role:      workflow.RoleReviewer,
		Deadline:  now.Add(30 * time.Minute),
		BundleRef: testBundleRef,
	})
	e10 := change.Event{
		ID:            h.ids.NewEvent(),
		ChangeID:      testChangeID,
		Version:       10,
		Type:          change.EventTaskAssigned,
		Actor:         change.Actor{Kind: change.ActorSystem, ID: "engine"},
		OccurredAt:    now.Add(9 * time.Second),
		CorrelationID: corr,
		CausationID:   &e9.ID,
		BlobRefs:      []workflow.BlobRef{testBundleRef},
		Payload:       taskPayload,
	}

	evs := []change.Event{e1, e2, e3, e4, e5, e6, e7, e8, e9, e10}
	_, err := h.events.Append(context.Background(), testChangeID, 0, evs, nil)
	require.NoError(t, err)
}

func validReview() finding.Review {
	reasoning := "This missing error check causes silent data loss"
	return finding.Review{
		HeadSHA:       testHeadSHA,
		FilesReviewed: []string{"internal/refunds/refunds.go"},
		Clean:         false,
		Findings: []finding.Input{
			{
				Claim:      "Missing error check in refund processing",
				Category:   finding.CategoryCorrectness,
				Severity:   finding.SeverityHigh,
				Confidence: finding.ConfidenceHigh,
				Location: finding.Location{
					File:  "internal/refunds/refunds.go",
					Lines: [2]int{10, 20},
				},
				Evidence: []finding.Evidence{
					finding.NewReasoningEvidence(reasoning),
				},
			},
		},
	}
}

func TestSubmitReviewHappyPath(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeReviewStream(t, h)

	assign := agenttools.Assignment{
		ChangeID: testChangeID,
		TaskID:   testTaskID,
		Agent:    testAgent,
		Role:     workflow.RoleReviewer,
		Deadline: t0.Add(30 * time.Minute),
		Task: workflow.TaskEnvelope{
			Change: workflow.ChangeRef{ID: testChangeID, HeadSHA: testHeadSHA},
			Inputs: workflow.Inputs{FilesChanged: []string{"internal/refunds/refunds.go"}},
		},
	}

	receipt, err := h.svc.SubmitReview(context.Background(), assign, validReview(), false)
	require.NoError(t, err)
	require.True(t, receipt.Accepted)
	require.Empty(t, receipt.FindingErrors)

	evs, err := h.events.Load(context.Background(), testChangeID)
	require.NoError(t, err)
	require.Len(t, evs, 12) // 10 base + 1 finding.filed + 1 review.submitted
	require.Equal(t, change.EventFindingFiled, evs[10].Type)
	require.Equal(t, change.EventReviewSubmitted, evs[11].Type)
}

func TestSubmitReviewInvalidFindingReturnedWithIndex(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeReviewStream(t, h)

	assign := agenttools.Assignment{
		ChangeID: testChangeID,
		TaskID:   testTaskID,
		Agent:    testAgent,
		Role:     workflow.RoleReviewer,
		Deadline: t0.Add(30 * time.Minute),
		Task: workflow.TaskEnvelope{
			Change: workflow.ChangeRef{ID: testChangeID, HeadSHA: testHeadSHA},
			Inputs: workflow.Inputs{FilesChanged: []string{"internal/refunds/refunds.go"}},
		},
	}

	rev := validReview()
	// Finding 0: bad path
	badFinding := rev.Findings[0]
	badFinding.Location.File = "../outside/escape.go"

	// Finding 1: valid
	goodFinding := rev.Findings[0]

	rev.Findings = []finding.Input{badFinding, goodFinding}

	receipt, err := h.svc.SubmitReview(context.Background(), assign, rev, false)
	require.NoError(t, err)
	require.False(t, receipt.Accepted)
	require.Len(t, receipt.FindingErrors, 1)
	require.Equal(t, 0, receipt.FindingErrors[0].Index)
	require.Contains(t, receipt.FindingErrors[0].Message, "clean relative path inside the repository")

	// The valid finding 1 should still be filed!
	evs, err := h.events.Load(context.Background(), testChangeID)
	require.NoError(t, err)
	require.Len(t, evs, 12) // 10 base + 1 finding.filed + 1 review.submitted
	require.Equal(t, change.EventFindingFiled, evs[10].Type)
	require.Equal(t, change.EventReviewSubmitted, evs[11].Type)
}

func TestSubmitReviewAllInvalidAppendsNothing(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeReviewStream(t, h)

	assign := agenttools.Assignment{
		ChangeID: testChangeID,
		TaskID:   testTaskID,
		Agent:    testAgent,
		Role:     workflow.RoleReviewer,
		Deadline: t0.Add(30 * time.Minute),
		Task: workflow.TaskEnvelope{
			Change: workflow.ChangeRef{ID: testChangeID, HeadSHA: testHeadSHA},
			Inputs: workflow.Inputs{FilesChanged: []string{"internal/refunds/refunds.go"}},
		},
	}

	rev := validReview()
	rev.Findings[0].Location.Lines = [2]int{50, 10} // invalid lines: first > last

	receipt, err := h.svc.SubmitReview(context.Background(), assign, rev, false)
	require.NoError(t, err)
	require.False(t, receipt.Accepted)
	require.Len(t, receipt.FindingErrors, 1)

	// Nothing appended!
	evs, err := h.events.Load(context.Background(), testChangeID)
	require.NoError(t, err)
	require.Len(t, evs, 10) // unchanged
}

func makeDeliberationStream(t *testing.T, h *testHarness) {
	t.Helper()
	makeReviewStream(t, h)

	firstID := h.ids.NewEvent()
	corr := change.CorrelationID(firstID.String())
	now := t0.Add(10 * time.Second)

	refF1, err := id.ParseFindingRef("F1")
	require.NoError(t, err)
	casePayload, _ := json.Marshal(change.CaseOpened{
		CaseID:  testCaseID,
		Finding: refF1,
	})
	e11 := change.Event{
		ID:            firstID,
		ChangeID:      testChangeID,
		Version:       11,
		Type:          change.EventCaseOpened,
		Actor:         change.Actor{Kind: change.ActorSystem, ID: "engine"},
		OccurredAt:    now,
		CorrelationID: corr,
		Payload:       casePayload,
	}

	taskPayload, _ := json.Marshal(change.TaskAssigned{
		TaskID:    testTaskID,
		Agent:     testAgent,
		Role:      workflow.RoleResolver,
		Deadline:  now.Add(30 * time.Minute),
		BundleRef: testBundleRef,
	})
	e12 := change.Event{
		ID:            h.ids.NewEvent(),
		ChangeID:      testChangeID,
		Version:       12,
		Type:          change.EventTaskAssigned,
		Actor:         change.Actor{Kind: change.ActorSystem, ID: "engine"},
		OccurredAt:    now.Add(time.Second),
		CorrelationID: corr,
		BlobRefs:      []workflow.BlobRef{testBundleRef},
		Payload:       taskPayload,
	}

	_, err = h.events.Append(context.Background(), testChangeID, 10, []change.Event{e11, e12}, nil)
	require.NoError(t, err)
}

func TestSubmitVerdictHappyPath(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeDeliberationStream(t, h)

	assign := agenttools.Assignment{
		ChangeID: testChangeID,
		TaskID:   testTaskID,
		Agent:    testAgent,
		Role:     workflow.RoleResolver,
		Deadline: t0.Add(30 * time.Minute),
		Task:     workflow.TaskEnvelope{CaseID: testCaseID},
	}

	v := deliberation.Verdict{
		CaseID:     testCaseID,
		Decision:   deliberation.DecisionUphold,
		Rationale:  "The cited code confirms the finding is valid and unhandled.",
		Confidence: finding.ConfidenceHigh,
		Citations: []deliberation.Citation{
			{
				Kind:  deliberation.CitationFinding,
				Ref:   "F1",
				Quote: "The finding claim",
			},
		},
	}

	err := h.svc.SubmitVerdict(context.Background(), assign, v, false)
	require.NoError(t, err)

	evs, err := h.events.Load(context.Background(), testChangeID)
	require.NoError(t, err)
	require.Len(t, evs, 13) // 10 base + 2 delib + 1 verdict
	require.Equal(t, change.EventVerdictSubmitted, evs[12].Type)
}

func TestSubmitVerdictRequiresTheAssignedCase(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeDeliberationStream(t, h)

	v := deliberation.Verdict{
		CaseID:     testCaseID,
		Decision:   deliberation.DecisionUphold,
		Rationale:  "The cited code confirms the finding is valid and unhandled.",
		Confidence: finding.ConfidenceHigh,
		Citations: []deliberation.Citation{
			{Kind: deliberation.CitationFinding, Ref: "F1", Quote: "The finding claim"},
		},
	}
	assign := agenttools.Assignment{
		ChangeID: testChangeID,
		TaskID:   testTaskID,
		Agent:    testAgent,
		Role:     workflow.RoleResolver,
		Deadline: t0.Add(30 * time.Minute),
		Task:     workflow.TaskEnvelope{},
	}

	err := h.svc.SubmitVerdict(context.Background(), assign, v, false)
	require.ErrorIs(t, err, agenttools.ErrValidation, "a verdict for any case must not settle an unserialised resolver task")

	assign.Task.CaseID = id.CaseID("case_01J8Z3K9QW7M2P5R8T0V4YB6CE")
	err = h.svc.SubmitVerdict(context.Background(), assign, v, false)
	require.ErrorIs(t, err, agenttools.ErrValidation, "a verdict must settle its own case, not another case's")

	evs, err := h.events.Load(context.Background(), testChangeID)
	require.NoError(t, err)
	require.Len(t, evs, 12, "a refused verdict appends nothing")
}

func TestAskHumansAndProgress(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeBaseStream(t, h, workflow.RoleTestAuthor, testAgent)

	assign := agenttools.Assignment{
		ChangeID: testChangeID,
		TaskID:   testTaskID,
		Agent:    testAgent,
		Role:     workflow.RoleTestAuthor,
		Deadline: t0.Add(30 * time.Minute),
	}

	// Ask humans
	questions := []string{"Should refunds allow partial amounts?", "What is the max refund limit?"}
	err := h.svc.AskHumans(context.Background(), assign, questions)
	require.NoError(t, err)
	// The room shows each question with its ticket, its number and the exact reply command.
	require.Len(t, h.asker.calls, 2)
	require.Contains(t, h.asker.calls[0], "PAY-142 needs an answer (Q1)")
	require.Contains(t, h.asker.calls[0], questions[0])
	require.Contains(t, h.asker.calls[1], "heimdall answer PAY-142 Q2 <your answer>")

	evs, err := h.events.Load(context.Background(), testChangeID)
	require.NoError(t, err)
	require.Len(t, evs, 5)
	require.Equal(t, change.EventQuestionAsked, evs[3].Type)
	require.Equal(t, change.EventQuestionAsked, evs[4].Type)
	// The room shows each question as Q1, Q2 and so on; the answer command names them that way (A14).
	first, err := change.Decode(evs[3])
	require.NoError(t, err)
	second, err := change.Decode(evs[4])
	require.NoError(t, err)
	require.Equal(t, []string{"Q1"}, first.(change.QuestionAsked).Refs)
	require.Equal(t, []string{"Q2"}, second.(change.QuestionAsked).Refs)

	// Progress
	err = h.svc.Progress(context.Background(), assign, "Analyzed requirements", t0.Add(10*time.Second))
	require.NoError(t, err)
	// Progress is durable and can be replayed after the service is reconstructed.
	evsAfterProgress, err := h.events.Load(context.Background(), testChangeID)
	require.NoError(t, err)
	require.Len(t, evsAfterProgress, 6)
	require.Equal(t, "task.progress", string(evsAfterProgress[5].Type))
}

func TestProgressPersistsAndRateLimitSurvivesServiceReconstruction(t *testing.T) {
	h := newTestHarness(t)
	makeBaseStream(t, h, workflow.RoleTestAuthor, testAgent)
	assignment := agenttools.Assignment{ChangeID: testChangeID, TaskID: testTaskID, Agent: testAgent, Role: workflow.RoleTestAuthor, AssignedAt: t0.Add(2 * time.Second), Deadline: t0.Add(30 * time.Minute)}
	require.NoError(t, h.svc.Progress(context.Background(), assignment, "working", t0.Add(10*time.Second)))
	refs := fakeReviewRefs{}
	restarted, err := agenttools.New(agenttools.Config{Events: h.events, Index: h.index, Blobs: h.blobs, Clock: h.clock, IDs: h.ids, Asker: h.asker, ReviewRefs: refs, ReviewValidator: semanticReviewValidator{refs: refs}})
	require.NoError(t, err)
	require.ErrorIs(t, restarted.Progress(context.Background(), assignment, "too soon", t0.Add(90*time.Second)), agenttools.ErrValidation)
	require.NoError(t, restarted.Progress(context.Background(), assignment, "ready", t0.Add(2*time.Minute+10*time.Second)))
	events, err := h.events.Load(context.Background(), testChangeID)
	require.NoError(t, err)
	var progress int
	for _, event := range events {
		if event.Type == change.EventTaskProgress {
			progress++
		}
	}
	require.Equal(t, 2, progress)
}

func TestMutationRejectsAssignmentRoleDifferentFromRecordedTask(t *testing.T) {
	h := newTestHarness(t)
	makeBaseStream(t, h, workflow.RoleTestAuthor, testAgent)
	assignment := agenttools.Assignment{ChangeID: testChangeID, TaskID: testTaskID, Agent: testAgent, Role: workflow.RoleReviewer, AssignedAt: t0.Add(2 * time.Second), Deadline: t0.Add(30 * time.Minute)}
	err := h.svc.Progress(context.Background(), assignment, "role confused", t0.Add(time.Second))
	require.ErrorIs(t, err, agenttools.ErrWrongRole)
}

func TestVersionConflictRetries(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeBaseStream(t, h, workflow.RoleTestAuthor, testAgent)

	assign := agenttools.Assignment{
		ChangeID:   testChangeID,
		TaskID:     testTaskID,
		Agent:      testAgent,
		Role:       workflow.RoleTestAuthor,
		AssignedAt: t0.Add(2 * time.Second),
		Deadline:   t0.Add(30 * time.Minute),
	}

	// 1. Conflict once, succeeds on retry
	h.events.conflictNext = 1
	err := h.svc.Ack(context.Background(), assign, t0.Add(5*time.Second))
	require.NoError(t, err)

	// 2. Conflict 5 times -> fails with ErrVersionConflict
	h.events.conflictNext = 5
	assign2 := agenttools.Assignment{
		ChangeID:   testChangeID,
		TaskID:     testTaskID,
		Agent:      testAgent,
		Role:       workflow.RoleTestAuthor,
		AssignedAt: t0.Add(2 * time.Second),
		Deadline:   t0.Add(30 * time.Minute),
		Task: workflow.TaskEnvelope{
			Change: workflow.ChangeRef{ID: testChangeID, BaseSHA: testBaseSHA, HeadSHA: testHeadSHA},
			Inputs: workflow.Inputs{
				AcceptanceCriteria: []workflow.CriterionRef{{ID: "AC1", Ref: testBundleRef}},
			},
		},
	}
	err = h.svc.SubmitTests(context.Background(), assign2, validSubmission(), false)
	require.ErrorIs(t, err, agenttools.ErrVersionConflict)
}

func TestMutationRoleGuardRejectsDifferentAssignedRole(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	// Create base stream in StateWritingTests with test-author
	makeBaseStream(t, h, workflow.RoleTestAuthor, testAgent)

	assign := agenttools.Assignment{
		ChangeID: testChangeID,
		TaskID:   testTaskID,
		Agent:    testAgent,
		Role:     workflow.RoleTestAuthor, // cannot submit a resolver verdict
		Deadline: t0.Add(30 * time.Minute),
	}

	v := deliberation.Verdict{
		CaseID:     testCaseID,
		Decision:   deliberation.DecisionUphold,
		Rationale:  "The cited code confirms the finding is valid and unhandled.",
		Confidence: finding.ConfidenceHigh,
		Citations: []deliberation.Citation{
			{Kind: deliberation.CitationFinding, Ref: "F1", Quote: "quote"},
		},
	}

	err := h.svc.SubmitVerdict(context.Background(), assign, v, false)
	require.ErrorIs(t, err, agenttools.ErrWrongRole)
}

func TestNoSecretInErrors(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeBaseStream(t, h, workflow.RoleTestAuthor, testAgent)

	// Split so gitleaks does not treat the fixture as a live credential.
	secretToken := "secret_ghp_" + "1234567890abcdef"
	assign := agenttools.Assignment{
		ChangeID:   testChangeID,
		TaskID:     testTaskID,
		Agent:      id.AgentName(secretToken),
		Role:       workflow.RoleTestAuthor,
		AssignedAt: t0.Add(2 * time.Second),
		Deadline:   t0.Add(30 * time.Minute),
	}

	// An error from ack or validation must NOT reveal the secretToken
	err := h.svc.Ack(context.Background(), assign, t0.Add(100*time.Second))
	require.Error(t, err)
	require.NotContains(t, err.Error(), secretToken)
}

// A finding filed with tool evidence carries that evidence in finding.filed, so the sandbox can repeat the tool and
// verify the claim (A2). Without it the verifier has nothing to run.
func TestSubmitReviewCarriesToolEvidenceInTheFindingEvent(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeReviewStream(t, h)
	assign := agenttools.Assignment{
		ChangeID: testChangeID, TaskID: testTaskID, Agent: testAgent, Role: workflow.RoleReviewer,
		Deadline: t0.Add(30 * time.Minute),
		Task: workflow.TaskEnvelope{
			Change: workflow.ChangeRef{ID: testChangeID, HeadSHA: testHeadSHA},
			Inputs: workflow.Inputs{FilesChanged: []string{"internal/refunds/refunds.go"}},
		},
	}
	review := validReview()
	loc := finding.Location{File: "internal/refunds/refunds.go", Lines: [2]int{10, 20}}
	review.Findings[0].Evidence = []finding.Evidence{{
		Type:       finding.EvidenceToolResult,
		ToolResult: &finding.ToolResult{Tool: "go vet", Location: loc},
	}}

	receipt, err := h.svc.SubmitReview(context.Background(), assign, review, false)
	require.NoError(t, err)
	require.True(t, receipt.Accepted, "%+v", receipt.FindingErrors)

	evs, err := h.events.Load(context.Background(), testChangeID)
	require.NoError(t, err)
	filed := evs[10]
	require.Equal(t, change.EventFindingFiled, filed.Type)
	p, err := change.Decode(filed)
	require.NoError(t, err)
	rec := change.FindingRecord(p.(change.FindingFiled))
	require.Equal(t, finding.TierE2, rec.Tier)
	require.NotNil(t, rec.ToolResult, "the tool evidence must travel on the event")
	require.Equal(t, loc, rec.ToolResult.Location)
}

// A finding that cites a confirmed memory item carries the reference on its event, so the server can look it up again.
func TestSubmitReviewCarriesTheMemoryRefInTheFindingEvent(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeReviewStream(t, h)
	assign := agenttools.Assignment{
		ChangeID: testChangeID, TaskID: testTaskID, Agent: testAgent, Role: workflow.RoleReviewer,
		Deadline: t0.Add(30 * time.Minute),
		Task: workflow.TaskEnvelope{
			Change: workflow.ChangeRef{ID: testChangeID, HeadSHA: testHeadSHA},
			Inputs: workflow.Inputs{FilesChanged: []string{"internal/refunds/refunds.go"}},
		},
	}
	review := validReview()
	ref := "decision:refund-limit"
	review.Findings[0].Evidence = []finding.Evidence{{Type: finding.EvidenceMemory, MemoryRef: &ref}}

	receipt, err := h.svc.SubmitReview(context.Background(), assign, review, false)
	require.NoError(t, err)
	require.True(t, receipt.Accepted, "%+v", receipt.FindingErrors)

	evs, err := h.events.Load(context.Background(), testChangeID)
	require.NoError(t, err)
	p, err := change.Decode(evs[10])
	require.NoError(t, err)
	rec := change.FindingRecord(p.(change.FindingFiled))
	require.Equal(t, finding.TierE3, rec.Tier)
	require.NotNil(t, rec.MemoryRef)
	require.Equal(t, ref, *rec.MemoryRef)
}

// With an Advance hook, a review goes to the workflow engine instead of straight into the store, so the steps see the
// findings and can start verifying them (A2). The service stores nothing itself then.
func TestSubmitReviewHandsItsEventsToTheAdvanceHook(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeReviewStream(t, h)
	var handed []change.Event
	refs := fakeReviewRefs{}
	svc, err := agenttools.New(agenttools.Config{
		Events: h.events, Index: h.index, Blobs: h.blobs, Clock: h.clock, IDs: h.ids, Asker: h.asker,
		ReviewRefs: refs, ReviewValidator: semanticReviewValidator{refs: refs},
		Advance: func(_ context.Context, _ id.ChangeID, evs []change.Event) error {
			handed = append(handed, evs...)
			return nil
		},
	})
	require.NoError(t, err)
	assign := agenttools.Assignment{
		ChangeID: testChangeID, TaskID: testTaskID, Agent: testAgent, Role: workflow.RoleReviewer,
		Deadline: t0.Add(30 * time.Minute),
		Task: workflow.TaskEnvelope{
			Change: workflow.ChangeRef{ID: testChangeID, HeadSHA: testHeadSHA},
			Inputs: workflow.Inputs{FilesChanged: []string{"internal/refunds/refunds.go"}},
		},
	}

	receipt, err := svc.SubmitReview(context.Background(), assign, validReview(), false)
	require.NoError(t, err)
	require.True(t, receipt.Accepted)

	require.Len(t, handed, 2)
	require.Equal(t, change.EventFindingFiled, handed[0].Type)
	require.Equal(t, change.EventReviewSubmitted, handed[1].Type)
	stored, err := h.events.Load(context.Background(), testChangeID)
	require.NoError(t, err)
	require.Len(t, stored, 10, "the service wrote nothing itself; the hook stores the events")
}
