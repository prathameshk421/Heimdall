package prsync_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/prsync"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const (
	testRepo     change.Repo    = "acme/pay"
	authorLogin  change.ActorID = "dev-alice"
	reviewerBob  change.ActorID = "rev-bob"
	reviewerCarl change.ActorID = "rev-carl"
	head1        id.SHA         = "1111111111111111111111111111111111111111"
	head2        id.SHA         = "2222222222222222222222222222222222222222"
	baseSHA      id.SHA         = "0000000000000000000000000000000000000000"
)

var testNow = time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)

// fakeEvents implements ports.EventStore with optimistic concurrency.
type fakeEvents struct {
	mu            sync.Mutex
	streams       map[id.ChangeID][]change.Event
	failAppend    int
	failAppendErr error
	appendCalls   int
}

func newFakeEvents() *fakeEvents {
	return &fakeEvents{streams: make(map[id.ChangeID][]change.Event)}
}

func (f *fakeEvents) Append(_ context.Context, changeID id.ChangeID, expected int64, evs []change.Event, _ []change.Effect) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appendCalls++
	if f.failAppend > 0 {
		f.failAppend--
		return 0, f.failAppendErr
	}
	existing := f.streams[changeID]
	var currentVersion int64
	if len(existing) > 0 {
		currentVersion = existing[len(existing)-1].Version
	}
	if currentVersion != expected {
		return 0, ports.ErrVersionConflict
	}
	for i, e := range evs {
		if e.Version != expected+int64(i+1) {
			return 0, fmt.Errorf("unexpected event version %d, want %d", e.Version, expected+int64(i+1))
		}
	}
	f.streams[changeID] = append(existing, evs...)
	return expected + int64(len(evs)), nil
}

func (f *fakeEvents) Load(_ context.Context, changeID id.ChangeID) ([]change.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]change.Event(nil), f.streams[changeID]...), nil
}

func (f *fakeEvents) setStream(changeID id.ChangeID, evs []change.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.streams[changeID] = append([]change.Event(nil), evs...)
}

// fakePRs implements prsync.PRs.
type fakePRs struct {
	mu  sync.Mutex
	prs map[change.Repo][]ports.PR
	err map[change.Repo]error
}

func newFakePRs() *fakePRs {
	return &fakePRs{prs: make(map[change.Repo][]ports.PR), err: make(map[change.Repo]error)}
}

func (f *fakePRs) OpenPRs(_ context.Context, repo change.Repo) ([]ports.PR, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.err[repo]; err != nil {
		return nil, err
	}
	return append([]ports.PR(nil), f.prs[repo]...), nil
}

// fakeIntake implements prsync.Intake.
type fakeIntake struct {
	mu       sync.Mutex
	changeID map[int]id.ChangeID
	err      map[int]error
	calls    []int
}

func newFakeIntake() *fakeIntake {
	return &fakeIntake{changeID: make(map[int]id.ChangeID), err: make(map[int]error)}
}

func (f *fakeIntake) HandlePR(_ context.Context, _ change.Repo, n int) (id.ChangeID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, n)
	if err := f.err[n]; err != nil {
		return "", err
	}
	cid, ok := f.changeID[n]
	if !ok {
		cid = id.ChangeID(fmt.Sprintf("PAY-%d", n))
	}
	return cid, nil
}

// fakeReviews implements prsync.Reviews.
type fakeReviews struct {
	mu      sync.Mutex
	reviews map[int][]prsync.PRReview
	err     map[int]error
}

func newFakeReviews() *fakeReviews {
	return &fakeReviews{reviews: make(map[int][]prsync.PRReview), err: make(map[int]error)}
}

func (f *fakeReviews) PRReviews(_ context.Context, _ change.Repo, n int) ([]prsync.PRReview, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.err[n]; err != nil {
		return nil, err
	}
	return append([]prsync.PRReview(nil), f.reviews[n]...), nil
}

// fakeChecks implements prsync.Checks.
type fakeChecks struct {
	mu     sync.Mutex
	checks map[id.SHA][]prsync.CheckRun
	err    map[id.SHA]error
}

func newFakeChecks() *fakeChecks {
	return &fakeChecks{checks: make(map[id.SHA][]prsync.CheckRun), err: make(map[id.SHA]error)}
}

func (f *fakeChecks) CheckRuns(_ context.Context, _ change.Repo, sha id.SHA) ([]prsync.CheckRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.err[sha]; err != nil {
		return nil, err
	}
	return append([]prsync.CheckRun(nil), f.checks[sha]...), nil
}

// fakeGate implements prsync.Gate.
type fakeGate struct {
	mu    sync.Mutex
	calls []id.ChangeID
	err   error
}

func (f *fakeGate) Evaluate(_ context.Context, changeID id.ChangeID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, changeID)
	return f.err
}

func (f *fakeGate) evaluated() []id.ChangeID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]id.ChangeID(nil), f.calls...)
}

type fakeClock struct{ at time.Time }

func (c fakeClock) Now() time.Time { return c.at }

type seqIDs struct {
	mu sync.Mutex
	n  int
}

func (s *seqIDs) NewEvent() change.EventID {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	return change.EventID(fmt.Sprintf("00000000-0000-4000-8000-%012d", s.n))
}

func (s *seqIDs) NewFinding() id.FindingID { return "" }
func (s *seqIDs) NewRun() id.RunID         { return "" }
func (s *seqIDs) NewCase() id.CaseID       { return "" }
func (s *seqIDs) NewTask() id.TaskID       { return "" }

type rig struct {
	repos   []change.Repo
	events  *fakeEvents
	prs     *fakePRs
	intake  *fakeIntake
	reviews *fakeReviews
	checks  *fakeChecks
	gate    *fakeGate
	ids     *seqIDs
	clock   fakeClock
	syncer  *prsync.Syncer
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{
		repos:   []change.Repo{testRepo},
		events:  newFakeEvents(),
		prs:     newFakePRs(),
		intake:  newFakeIntake(),
		reviews: newFakeReviews(),
		checks:  newFakeChecks(),
		gate:    &fakeGate{},
		ids:     &seqIDs{},
		clock:   fakeClock{at: testNow},
	}
	s, err := prsync.New(prsync.Config{
		Repos:   r.repos,
		PRs:     r.prs,
		Intake:  r.intake,
		Reviews: r.reviews,
		Checks:  r.checks,
		Gate:    r.gate,
		Events:  r.events,
		Clock:   r.clock,
		IDs:     r.ids,
	})
	require.NoError(t, err)
	r.syncer = s
	return r
}

func samplePR(n int, sha id.SHA, state ports.PRState) ports.PR {
	return ports.PR{
		Repo:    testRepo,
		Number:  n,
		Title:   fmt.Sprintf("PAY-%d implement feature", n),
		Branch:  fmt.Sprintf("feature/PAY-%d", n),
		Author:  authorLogin,
		BaseSHA: baseSHA,
		HeadSHA: sha,
		State:   state,
	}
}

func (r *rig) seedInitialPROpened(changeID id.ChangeID, n int, sha id.SHA) {
	pl := change.PROpened{Repo: testRepo, Number: n, HeadSHA: sha, Author: authorLogin, Ticket: changeID}
	raw, _ := json.Marshal(pl)
	r.events.setStream(changeID, []change.Event{
		{
			ID:            r.ids.NewEvent(),
			ChangeID:      changeID,
			Version:       1,
			Type:          change.EventPROpened,
			Actor:         change.Actor{Kind: change.ActorGitHub, ID: authorLogin},
			OccurredAt:    testNow,
			CorrelationID: "corr-1",
			Payload:       raw,
		},
	})
}

func TestFirstSyncAppendsApprovalAndCIOnce(t *testing.T) {
	r := newRig(t)
	pr := samplePR(10, head1, ports.PRStateOpen)
	r.prs.prs[testRepo] = []ports.PR{pr}
	changeID := id.ChangeID("PAY-10")
	r.intake.changeID[10] = changeID
	r.seedInitialPROpened(changeID, 10, head1)

	r.reviews.reviews[10] = []prsync.PRReview{
		{
			ID:          "rev-1",
			Login:       reviewerBob,
			State:       prsync.ReviewApproved,
			CommitSHA:   head1,
			SubmittedAt: testNow,
		},
	}
	r.checks.checks[head1] = []prsync.CheckRun{
		{
			ID:         "check-1",
			Name:       "build",
			Status:     prsync.CheckCompleted,
			Conclusion: change.CISuccess,
			SHA:        head1,
		},
	}

	res, err := r.syncer.SyncOnce(context.Background())
	require.NoError(t, err)
	require.Empty(t, res.Failed)
	require.Equal(t, 2, res.Appended)
	require.Equal(t, 1, res.Evaluated)

	evs, err := r.events.Load(context.Background(), changeID)
	require.NoError(t, err)
	require.Len(t, evs, 3) // Initial PR opened (v1) + approval (v2) + CI (v3)

	require.Equal(t, change.EventApprovalRecorded, evs[1].Type)
	var app change.ApprovalRecorded
	require.NoError(t, json.Unmarshal(evs[1].Payload, &app))
	require.Equal(t, reviewerBob, app.Login)
	require.Equal(t, head1, app.CommitSHA)
	require.Equal(t, change.ActorHuman, evs[1].Actor.Kind)

	require.Equal(t, change.EventCIReported, evs[2].Type)
	var ci change.CIReported
	require.NoError(t, json.Unmarshal(evs[2].Payload, &ci))
	require.Equal(t, "build", ci.Check)
	require.Equal(t, change.CISuccess, ci.Conclusion)
	require.Equal(t, head1, ci.SHA)

	require.Equal(t, []id.ChangeID{changeID}, r.gate.evaluated())
}

func TestSecondSyncAppendsNothing(t *testing.T) {
	r := newRig(t)
	pr := samplePR(10, head1, ports.PRStateOpen)
	r.prs.prs[testRepo] = []ports.PR{pr}
	changeID := id.ChangeID("PAY-10")
	r.intake.changeID[10] = changeID
	r.seedInitialPROpened(changeID, 10, head1)

	r.reviews.reviews[10] = []prsync.PRReview{
		{
			ID:          "rev-1",
			Login:       reviewerBob,
			State:       prsync.ReviewApproved,
			CommitSHA:   head1,
			SubmittedAt: testNow,
		},
	}
	r.checks.checks[head1] = []prsync.CheckRun{
		{
			ID:         "check-1",
			Name:       "build",
			Status:     prsync.CheckCompleted,
			Conclusion: change.CISuccess,
			SHA:        head1,
		},
	}

	// First sync
	_, err := r.syncer.SyncOnce(context.Background())
	require.NoError(t, err)

	// Second sync: identical input
	res2, err := r.syncer.SyncOnce(context.Background())
	require.NoError(t, err)
	require.Empty(t, res2.Failed)
	require.Equal(t, 0, res2.Appended)
	require.Equal(t, 0, res2.Evaluated)

	evs, err := r.events.Load(context.Background(), changeID)
	require.NoError(t, err)
	require.Len(t, evs, 3)                                        // No new events added
	require.Equal(t, []id.ChangeID{changeID}, r.gate.evaluated()) // Gate was not called on 2nd sync
}

func TestNewPushVoidsOldApproval(t *testing.T) {
	r := newRig(t)
	changeID := id.ChangeID("PAY-10")
	r.intake.changeID[10] = changeID
	r.seedInitialPROpened(changeID, 10, head1)

	// Review 1 was for old head1
	r.reviews.reviews[10] = []prsync.PRReview{
		{
			ID:          "rev-old",
			Login:       reviewerBob,
			State:       prsync.ReviewApproved,
			CommitSHA:   head1,
			SubmittedAt: testNow,
		},
	}

	// PR head is now pushed to head2
	pr := samplePR(10, head2, ports.PRStateOpen)
	r.prs.prs[testRepo] = []ports.PR{pr}

	res, err := r.syncer.SyncOnce(context.Background())
	require.NoError(t, err)
	require.Empty(t, res.Failed)
	require.Equal(t, 0, res.Appended) // Old head review ignored!

	evs, err := r.events.Load(context.Background(), changeID)
	require.NoError(t, err)
	require.Len(t, evs, 1) // Only initial PR opened
}

func TestDismissedApprovalIsInvalidated(t *testing.T) {
	r := newRig(t)
	pr := samplePR(10, head1, ports.PRStateOpen)
	r.prs.prs[testRepo] = []ports.PR{pr}
	changeID := id.ChangeID("PAY-10")
	r.intake.changeID[10] = changeID
	r.seedInitialPROpened(changeID, 10, head1)

	// Initial approval
	r.reviews.reviews[10] = []prsync.PRReview{
		{
			ID:          "rev-1",
			Login:       reviewerBob,
			State:       prsync.ReviewApproved,
			CommitSHA:   head1,
			SubmittedAt: testNow,
		},
	}
	_, err := r.syncer.SyncOnce(context.Background())
	require.NoError(t, err)

	// Later dismissed review
	r.reviews.reviews[10] = []prsync.PRReview{
		{
			ID:          "rev-1",
			Login:       reviewerBob,
			State:       prsync.ReviewApproved,
			CommitSHA:   head1,
			SubmittedAt: testNow,
		},
		{
			ID:          "rev-2",
			Login:       reviewerBob,
			State:       prsync.ReviewDismissed,
			CommitSHA:   head1,
			SubmittedAt: testNow.Add(time.Minute),
		},
	}

	res2, err := r.syncer.SyncOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, res2.Appended)

	evs, err := r.events.Load(context.Background(), changeID)
	require.NoError(t, err)
	require.Len(t, evs, 3) // opened, approval.recorded, approval.invalidated
	require.Equal(t, change.EventApprovalInvalidated, evs[2].Type)

	var inv change.ApprovalInvalidated
	require.NoError(t, json.Unmarshal(evs[2].Payload, &inv))
	require.Equal(t, reviewerBob, inv.Login)
	require.Equal(t, head1, inv.CommitSHA)
}

func TestAuthorApprovalIsIgnored(t *testing.T) {
	r := newRig(t)
	pr := samplePR(10, head1, ports.PRStateOpen)
	r.prs.prs[testRepo] = []ports.PR{pr}
	changeID := id.ChangeID("PAY-10")
	r.intake.changeID[10] = changeID
	r.seedInitialPROpened(changeID, 10, head1)

	// Author approves their own PR
	r.reviews.reviews[10] = []prsync.PRReview{
		{
			ID:          "rev-self",
			Login:       authorLogin,
			State:       prsync.ReviewApproved,
			CommitSHA:   head1,
			SubmittedAt: testNow,
		},
	}

	res, err := r.syncer.SyncOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, res.Appended)

	evs, err := r.events.Load(context.Background(), changeID)
	require.NoError(t, err)
	require.Len(t, evs, 1) // Only initial PR opened
}

func TestOldHeadCIResultIsIgnored(t *testing.T) {
	r := newRig(t)
	pr := samplePR(10, head2, ports.PRStateOpen) // Current head is head2
	r.prs.prs[testRepo] = []ports.PR{pr}
	changeID := id.ChangeID("PAY-10")
	r.intake.changeID[10] = changeID
	r.seedInitialPROpened(changeID, 10, head2)

	// CI check for old commit head1
	r.checks.checks[head1] = []prsync.CheckRun{
		{
			ID:         "check-old",
			Name:       "build",
			Status:     prsync.CheckCompleted,
			Conclusion: change.CISuccess,
			SHA:        head1,
		},
	}

	res, err := r.syncer.SyncOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, res.Appended)

	evs, err := r.events.Load(context.Background(), changeID)
	require.NoError(t, err)
	require.Len(t, evs, 1)
}

func TestInProgressCheckAppendsNothing(t *testing.T) {
	r := newRig(t)
	pr := samplePR(10, head1, ports.PRStateOpen)
	r.prs.prs[testRepo] = []ports.PR{pr}
	changeID := id.ChangeID("PAY-10")
	r.intake.changeID[10] = changeID
	r.seedInitialPROpened(changeID, 10, head1)

	r.checks.checks[head1] = []prsync.CheckRun{
		{
			ID:         "check-queued",
			Name:       "build",
			Status:     prsync.CheckQueued,
			Conclusion: "",
			SHA:        head1,
		},
		{
			ID:         "check-running",
			Name:       "lint",
			Status:     prsync.CheckInProgress,
			Conclusion: "",
			SHA:        head1,
		},
		{
			ID:         "check-running-with-conclusion",
			Name:       "test",
			Status:     prsync.CheckInProgress,
			Conclusion: change.CISuccess,
			SHA:        head1,
		},
	}

	res, err := r.syncer.SyncOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, res.Appended)

	evs, err := r.events.Load(context.Background(), changeID)
	require.NoError(t, err)
	require.Len(t, evs, 1)
}

func TestGateCalledOnlyForChangedPRs(t *testing.T) {
	r := newRig(t)
	pr := samplePR(10, head1, ports.PRStateOpen)
	r.prs.prs[testRepo] = []ports.PR{pr}
	changeID := id.ChangeID("PAY-10")
	r.intake.changeID[10] = changeID
	r.seedInitialPROpened(changeID, 10, head1)

	// Sync 1: nothing new on PR -> gate not called
	res1, err := r.syncer.SyncOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, res1.Evaluated)
	require.Empty(t, r.gate.evaluated())

	// Add an approval -> sync 2 appends event and evaluates gate
	r.reviews.reviews[10] = []prsync.PRReview{
		{
			ID:          "rev-bob",
			Login:       reviewerBob,
			State:       prsync.ReviewApproved,
			CommitSHA:   head1,
			SubmittedAt: testNow,
		},
	}
	res2, err := r.syncer.SyncOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, res2.Evaluated)
	require.Equal(t, []id.ChangeID{changeID}, r.gate.evaluated())

	// Sync 3: unchanged -> gate NOT called
	res3, err := r.syncer.SyncOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, res3.Evaluated)
	require.Equal(t, []id.ChangeID{changeID}, r.gate.evaluated())
}

func TestOneFailingPRDoesNotStopOthers(t *testing.T) {
	r := newRig(t)
	pr1 := samplePR(1, head1, ports.PRStateOpen)
	pr2 := samplePR(2, head1, ports.PRStateOpen)
	r.prs.prs[testRepo] = []ports.PR{pr1, pr2}

	changeID2 := id.ChangeID("PAY-2")
	r.intake.changeID[2] = changeID2
	r.seedInitialPROpened(changeID2, 2, head1)

	// PR 1 fails in intake
	r.intake.err[1] = errors.New("simulated intake failure")

	// PR 2 has an approval
	r.reviews.reviews[2] = []prsync.PRReview{
		{
			ID:          "rev-2",
			Login:       reviewerBob,
			State:       prsync.ReviewApproved,
			CommitSHA:   head1,
			SubmittedAt: testNow,
		},
	}

	res, err := r.syncer.SyncOnce(context.Background())
	require.NoError(t, err)
	require.Len(t, res.Failed, 1)
	require.Equal(t, 1, res.Failed[0].Number)
	require.Equal(t, testRepo, res.Failed[0].Repo)
	require.ErrorContains(t, res.Failed[0].Err, "simulated intake failure")

	// PR 2 still synced
	require.Equal(t, 1, res.Appended)
	evs, err := r.events.Load(context.Background(), changeID2)
	require.NoError(t, err)
	require.Len(t, evs, 2)
}

func TestVersionConflictRetried(t *testing.T) {
	r := newRig(t)
	pr := samplePR(10, head1, ports.PRStateOpen)
	r.prs.prs[testRepo] = []ports.PR{pr}
	changeID := id.ChangeID("PAY-10")
	r.intake.changeID[10] = changeID
	r.seedInitialPROpened(changeID, 10, head1)

	r.reviews.reviews[10] = []prsync.PRReview{
		{
			ID:          "rev-bob",
			Login:       reviewerBob,
			State:       prsync.ReviewApproved,
			CommitSHA:   head1,
			SubmittedAt: testNow,
		},
	}

	// Fail the first two append attempts with version conflict, succeed on 3rd
	r.events.failAppend = 2
	r.events.failAppendErr = ports.ErrVersionConflict

	res, err := r.syncer.SyncOnce(context.Background())
	require.NoError(t, err)
	require.Empty(t, res.Failed)
	require.Equal(t, 1, res.Appended)
	require.Equal(t, 3, r.events.appendCalls)

	evs, err := r.events.Load(context.Background(), changeID)
	require.NoError(t, err)
	require.Len(t, evs, 2)
}

func TestClosedPROnce(t *testing.T) {
	r := newRig(t)
	prClosed := samplePR(10, head1, ports.PRStateClosed)
	prClosed.Merged = true
	r.prs.prs[testRepo] = []ports.PR{prClosed}

	changeID := id.ChangeID("PAY-10")
	r.intake.changeID[10] = changeID
	r.seedInitialPROpened(changeID, 10, head1)

	// First sync on closed PR: appends pr.closed
	res1, err := r.syncer.SyncOnce(context.Background())
	require.NoError(t, err)
	require.Empty(t, res1.Failed)
	require.Equal(t, 1, res1.Appended)

	evs, err := r.events.Load(context.Background(), changeID)
	require.NoError(t, err)
	require.Len(t, evs, 2)
	require.Equal(t, change.EventPRClosed, evs[1].Type)

	var cl change.PRClosed
	require.NoError(t, json.Unmarshal(evs[1].Payload, &cl))
	require.True(t, cl.Merged)
	require.Equal(t, 10, cl.Number)
	require.Equal(t, testRepo, cl.Repo)
	require.Equal(t, head1, cl.HeadSHA)

	// Second sync on closed PR: appends nothing
	res2, err := r.syncer.SyncOnce(context.Background())
	require.NoError(t, err)
	require.Empty(t, res2.Failed)
	require.Equal(t, 0, res2.Appended)

	evs2, err := r.events.Load(context.Background(), changeID)
	require.NoError(t, err)
	require.Len(t, evs2, 2)
}

func TestTransportErrorOnListingReturnsError(t *testing.T) {
	r := newRig(t)
	r.prs.err[testRepo] = ports.ErrUnavailable

	_, err := r.syncer.SyncOnce(context.Background())
	require.ErrorIs(t, err, ports.ErrUnavailable)
}

func TestNewRequiresAllPorts(t *testing.T) {
	r := newRig(t)
	cfg := prsync.Config{
		Repos:   r.repos,
		PRs:     r.prs,
		Intake:  r.intake,
		Reviews: r.reviews,
		Checks:  r.checks,
		Gate:    r.gate,
		Events:  r.events,
		Clock:   r.clock,
		IDs:     r.ids,
	}

	_, err := prsync.New(cfg)
	require.NoError(t, err)

	bad := cfg
	bad.Events = nil
	_, err = prsync.New(bad)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Events")

	bad = cfg
	bad.Repos = nil
	_, err = prsync.New(bad)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Repos")
}
