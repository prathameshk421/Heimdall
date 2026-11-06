package ports_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Each port is satisfied by a tiny in-test fake. The compile-time assertions prove the interfaces can be implemented;
// the tests call each method through the interface to prove the signatures are usable as the consumer needs them.

var (
	ctx      = context.Background()
	shaA     = id.SHA("9f1c2ab")
	repo     = change.Repo("acme/pay-demo")
	key      = change.EffectKey("room_post:PAY-142:1:msg-1")
	blobRef  = workflow.BlobRef("blob:sha256:" + strings.Repeat("a", 64))
	instant  = time.Date(2026, 11, 4, 9, 30, 0, 0, time.UTC)
	ticketID = id.ChangeID("PAY-142")
)

type fakeStore struct{}

var _ ports.EventStore = fakeStore{}

func (fakeStore) Append(_ context.Context, _ id.ChangeID, expected int64, evs []change.Event, _ []change.Effect) (int64, error) {
	return expected + int64(len(evs)), nil
}
func (fakeStore) Load(_ context.Context, _ id.ChangeID) ([]change.Event, error) { return nil, nil }

type fakeOutbox struct{ done []change.EffectKey }

var _ ports.Outbox = (*fakeOutbox)(nil)

func (*fakeOutbox) Pending(_ context.Context, limit int) ([]change.Effect, error) {
	return make([]change.Effect, limit), nil
}
func (f *fakeOutbox) Done(_ context.Context, k change.EffectKey) error {
	f.done = append(f.done, k)
	return nil
}
func (*fakeOutbox) Claim(_ context.Context, limit int, _ time.Duration) ([]change.Effect, error) {
	return make([]change.Effect, limit), nil
}
func (*fakeOutbox) ClaimEffect(_ context.Context, _ change.EffectKey, _ time.Duration) (*change.Effect, error) {
	return nil, nil
}
func (*fakeOutbox) Start(_ context.Context, _ change.EffectKey) error          { return nil }
func (*fakeOutbox) Fail(_ context.Context, _ change.EffectKey, _ string) error { return nil }

type fakePRs struct{}

var _ ports.PRReader = fakePRs{}

func (fakePRs) PR(_ context.Context, _ change.Repo, n int) (ports.PR, error) {
	return ports.PR{Number: n}, nil
}
func (fakePRs) Files(_ context.Context, _ change.Repo, _ int) ([]ports.ChangedFile, error) {
	return []ports.ChangedFile{{Path: "a.go"}}, nil
}
func (fakePRs) OpenPRs(_ context.Context, _ change.Repo) ([]ports.PR, error) { return nil, nil }

type fakeComments struct{ last string }

var _ ports.PRComments = (*fakeComments)(nil)

func (f *fakeComments) Comment(_ context.Context, _ change.Repo, _ int, _ change.EffectKey, body string) error {
	f.last = body
	return nil
}

type fakeRepos struct{}

var _ ports.RepoReader = fakeRepos{}

func (fakeRepos) DefaultHead(_ context.Context, _ change.Repo) (id.SHA, error) {
	return shaA, nil
}

type fakeBranches struct{}

var _ ports.BranchWriter = fakeBranches{}

func (fakeBranches) EnsureBranch(_ context.Context, _ change.Repo, _ id.SHA, _ string) error {
	return nil
}
func (fakeBranches) CommitFiles(_ context.Context, _ change.Repo, _, _ string, _ []ports.File) (id.SHA, error) {
	return shaA, nil
}

type fakeChecks struct{ got ports.Check }

var _ ports.CheckWriter = (*fakeChecks)(nil)

func (f *fakeChecks) SetCheck(_ context.Context, _ change.Repo, _ id.SHA, c ports.Check) error {
	f.got = c
	return nil
}

type fakeReviews struct{}

var _ ports.ReviewReader = fakeReviews{}

func (fakeReviews) PRReviews(_ context.Context, _ change.Repo, _ int) ([]ports.PRReview, error) {
	return []ports.PRReview{{
		ID:          "rev-1",
		Login:       "alice",
		State:       ports.ReviewStateApproved,
		CommitSHA:   shaA,
		SubmittedAt: instant,
	}}, nil
}

type fakeCheckRuns struct{}

var _ ports.CheckReader = fakeCheckRuns{}

func (fakeCheckRuns) CheckRuns(_ context.Context, _ change.Repo, _ id.SHA) ([]ports.CheckRun, error) {
	return []ports.CheckRun{{
		ID:         "chk-1",
		Name:       "ci/test",
		Status:     ports.CheckCompleted,
		Conclusion: change.CISuccess,
		SHA:        shaA,
	}}, nil
}

type fakeCode struct{}

var _ ports.CodeReader = fakeCode{}

func (fakeCode) TreeHash(_ context.Context, _ change.Repo, _ id.SHA, _ string) (id.SHA, error) {
	return "7d3e9a1", nil
}
func (fakeCode) ReadFile(_ context.Context, _ change.Repo, _ id.SHA, _ string) ([]byte, error) {
	return []byte("version: 1\n"), nil
}
func (fakeCode) Tarball(_ context.Context, _ change.Repo, _ id.SHA) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader([]byte("tar"))), nil
}

type fakeAgents struct{}

var _ ports.AgentRuntime = fakeAgents{}

func (fakeAgents) Wake(_ context.Context, _ ports.AgentRef, _ workflow.TaskEnvelope) (ports.WakeResult, error) {
	return ports.WakeResult{Accepted: true}, nil
}
func (fakeAgents) Liveness(_ context.Context, _ ports.AgentRef) (ports.Liveness, error) {
	return ports.LivenessLive, nil
}

type fakeSandbox struct{}

var _ ports.Sandbox = fakeSandbox{}

func (fakeSandbox) Run(_ context.Context, _ ports.SandboxJob) (ports.SandboxResult, error) {
	return ports.SandboxResult{Passed: 1, LogRef: blobRef}, nil
}

type fakeMemory struct{ episodes int }

var _ ports.Memory = (*fakeMemory)(nil)

func (f *fakeMemory) AddEpisode(_ context.Context, _ ports.Episode) error {
	f.episodes++
	return nil
}
func (*fakeMemory) Search(_ context.Context, q ports.MemoryQuery) ([]ports.MemoryItem, error) {
	return []ports.MemoryItem{{Ref: "mem_1", Text: q.Question, Label: ports.LabelConfirmed}}, nil
}

type fakeChat struct{}

var _ ports.Chat = fakeChat{}

func (fakeChat) OpenRoom(_ context.Context, _ ports.RoomRequest) (change.RoomID, error) {
	return "room-42", nil
}
func (fakeChat) Post(_ context.Context, _ change.RoomID, _ change.EffectKey, _ string) error {
	return nil
}
func (fakeChat) Read(_ context.Context, _ change.RoomID, after ports.Cursor) ([]ports.Message, ports.Cursor, error) {
	return nil, after + "+1", nil
}

type fakeTickets struct{}

var _ ports.TicketReader = fakeTickets{}

func (fakeTickets) Changes(_ context.Context, since ports.Cursor) ([]ports.TicketChange, ports.Cursor, error) {
	return nil, since, nil
}
func (fakeTickets) Ticket(_ context.Context, key id.ChangeID) (ports.Ticket, error) {
	return ports.Ticket{Key: key}, nil
}

type fakeTicketWriter struct{}

var _ ports.TicketWriter = fakeTicketWriter{}

func (fakeTicketWriter) Comment(_ context.Context, _ id.ChangeID, _ change.EffectKey, _ string) error {
	return nil
}
func (fakeTicketWriter) CreateTicket(_ context.Context, _ ports.TicketDraft) (id.ChangeID, error) {
	return "KAN-9", nil
}

type fakeBlobs struct{}

var _ ports.BlobStore = fakeBlobs{}

func (fakeBlobs) Put(_ context.Context, _ ports.BlobClass, r io.Reader) (workflow.BlobRef, error) {
	_, _ = io.Copy(io.Discard, r)
	return blobRef, nil
}
func (fakeBlobs) Get(_ context.Context, _ workflow.BlobRef) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("log")), nil
}

type fakeClock struct{}

var _ ports.Clock = fakeClock{}

func (fakeClock) Now() time.Time { return instant }

type fakeIDs struct{}

var _ ports.IDs = fakeIDs{}

func (fakeIDs) NewFinding() id.FindingID { return "fnd_01HZX3K9QW7M2P5R8T0V4YB6CD" }
func (fakeIDs) NewRun() id.RunID         { return "run_01HZX3K9QW7M2P5R8T0V4YB6CD" }
func (fakeIDs) NewCase() id.CaseID       { return "case_01HZX3K9QW7M2P5R8T0V4YB6CD" }
func (fakeIDs) NewTask() id.TaskID       { return "tsk_01HZX3K9QW7M2P5R8T0V4YB6CD" }
func (fakeIDs) NewEvent() change.EventID { return "3f1c9a52-8d2b-4e7a-9c11-0b6f5d4e3a21" }

func TestEventStoreAndOutboxContracts(t *testing.T) {
	var store ports.EventStore = fakeStore{}
	v, err := store.Append(ctx, ticketID, 3, make([]change.Event, 2), nil)
	require.NoError(t, err)
	require.Equal(t, int64(5), v)

	var box ports.Outbox = &fakeOutbox{}
	fx, err := box.Pending(ctx, 4)
	require.NoError(t, err)
	require.Len(t, fx, 4)
	require.NoError(t, box.Start(ctx, key))
	require.NoError(t, box.Done(ctx, key))
	require.NoError(t, box.Fail(ctx, key, "timeout"))
}

func TestVCSContractsAreSplitByConsumer(t *testing.T) {
	var reader ports.PRReader = fakePRs{}
	pr, err := reader.PR(ctx, repo, 7)
	require.NoError(t, err)
	require.Equal(t, 7, pr.Number)
	files, err := reader.Files(ctx, repo, 7)
	require.NoError(t, err)
	require.Len(t, files, 1)
	_, err = reader.OpenPRs(ctx, repo)
	require.NoError(t, err)

	var comments ports.PRComments = &fakeComments{}
	require.NoError(t, comments.Comment(ctx, repo, 7, key, "hello"))

	var repos ports.RepoReader = fakeRepos{}
	head, err := repos.DefaultHead(ctx, repo)
	require.NoError(t, err)
	require.Equal(t, shaA, head)

	var branches ports.BranchWriter = fakeBranches{}
	require.NoError(t, branches.EnsureBranch(ctx, repo, shaA, "PAY-142-tests"))
	got, err := branches.CommitFiles(ctx, repo, "PAY-142-tests", "tests", []ports.File{{Path: "t.go"}})
	require.NoError(t, err)
	require.Equal(t, shaA, got)

	var checks ports.CheckWriter = &fakeChecks{}
	require.NoError(t, checks.SetCheck(ctx, repo, shaA, ports.Check{Name: "heimdall/gate", Status: ports.CheckQueued}))

	var reviews ports.ReviewReader = fakeReviews{}
	revs, err := reviews.PRReviews(ctx, repo, 7)
	require.NoError(t, err)
	require.Len(t, revs, 1)
	require.NoError(t, revs[0].Validate())

	var checkRuns ports.CheckReader = fakeCheckRuns{}
	runs, err := checkRuns.CheckRuns(ctx, repo, shaA)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.NoError(t, runs[0].Validate())

	var code ports.CodeReader = fakeCode{}
	tree, err := code.TreeHash(ctx, repo, shaA, "tests/heimdall/PAY-142")
	require.NoError(t, err)
	require.Equal(t, id.SHA("7d3e9a1"), tree)
	rc, err := code.Tarball(ctx, repo, shaA)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
}

func TestRuntimeSandboxAndMemoryContracts(t *testing.T) {
	var agents ports.AgentRuntime = fakeAgents{}
	res, err := agents.Wake(ctx, ports.AgentRef{Name: "bob-reviewer", Role: workflow.RoleReviewer}, workflow.TaskEnvelope{})
	require.NoError(t, err)
	require.True(t, res.Accepted)
	live, err := agents.Liveness(ctx, ports.AgentRef{Name: "bob-reviewer", Role: workflow.RoleReviewer})
	require.NoError(t, err)
	require.True(t, live.Valid())

	var sb ports.Sandbox = fakeSandbox{}
	out, err := sb.Run(ctx, ports.SandboxJob{Kind: ports.SandboxRedFirst})
	require.NoError(t, err)
	require.Equal(t, 1, out.Passed)

	var mem ports.Memory = &fakeMemory{}
	require.NoError(t, mem.AddEpisode(ctx, ports.Episode{Key: key, Source: "decision", Status: ports.EpisodeConfirmed, GroupID: "payments"}))
	items, err := mem.Search(ctx, ports.MemoryQuery{Question: "refunds", Groups: []string{"payments"}, Limit: 5})
	require.NoError(t, err)
	require.Equal(t, ports.LabelConfirmed, items[0].Label)
}

func TestChatTrackerBlobsClockAndIDsContracts(t *testing.T) {
	var chat ports.Chat = fakeChat{}
	room, err := chat.OpenRoom(ctx, ports.RoomRequest{ChangeID: ticketID, Title: "PAY-142", Key: key})
	require.NoError(t, err)
	require.Equal(t, change.RoomID("room-42"), room)
	require.NoError(t, chat.Post(ctx, room, key, "heimdall start KAN-1"))
	_, next, err := chat.Read(ctx, room, "c0")
	require.NoError(t, err)
	require.Equal(t, ports.Cursor("c0+1"), next)

	var reader ports.TicketReader = fakeTickets{}
	_, cursor, err := reader.Changes(ctx, "c0")
	require.NoError(t, err)
	require.Equal(t, ports.Cursor("c0"), cursor)
	tk, err := reader.Ticket(ctx, ticketID)
	require.NoError(t, err)
	require.Equal(t, ticketID, tk.Key)

	var writer ports.TicketWriter = fakeTicketWriter{}
	require.NoError(t, writer.Comment(ctx, ticketID, key, "note"))
	created, err := writer.CreateTicket(ctx, ports.TicketDraft{Title: "rejected"})
	require.NoError(t, err)
	require.Equal(t, id.ChangeID("KAN-9"), created)

	var blobs ports.BlobStore = fakeBlobs{}
	ref, err := blobs.Put(ctx, ports.BlobLog, strings.NewReader("log"))
	require.NoError(t, err)
	require.Equal(t, blobRef, ref)
	rc, err := blobs.Get(ctx, ref)
	require.NoError(t, err)
	require.NoError(t, rc.Close())

	var clock ports.Clock = fakeClock{}
	require.True(t, clock.Now().Equal(instant))
	var ids ports.IDs = fakeIDs{}
	require.Equal(t, id.TaskID("tsk_01HZX3K9QW7M2P5R8T0V4YB6CD"), ids.NewTask())
	require.NotEmpty(t, ids.NewEvent().String())
}

func TestPRReviewValidate(t *testing.T) {
	good := ports.PRReview{
		ID:          "rev_123",
		Login:       "alice",
		State:       ports.ReviewStateApproved,
		CommitSHA:   shaA,
		SubmittedAt: instant,
	}
	require.NoError(t, good.Validate())

	for _, state := range []ports.ReviewState{
		ports.ReviewStateApproved,
		ports.ReviewStateChangesRequested,
		ports.ReviewStateCommented,
		ports.ReviewStateDismissed,
	} {
		r := good
		r.State = state
		require.NoError(t, r.Validate())
	}

	badID := good
	badID.ID = ""
	require.Error(t, badID.Validate())

	badLogin := good
	badLogin.Login = ""
	require.Error(t, badLogin.Validate())

	badState := good
	badState.State = "invalid_state"
	require.Error(t, badState.Validate())

	badSHA := good
	badSHA.CommitSHA = "not-a-sha"
	require.Error(t, badSHA.Validate())

	zeroTime := good
	zeroTime.SubmittedAt = time.Time{}
	require.Error(t, zeroTime.Validate())
}

func TestCheckRunValidate(t *testing.T) {
	good := ports.CheckRun{
		ID:         "chk_123",
		Name:       "ci/test",
		Status:     ports.CheckCompleted,
		Conclusion: change.CISuccess,
		SHA:        shaA,
	}
	require.NoError(t, good.Validate())

	inProg := ports.CheckRun{
		ID:     "chk_123",
		Name:   "ci/test",
		Status: ports.CheckInProgress,
		SHA:    shaA,
	}
	require.NoError(t, inProg.Validate())

	queued := ports.CheckRun{
		ID:     "chk_123",
		Name:   "ci/test",
		Status: ports.CheckQueued,
		SHA:    shaA,
	}
	require.NoError(t, queued.Validate())

	inProgWithConclusion := inProg
	inProgWithConclusion.Conclusion = change.CISuccess
	require.Error(t, inProgWithConclusion.Validate())

	queuedWithConclusion := queued
	queuedWithConclusion.Conclusion = change.CISuccess
	require.Error(t, queuedWithConclusion.Validate())

	completedNoConclusion := good
	completedNoConclusion.Conclusion = ""
	require.Error(t, completedNoConclusion.Validate())

	completedBadConclusion := good
	completedBadConclusion.Conclusion = "exploded"
	require.Error(t, completedBadConclusion.Validate())

	badStatus := good
	badStatus.Status = "waiting"
	require.Error(t, badStatus.Validate())

	badID := good
	badID.ID = ""
	require.Error(t, badID.Validate())

	badName := good
	badName.Name = ""
	require.Error(t, badName.Validate())

	badSHA := good
	badSHA.SHA = "bad-sha"
	require.Error(t, badSHA.Validate())
}
