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

func (f *fakeIDs) NewFinding() id.FindingID { return id.FindingID(f.next("fnd")) }
func (f *fakeIDs) NewRun() id.RunID         { return id.RunID(f.next("run")) }
func (f *fakeIDs) NewCase() id.CaseID       { return id.CaseID(f.next("case")) }
func (f *fakeIDs) NewTask() id.TaskID       { return id.TaskID(f.next("tsk")) }

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
func (fakeReviewRefs) ProfileChecks(_ context.Context, _ workflow.ChangeRef) ([]change.ProfileCheck, error) {
	return []change.ProfileCheck{{ID: "patterns"}}, nil
}
func (fakeReviewRefs) MemoryExists(_ context.Context, _ workflow.ChangeRef, _ id.AgentName, ref string) (bool, error) {
	return ref == "decision:refund-limit", nil
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

func eventStream(t *testing.T, h *testHarness) []change.Event {
	t.Helper()
	evs, err := h.events.Load(context.Background(), testChangeID)
	require.NoError(t, err)
	return evs
}

// streamEv is one event to append: type, actor, payload and the blobs it names.
type streamEv struct {
	typ     change.EventType
	actor   change.Actor
	payload any
	blobs   []workflow.BlobRef
}

// appendStream appends events with sequential versions, one second apart, chained by causation under one correlation.
func appendStream(t *testing.T, h *testHarness, evs []streamEv) {
	t.Helper()
	cur := eventStream(t, h)
	var out []change.Event
	var prev *change.EventID
	var corr change.CorrelationID
	if len(cur) > 0 {
		corr = cur[0].CorrelationID
		prev = &cur[len(cur)-1].ID
	}
	for i, e := range evs {
		raw, err := json.Marshal(e.payload)
		require.NoError(t, err)
		eid := h.ids.NewEvent()
		if len(cur) == 0 && i == 0 {
			corr = change.CorrelationID(eid.String())
		}
		out = append(out, change.Event{
			ID: eid, ChangeID: testChangeID, Version: int64(len(cur) + i + 1), Type: e.typ, Actor: e.actor,
			OccurredAt: t0.Add(time.Duration(len(cur)+i) * time.Second), CorrelationID: corr, CausationID: prev,
			Payload: raw, BlobRefs: e.blobs,
		})
		prev = &out[len(out)-1].ID
	}
	_, err := h.events.Append(context.Background(), testChangeID, int64(len(cur)), out, nil)
	require.NoError(t, err)
}

func makeBaseStream(t *testing.T, h *testHarness, role workflow.Role, agent id.AgentName) {
	t.Helper()
	appendStream(t, h, []streamEv{
		{change.EventChangeTriggered, change.Actor{Kind: change.ActorSystem, ID: "trigger"},
			change.Triggered{Ticket: testChangeID, Repo: "acme/pay-demo", Assignee: "dev-author", Source: "jira"}, nil},
		{change.EventRoomOpened, change.Actor{Kind: change.ActorSystem, ID: "engine"}, change.RoomOpened{RoomID: "room-pay-142"}, nil},
		{change.EventTaskAssigned, change.Actor{Kind: change.ActorSystem, ID: "engine"},
			change.TaskAssigned{TaskID: testTaskID, CaseID: testCaseID, Agent: agent, Role: role, Deadline: t0.Add(30 * time.Minute), BundleRef: testBundleRef},
			[]workflow.BlobRef{testBundleRef}},
	})
}

// baseAssign is the assignment the base stream records, with the task envelope a mutation needs.
func baseAssign(role workflow.Role) agenttools.Assignment {
	return agenttools.Assignment{
		ChangeID: testChangeID, TaskID: testTaskID, Agent: testAgent, Role: role,
		AssignedAt: t0.Add(2 * time.Second), Deadline: t0.Add(30 * time.Minute),
	}
}

func testsAssign() agenttools.Assignment {
	a := baseAssign(workflow.RoleTestAuthor)
	a.Task = workflow.TaskEnvelope{
		Change: workflow.ChangeRef{ID: testChangeID, BaseSHA: testBaseSHA, HeadSHA: testHeadSHA},
		Inputs: workflow.Inputs{AcceptanceCriteria: []workflow.CriterionRef{{ID: "AC1", Ref: testBundleRef}}},
	}
	return a
}

func reviewAssign() agenttools.Assignment {
	a := baseAssign(workflow.RoleReviewer)
	a.Task = workflow.TaskEnvelope{
		Change: workflow.ChangeRef{ID: testChangeID, HeadSHA: testHeadSHA},
		Inputs: workflow.Inputs{FilesChanged: []string{"internal/refunds/refunds.go"}},
	}
	return a
}

// --- Tests ---

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
	require.False(t, assign.Acked)
	require.Equal(t, t0.Add(2*time.Second), assign.AssignedAt)
	require.Equal(t, t0.Add(30*time.Minute), assign.Deadline)
	require.Nil(t, assign.Revision, "no profile request was made")
}

// The profiler's task carries the newest request to revise the repository's profile, so get_task can show the note with
// the promoted profile (D184).
func TestCurrentCarriesTheNewestProfileRequest(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeBaseStream(t, h, workflow.RoleProfiler, testAgent)
	h.index.set(testAgent, workflow.RoleProfiler, ports.ActiveTaskRef{ChangeID: testChangeID, TaskID: testTaskID})
	for _, note := range []string{"add lint", "add a golangci-lint check that writes SARIF"} {
		appendStream(t, h, []streamEv{{change.EventProfileRequested, change.Actor{Kind: change.ActorHuman, ID: "ana"},
			change.ProfileRequested{Repo: "acme/pay-demo", By: "ana", Note: note}, nil}})
	}

	assign, err := h.svc.Current(context.Background(), testAgent, workflow.RoleProfiler)
	require.NoError(t, err)
	require.Equal(t, &change.ProfileRequestInfo{Repo: "acme/pay-demo", By: "ana",
		Note: "add a golangci-lint check that writes SARIF", Asked: t0.Add(4 * time.Second)}, assign.Revision)
}

func TestCurrentRefusesWithoutAMatchingTask(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	_, err := h.svc.Current(context.Background(), testAgent, workflow.RoleTestAuthor)
	require.ErrorIs(t, err, agenttools.ErrNoTask)

	makeBaseStream(t, h, workflow.RoleTestAuthor, testAgent)
	// Index points to a task, but the stream's task belongs to bob, not alice.
	h.index.set(id.AgentName("alice"), workflow.RoleTestAuthor, ports.ActiveTaskRef{ChangeID: testChangeID, TaskID: testTaskID})
	_, err = h.svc.Current(context.Background(), id.AgentName("alice"), workflow.RoleTestAuthor)
	require.ErrorIs(t, err, agenttools.ErrWrongAgent)
}

func TestAckHappyPathAndIdempotent(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeBaseStream(t, h, workflow.RoleTestAuthor, testAgent)
	assign := baseAssign(workflow.RoleTestAuthor)

	// 1. Ack within window
	ackTime := t0.Add(10 * time.Second)
	require.NoError(t, h.svc.Ack(context.Background(), assign, ackTime))
	evs := eventStream(t, h)
	require.Len(t, evs, 4)
	require.Equal(t, change.EventTaskAcked, evs[3].Type)

	// 2. Second Ack is idempotent: appends nothing
	require.NoError(t, h.svc.Ack(context.Background(), assign, ackTime.Add(time.Second)))
	require.Len(t, eventStream(t, h), 4)
}

func TestAckRefusals(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeBaseStream(t, h, workflow.RoleTestAuthor, testAgent)
	assign := baseAssign(workflow.RoleTestAuthor)

	// An ack after the deadline is refused; a late ack before it is accepted (a recovered agent can still work).
	require.ErrorIs(t, h.svc.Ack(context.Background(), assign, assign.Deadline.Add(time.Second)), agenttools.ErrAckWindowExpired)
	require.NoError(t, h.svc.Ack(context.Background(), assign, assign.AssignedAt.Add(agenttools.AckWindow+time.Second)))

	eve := assign
	eve.Agent = id.AgentName("eve")
	require.ErrorIs(t, h.svc.Ack(context.Background(), eve, t0.Add(10*time.Second)), agenttools.ErrWrongAgent)
}

func TestTimedOutAssignmentIsUnavailableEvenWhenTaskIndexIsStale(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeBaseStream(t, h, workflow.RoleTestAuthor, testAgent)
	h.index.set(testAgent, workflow.RoleTestAuthor, ports.ActiveTaskRef{ChangeID: testChangeID, TaskID: testTaskID})
	appendStream(t, h, []streamEv{{change.EventTaskTimedOut, change.Actor{Kind: change.ActorSystem, ID: "engine"},
		change.TaskTimedOut{TaskID: testTaskID, Agent: testAgent, Role: workflow.RoleTestAuthor, Reason: "ack deadline elapsed"}, nil}})

	_, err := h.svc.Current(context.Background(), testAgent, workflow.RoleTestAuthor)
	require.ErrorIs(t, err, agenttools.ErrNoTask)
	assignment := baseAssign(workflow.RoleTestAuthor)
	assignment.Deadline = t0.Add(time.Hour)
	require.ErrorIs(t, h.svc.Progress(context.Background(), assignment, "late", t0.Add(61*time.Second)), agenttools.ErrNoTask)
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
	assign := testsAssign()

	sub := validSubmission()
	require.NoError(t, h.svc.SubmitTests(context.Background(), assign, sub, false))
	evs := eventStream(t, h)
	require.Len(t, evs, 4)
	require.Equal(t, change.EventTestsSubmitted, evs[3].Type)

	// Exact same submission again: idempotent.
	require.NoError(t, h.svc.SubmitTests(context.Background(), assign, sub, false))
	require.Len(t, eventStream(t, h), 4)
}

// A submission for a change the operator already closed is refused before anything is stored, so the log never holds
// an event the reducer refuses (the live KAN-9 and KAN-11 streams did, and room discovery stopped on them).
func TestPrepareTestsSubmissionRefusesAClosedChange(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeBaseStream(t, h, workflow.RoleTestAuthor, testAgent)
	appendStream(t, h, []streamEv{{change.EventChangeRejected, change.Actor{Kind: change.ActorHuman, ID: "operator"},
		change.Rejected{Reason: "closed by the operator"}, nil}})

	_, prepared, _, err := h.svc.PrepareTestsSubmission(context.Background(), testsAssign(), validSubmission(), false)
	require.ErrorIs(t, err, agenttools.ErrRefused)
	require.False(t, prepared)
}

func TestSubmitTestsLateSemantics(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeBaseStream(t, h, workflow.RoleTestAuthor, testAgent)

	err := h.svc.SubmitTests(context.Background(), testsAssign(), validSubmission(), true) // late = true
	require.ErrorIs(t, err, agenttools.ErrLateSubmission)

	// But task.timed_out event MUST be recorded (record and refuse)
	evs := eventStream(t, h)
	require.Len(t, evs, 4)
	require.Equal(t, change.EventTaskTimedOut, evs[3].Type)
}

func TestSubmitTestsWrongRoleRefusal(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeBaseStream(t, h, workflow.RoleReviewer, testAgent)
	require.ErrorIs(t, h.svc.SubmitTests(context.Background(), baseAssign(workflow.RoleReviewer), validSubmission(), false), agenttools.ErrWrongRole)
}

func makeReviewStream(t *testing.T, h *testHarness) {
	t.Helper()
	sys := change.Actor{Kind: change.ActorSystem, ID: "engine"}
	appendStream(t, h, []streamEv{
		{change.EventChangeTriggered, change.Actor{Kind: change.ActorSystem, ID: "trigger"},
			change.Triggered{Ticket: testChangeID, Repo: "acme/pay-demo", Assignee: "dev-author", Source: "jira"}, nil},
		{change.EventRoomOpened, sys, change.RoomOpened{RoomID: "room-pay-142"}, nil},
		{change.EventTaskAssigned, sys, change.TaskAssigned{TaskID: "tsk_01J8Z3K9QW7M2P5R8T0V4TESTA", Agent: "test-dev",
			Role: workflow.RoleTestAuthor, Deadline: t0.Add(30 * time.Minute), BundleRef: testBundleRef}, []workflow.BlobRef{testBundleRef}},
		{change.EventTestsSubmitted, change.Actor{Kind: change.ActorAgent, ID: "test-dev"},
			change.TestsSubmitted{Refs: []string{"tests/heimdall/PAY-142/refund_test.go"}, ACMap: map[string][]string{"AC1": {"TestRefund"}},
				SHA: testBaseSHA, TreeHash: testBaseSHA}, []workflow.BlobRef{testBundleRef}},
		{change.EventTestsVerified, change.Actor{Kind: change.ActorSystem, ID: "sandbox"},
			change.TestsVerified{RunIDs: []id.RunID{"run_01J8Z3K9QW7M2P5R8T0V4YB6CD"}, SHA: testBaseSHA, TreeHash: testBaseSHA}, nil},
		{change.EventTestsCommitted, sys, change.TestsCommitted{SHA: testBaseSHA, TreeHash: testBaseSHA}, nil},
		{change.EventPROpened, change.Actor{Kind: change.ActorGitHub, ID: "author"},
			change.PROpened{Repo: "acme/pay-demo", Number: 1, HeadSHA: testHeadSHA, Author: "author", Ticket: testChangeID}, nil},
		{change.EventSlopEvaluated, change.Actor{Kind: change.ActorSystem, ID: "rule"}, change.SlopEvaluated{Pass: true}, nil},
		{change.EventReviewersPicked, sys, change.ReviewersPicked{People: []change.ActorID{change.ActorID(testAgent)}, By: "lead", Method: change.PickDefault}, nil},
		{change.EventTaskAssigned, sys, change.TaskAssigned{TaskID: testTaskID, Agent: testAgent, Role: workflow.RoleReviewer,
			Deadline: t0.Add(30 * time.Minute), BundleRef: testBundleRef}, []workflow.BlobRef{testBundleRef}},
	})
}

func validReview() finding.Review {
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
					finding.NewReasoningEvidence("This missing error check causes silent data loss"),
				},
			},
		},
	}
}

func TestSubmitReviewHappyPath(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeReviewStream(t, h)

	receipt, err := h.svc.SubmitReview(context.Background(), reviewAssign(), validReview(), false)
	require.NoError(t, err)
	require.True(t, receipt.Accepted)
	require.Empty(t, receipt.FindingErrors)

	evs := eventStream(t, h)
	require.Len(t, evs, 12) // 10 base + 1 finding.filed + 1 review.submitted
	require.Equal(t, change.EventFindingFiled, evs[10].Type)
	require.Equal(t, change.EventReviewSubmitted, evs[11].Type)
}

func TestSubmitReviewInvalidFindingReturnedWithIndex(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeReviewStream(t, h)

	rev := validReview()
	badFinding := rev.Findings[0]
	badFinding.Location.File = "../outside/escape.go"
	rev.Findings = []finding.Input{badFinding, rev.Findings[0]} // finding 0 bad, finding 1 valid

	receipt, err := h.svc.SubmitReview(context.Background(), reviewAssign(), rev, false)
	require.NoError(t, err)
	require.False(t, receipt.Accepted)
	require.Len(t, receipt.FindingErrors, 1)
	require.Equal(t, 0, receipt.FindingErrors[0].Index)
	require.Contains(t, receipt.FindingErrors[0].Message, "clean relative path inside the repository")

	// The valid finding 1 should still be filed!
	evs := eventStream(t, h)
	require.Len(t, evs, 12)
	require.Equal(t, change.EventFindingFiled, evs[10].Type)
	require.Equal(t, change.EventReviewSubmitted, evs[11].Type)
}

func TestSubmitReviewAllInvalidAppendsNothing(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeReviewStream(t, h)

	rev := validReview()
	rev.Findings[0].Location.Lines = [2]int{50, 10} // invalid lines: first > last

	receipt, err := h.svc.SubmitReview(context.Background(), reviewAssign(), rev, false)
	require.NoError(t, err)
	require.False(t, receipt.Accepted)
	require.Len(t, receipt.FindingErrors, 1)
	require.Len(t, eventStream(t, h), 10, "nothing appended")
}

func makeDeliberationStream(t *testing.T, h *testHarness) {
	t.Helper()
	makeReviewStream(t, h)
	refF1, err := id.ParseFindingRef("F1")
	require.NoError(t, err)
	appendStream(t, h, []streamEv{
		{change.EventCaseOpened, change.Actor{Kind: change.ActorSystem, ID: "engine"}, change.CaseOpened{CaseID: testCaseID, Finding: refF1}, nil},
		{change.EventTaskAssigned, change.Actor{Kind: change.ActorSystem, ID: "engine"},
			change.TaskAssigned{TaskID: testTaskID, Agent: testAgent, Role: workflow.RoleResolver, Deadline: t0.Add(30 * time.Minute), BundleRef: testBundleRef},
			[]workflow.BlobRef{testBundleRef}},
	})
}

func upholdVerdict() deliberation.Verdict {
	return deliberation.Verdict{
		CaseID:     testCaseID,
		Decision:   deliberation.DecisionUphold,
		Rationale:  "The cited code confirms the finding is valid and unhandled.",
		Confidence: finding.ConfidenceHigh,
		Citations: []deliberation.Citation{
			{Kind: deliberation.CitationFinding, Ref: "F1", Quote: "The finding claim"},
		},
	}
}

func TestSubmitVerdictHappyPath(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeDeliberationStream(t, h)
	assign := baseAssign(workflow.RoleResolver)
	assign.Task = workflow.TaskEnvelope{CaseID: testCaseID}

	require.NoError(t, h.svc.SubmitVerdict(context.Background(), assign, upholdVerdict(), false))
	evs := eventStream(t, h)
	require.Len(t, evs, 13) // 10 base + 2 delib + 1 verdict
	require.Equal(t, change.EventVerdictSubmitted, evs[12].Type)
}

func TestSubmitVerdictRequiresTheAssignedCase(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeDeliberationStream(t, h)
	assign := baseAssign(workflow.RoleResolver)

	err := h.svc.SubmitVerdict(context.Background(), assign, upholdVerdict(), false)
	require.ErrorIs(t, err, agenttools.ErrValidation, "a verdict for any case must not settle an unserialised resolver task")

	assign.Task.CaseID = id.CaseID("case_01J8Z3K9QW7M2P5R8T0V4YB6CE")
	err = h.svc.SubmitVerdict(context.Background(), assign, upholdVerdict(), false)
	require.ErrorIs(t, err, agenttools.ErrValidation, "a verdict must settle its own case, not another case's")
	require.Len(t, eventStream(t, h), 12, "a refused verdict appends nothing")
}

func TestAskHumansAndProgress(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeBaseStream(t, h, workflow.RoleTestAuthor, testAgent)
	assign := baseAssign(workflow.RoleTestAuthor)

	questions := []string{"Should refunds allow partial amounts?", "What is the max refund limit?"}
	require.NoError(t, h.svc.AskHumans(context.Background(), assign, questions))
	// The room shows each question with its ticket, its number and the exact reply command.
	require.Len(t, h.asker.calls, 2)
	require.Contains(t, h.asker.calls[0], "PAY-142 needs an answer (Q1)")
	require.Contains(t, h.asker.calls[0], questions[0])
	require.Contains(t, h.asker.calls[1], "heimdall answer PAY-142 Q2 <your answer>")

	evs := eventStream(t, h)
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

	require.NoError(t, h.svc.Progress(context.Background(), assign, "Analyzed requirements", t0.Add(10*time.Second)))
	// Progress is durable and can be replayed after the service is reconstructed.
	evsAfterProgress := eventStream(t, h)
	require.Len(t, evsAfterProgress, 6)
	require.Equal(t, change.EventTaskProgress, evsAfterProgress[5].Type)
}

func TestProgressPersistsAndRateLimitSurvivesServiceReconstruction(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeBaseStream(t, h, workflow.RoleTestAuthor, testAgent)
	assignment := baseAssign(workflow.RoleTestAuthor)
	require.NoError(t, h.svc.Progress(context.Background(), assignment, "working", t0.Add(10*time.Second)))
	refs := fakeReviewRefs{}
	restarted, err := agenttools.New(agenttools.Config{Events: h.events, Index: h.index, Blobs: h.blobs, Clock: h.clock, IDs: h.ids, Asker: h.asker, ReviewRefs: refs, ReviewValidator: semanticReviewValidator{refs: refs}})
	require.NoError(t, err)
	require.ErrorIs(t, restarted.Progress(context.Background(), assignment, "too soon", t0.Add(90*time.Second)), agenttools.ErrValidation)
	require.NoError(t, restarted.Progress(context.Background(), assignment, "ready", t0.Add(2*time.Minute+10*time.Second)))
	var progress int
	for _, event := range eventStream(t, h) {
		if event.Type == change.EventTaskProgress {
			progress++
		}
	}
	require.Equal(t, 2, progress)
}

func TestMutationRejectsAssignmentRoleDifferentFromRecordedTask(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeBaseStream(t, h, workflow.RoleTestAuthor, testAgent)
	err := h.svc.Progress(context.Background(), baseAssign(workflow.RoleReviewer), "role confused", t0.Add(time.Second))
	require.ErrorIs(t, err, agenttools.ErrWrongRole)
}

func TestVersionConflictRetries(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeBaseStream(t, h, workflow.RoleTestAuthor, testAgent)

	// 1. Conflict once, succeeds on retry
	h.events.conflictNext = 1
	require.NoError(t, h.svc.Ack(context.Background(), baseAssign(workflow.RoleTestAuthor), t0.Add(5*time.Second)))

	// 2. Conflict 5 times -> fails with ErrVersionConflict
	h.events.conflictNext = 5
	require.ErrorIs(t, h.svc.SubmitTests(context.Background(), testsAssign(), validSubmission(), false), agenttools.ErrVersionConflict)
}

func TestMutationRoleGuardRejectsDifferentAssignedRole(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeBaseStream(t, h, workflow.RoleTestAuthor, testAgent)
	// A test-author cannot submit a resolver verdict.
	err := h.svc.SubmitVerdict(context.Background(), baseAssign(workflow.RoleTestAuthor), upholdVerdict(), false)
	require.ErrorIs(t, err, agenttools.ErrWrongRole)
}

func TestNoSecretInErrors(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeBaseStream(t, h, workflow.RoleTestAuthor, testAgent)

	// Split so gitleaks does not treat the fixture as a live credential.
	secretToken := "secret_ghp_" + "1234567890abcdef"
	assign := baseAssign(workflow.RoleTestAuthor)
	assign.Agent = id.AgentName(secretToken)

	// An error from ack or validation must NOT reveal the secretToken
	err := h.svc.Ack(context.Background(), assign, t0.Add(100*time.Second))
	require.Error(t, err)
	require.NotContains(t, err.Error(), secretToken)
}

// The finding event carries what the verifier needs to repeat the claim: tool evidence (A2), a confirmed memory ref,
// and the rule cards the finding cites (deduped). Without them the verifier has nothing to run.
func TestSubmitReviewCarriesEvidenceAndRefsInTheEvents(t *testing.T) {
	t.Parallel()
	loc := finding.Location{File: "internal/refunds/refunds.go", Lines: [2]int{10, 20}}
	memRef := "decision:refund-limit"
	cases := []struct {
		name   string
		mutate func(*finding.Review)
		check  func(t *testing.T, filed change.FindingFiled, submitted change.ReviewSubmitted)
	}{
		{"tool evidence", func(r *finding.Review) {
			r.Findings[0].Evidence = []finding.Evidence{{Type: finding.EvidenceToolResult, ToolResult: &finding.ToolResult{CheckID: "patterns", Location: loc}}}
		}, func(t *testing.T, filed change.FindingFiled, _ change.ReviewSubmitted) {
			rec := change.FindingRecord(filed)
			require.Equal(t, finding.TierE2, rec.Tier)
			require.NotNil(t, rec.ToolResult, "the tool evidence must travel on the event")
			require.Equal(t, loc, rec.ToolResult.Location)
		}},
		{"memory ref", func(r *finding.Review) {
			r.Findings[0].Evidence = []finding.Evidence{{Type: finding.EvidenceMemory, MemoryRef: &memRef}}
		}, func(t *testing.T, filed change.FindingFiled, _ change.ReviewSubmitted) {
			rec := change.FindingRecord(filed)
			require.Equal(t, finding.TierE3, rec.Tier)
			require.NotNil(t, rec.MemoryRef)
			require.Equal(t, memRef, *rec.MemoryRef)
		}},
		{"rule checks and card refs", func(r *finding.Review) {
			r.Findings[0].ContextRefs = []string{"card:refund-cap.md", "card:refund-cap.md"}
			r.RuleChecks = []finding.RuleCheck{{Card: "refund-cap.md", Verdict: finding.RuleViolated, Note: "The cap ignores earlier refunds."}}
		}, func(t *testing.T, filed change.FindingFiled, submitted change.ReviewSubmitted) {
			require.Equal(t, []string{"refund-cap.md"}, filed.CardRefs)
			require.Equal(t, []finding.RuleCheck{{Card: "refund-cap.md", Verdict: finding.RuleViolated, Note: "The cap ignores earlier refunds."}}, submitted.RuleChecks)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newTestHarness(t)
			makeReviewStream(t, h)
			review := validReview()
			c.mutate(&review)

			receipt, err := h.svc.SubmitReview(context.Background(), reviewAssign(), review, false)
			require.NoError(t, err)
			require.True(t, receipt.Accepted, "%+v", receipt.FindingErrors)

			evs := eventStream(t, h)
			filed, err := change.Decode(evs[10])
			require.NoError(t, err)
			require.Equal(t, change.EventFindingFiled, evs[10].Type)
			submitted, err := change.Decode(evs[len(evs)-1])
			require.NoError(t, err)
			c.check(t, filed.(change.FindingFiled), submitted.(change.ReviewSubmitted))
		})
	}
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

	receipt, err := svc.SubmitReview(context.Background(), reviewAssign(), validReview(), false)
	require.NoError(t, err)
	require.True(t, receipt.Accepted)

	require.Len(t, handed, 2)
	require.Equal(t, change.EventFindingFiled, handed[0].Type)
	require.Equal(t, change.EventReviewSubmitted, handed[1].Type)
	require.Len(t, eventStream(t, h), 10, "the service wrote nothing itself; the hook stores the events")
}

// A submission is stored whole and names the task it ends (HARNESS_DESIGN B6): through the tool, the fold and back,
// every field is in the view or the blob.
func TestATestsSubmissionRoundTripsThroughTheFold(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeBaseStream(t, h, workflow.RoleTestAuthor, testAgent)
	sub := validSubmission()
	sub.Notes = "assumes the refund cap is per payment"
	require.NoError(t, h.svc.SubmitTests(context.Background(), testsAssign(), sub, false))

	evs := eventStream(t, h)
	v, err := change.Fold(evs)
	require.NoError(t, err)
	require.NotNil(t, v.Tests)
	require.Equal(t, testTaskID, v.Tests.TaskID)
	require.Equal(t, testBaseSHA, v.Tests.BaseSHA)
	require.Equal(t, sub.AcceptanceCriteria, v.Tests.ACMap)
	require.Equal(t, v.Tests.Expected, sub.ExpectedPaths)
	require.Len(t, v.Tests.Files, len(sub.Files))
	for i, f := range sub.Files {
		require.Equal(t, f.Path, v.Tests.Files[i].Path)
		require.NotEmpty(t, v.Tests.Files[i].Blob, "each file names the blob that holds the submission")
	}
	require.Equal(t, 1, v.Tests.Attempts)
	var payload change.TestsSubmitted
	require.NoError(t, json.Unmarshal(evs[len(evs)-1].Payload, &payload))
	require.Equal(t, sub.Notes, payload.Notes)
}
