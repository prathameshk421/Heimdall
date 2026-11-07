package effects_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/effects"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const (
	ticket = id.ChangeID("KAN-7")
	repo   = change.Repo("aarush323/heimdall-fixture-pay")
	base   = id.SHA("0123456789abcdef0123456789abcdef01234567")
	canary = "CANARY-7f3a9c-do-not-print"
)

var (
	now        = time.Date(2026, 11, 5, 9, 30, 0, 0, time.UTC)
	errTracker = errors.New("tracker unreachable")
)

// Compile-time checks: the executors are built from the ports and the ingest interface.
var (
	_ effects.Ingest     = (*fakeIngest)(nil)
	_ effects.BaseReader = (*fakeBase)(nil)
	_ ports.TicketReader = (*fakeTickets)(nil)
	_ ports.Chat         = (*fakeChat)(nil)
	_ ports.EventStore   = (*fakeStore)(nil)
	_ ports.BranchWriter = (*fakeBranches)(nil)
	_ ports.IDs          = (*fakeIDs)(nil)
	_ ports.Clock        = fixedClock{}
)

type fixedClock struct{}

func (fixedClock) Now() time.Time { return now }

type fakeIDs struct{ n int }

func (f *fakeIDs) NewEvent() change.EventID {
	f.n++
	return change.EventID(fmt.Sprintf("20000000-0000-4000-8000-%012d", f.n))
}
func (f *fakeIDs) NewFinding() id.FindingID { return "fnd_01ARZ3NDEKTSV4RRFFQ69G5FAV" }
func (f *fakeIDs) NewRun() id.RunID         { return "run_01ARZ3NDEKTSV4RRFFQ69G5FAW" }
func (f *fakeIDs) NewCase() id.CaseID       { return "cas_01ARZ3NDEKTSV4RRFFQ69G5FAX" }
func (f *fakeIDs) NewTask() id.TaskID       { return "tsk_01ARZ3NDEKTSV4RRFFQ69G5FAV" }

type fakeTickets struct {
	ticket ports.Ticket
	err    error
	calls  int
}

func (f *fakeTickets) Changes(context.Context, ports.Cursor) ([]ports.TicketChange, ports.Cursor, error) {
	return nil, "", nil
}

func (f *fakeTickets) Ticket(_ context.Context, _ id.ChangeID) (ports.Ticket, error) {
	f.calls++
	if f.err != nil {
		return ports.Ticket{}, f.err
	}
	return f.ticket, nil
}

type fakeChat struct {
	err      error
	requests []ports.RoomRequest
	room     change.RoomID
}

func (f *fakeChat) OpenRoom(_ context.Context, r ports.RoomRequest) (change.RoomID, error) {
	f.requests = append(f.requests, r)
	if f.err != nil {
		return "", f.err
	}
	return f.room, nil
}

func (f *fakeChat) Post(context.Context, change.RoomID, change.EffectKey, string) error { return nil }

func (f *fakeChat) Read(context.Context, change.RoomID, ports.Cursor) ([]ports.Message, ports.Cursor, error) {
	return nil, "", nil
}

type fakeStore struct {
	events []change.Event
	err    error
}

func (f *fakeStore) Append(context.Context, id.ChangeID, int64, []change.Event, []change.Effect) (int64, error) {
	return 0, errors.New("append is not used by the executors")
}

func (f *fakeStore) Load(_ context.Context, _ id.ChangeID) ([]change.Event, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.events, nil
}

type fakeIngest struct {
	err     error
	calls   int
	change  id.ChangeID
	written []change.Event
}

func (f *fakeIngest) Ingest(_ context.Context, c id.ChangeID, evs []change.Event) error {
	f.calls++
	f.change = c
	f.written = append(f.written, evs...)
	return f.err
}

type fakeBase struct {
	sha   id.SHA
	err   error
	calls int
}

func (f *fakeBase) DefaultHead(_ context.Context, _ change.Repo) (id.SHA, error) {
	f.calls++
	return f.sha, f.err
}

type fakeBranches struct {
	err   error
	calls int
	repo  change.Repo
	from  id.SHA
	name  string
	key   string
}

func (f *fakeBranches) EnsureBranch(_ context.Context, r change.Repo, from id.SHA, name string, key string) error {
	f.calls++
	f.repo, f.from, f.name, f.key = r, from, name, key
	return f.err
}

func (f *fakeBranches) CommitFiles(context.Context, change.Repo, string, string, []ports.File) (id.SHA, error) {
	return "", errors.New("commits are not used by the executors")
}

// triggeredEvent is change.triggered at version 1, the cause of the room request.
func triggeredEvent(t *testing.T) change.Event {
	t.Helper()
	return storedEvent(t, "10000000-0000-4000-8000-000000000001", 1, change.EventChangeTriggered,
		change.Actor{Kind: change.ActorJira, ID: "jira"}, change.Triggered{
			Ticket: ticket, Repo: repo, Assignee: "ana", Source: change.TriggerJira,
		})
}

// roomOpenedEvent is room.opened at version 2, the cause of the branch.
func roomOpenedEvent(t *testing.T) change.Event {
	t.Helper()
	return storedEvent(t, "10000000-0000-4000-8000-000000000002", 2, change.EventRoomOpened,
		change.Actor{Kind: change.ActorSwitch, ID: "switch"}, change.RoomOpened{RoomID: "room-7"})
}

// branchEnsuredEvent is branch.ensured at version 3.
func branchEnsuredEvent(t *testing.T) change.Event {
	t.Helper()
	return storedEvent(t, "10000000-0000-4000-8000-000000000003", 3, change.EventBranchEnsured,
		change.Actor{Kind: change.ActorGitHub, ID: "github"}, change.BranchEnsured{
			Repo: repo, Branch: "KAN-7-fix-refunds", BaseSHA: base,
		})
}

func storedEvent(t *testing.T, eid string, version int64, typ change.EventType, actor change.Actor, p any) change.Event {
	t.Helper()
	raw, err := json.Marshal(p)
	require.NoError(t, err)
	return change.Event{
		ID:            change.EventID(eid),
		ChangeID:      ticket,
		Version:       version,
		Type:          typ,
		Actor:         actor,
		OccurredAt:    now.Add(-time.Minute),
		CorrelationID: "corr-7",
		Payload:       raw,
	}
}

// roomEffect is the room_open effect the open-room step builds for the trigger of version 1.
func roomEffect(t *testing.T, invite ...string) change.Effect {
	t.Helper()
	key, err := change.IdempotencyKey(ticket, 1, change.EffectRoomOpen, "room")
	require.NoError(t, err)
	raw, err := json.Marshal(change.RoomOpenPayload{Ticket: ticket, Invite: invite})
	require.NoError(t, err)
	return change.Effect{Key: key, Kind: change.EffectRoomOpen, Target: string(ticket), Payload: raw}
}

// branchEffect is the github_branch effect the branch step builds for the room of version 2.
func branchEffect(t *testing.T, template string) change.Effect {
	t.Helper()
	key, err := change.IdempotencyKey(ticket, 2, change.EffectGitHubBranch, "branch")
	require.NoError(t, err)
	raw, err := json.Marshal(change.BranchPayload{Repo: repo, Ticket: ticket, Template: template})
	require.NoError(t, err)
	return change.Effect{Key: key, Kind: change.EffectGitHubBranch, Target: string(ticket), Payload: raw}
}

// kit is one set of fakes and the two executors built on them.
type kit struct {
	tickets  *fakeTickets
	chat     *fakeChat
	store    *fakeStore
	ingest   *fakeIngest
	base     *fakeBase
	branches *fakeBranches
	room     effects.RoomOpen
	branch   effects.GitHubBranch
}

func newKit(t *testing.T, events ...change.Event) *kit {
	t.Helper()
	k := &kit{
		tickets:  &fakeTickets{ticket: ports.Ticket{Key: ticket, Title: "Fix refunds on partial capture", Status: "In Progress"}},
		chat:     &fakeChat{room: "room-7"},
		store:    &fakeStore{events: events},
		ingest:   &fakeIngest{},
		base:     &fakeBase{sha: base},
		branches: &fakeBranches{},
	}
	room, err := effects.NewRoomOpen(effects.RoomOpenDeps{
		Tickets: k.tickets, Chat: k.chat, Store: k.store, Ingest: k.ingest, IDs: &fakeIDs{}, Clock: fixedClock{},
	})
	require.NoError(t, err)
	k.room = room
	branch, err := effects.NewGitHubBranch(effects.GitHubBranchDeps{
		Tickets: k.tickets, Base: k.base, Branches: k.branches, Store: k.store, Ingest: k.ingest,
		IDs: &fakeIDs{}, Clock: fixedClock{},
	})
	require.NoError(t, err)
	k.branch = branch
	return k
}

func TestRoomOpenAsksForTheWorkflowAgentsToBeInTheRoom(t *testing.T) {
	k := newKit(t, triggeredEvent(t))
	room, err := effects.NewRoomOpen(effects.RoomOpenDeps{
		Tickets: k.tickets, Chat: k.chat, Store: k.store, Ingest: k.ingest, IDs: &fakeIDs{}, Clock: fixedClock{},
		Agents: []string{"test-author", "reviewer-1", "resolver"},
	})
	require.NoError(t, err)

	require.NoError(t, room.Execute(context.Background(), roomEffect(t, "role:test-author", "person:ana")))
	require.Len(t, k.chat.requests, 1)
	require.Equal(t, []string{"test-author", "reviewer-1", "resolver"}, k.chat.requests[0].Agents,
		"an agent that is not in the room never receives its @mention")
}

func TestRoomOpenPassesEffectKeyAsIdempotencyKeyAndTitleFromTracker(t *testing.T) {
	k := newKit(t, triggeredEvent(t))
	fx := roomEffect(t, "role:test-author", "person:ana")

	require.NoError(t, k.room.Execute(context.Background(), fx))
	require.Len(t, k.chat.requests, 1)
	req := k.chat.requests[0]
	require.Equal(t, fx.Key, req.Key, "the effect key is the idempotency key of the room request")
	require.Equal(t, ticket, req.ChangeID)
	require.Equal(t, "KAN-7 Fix refunds on partial capture", req.Title, "the title leads with the ticket key")
}

// Found live: KAN-9 and KAN-11 had the same summary, and OpenRoom adopts an open room by title, so both changes shared
// one room and one Discord channel. The ticket key in the title keeps each change in its own room.
func TestTwoTicketsWithTheSameSummaryGetDifferentRoomTitles(t *testing.T) {
	first := newKit(t, triggeredEvent(t))
	require.NoError(t, first.room.Execute(context.Background(), roomEffect(t, "person:ana")))
	second := newKit(t, triggeredEvent(t))
	second.tickets.ticket.Key = "KAN-8"
	require.NoError(t, second.room.Execute(context.Background(), roomEffect(t, "person:ana")))
	require.Equal(t, first.tickets.ticket.Title, second.tickets.ticket.Title, "same summary")
	require.NotEqual(t, first.chat.requests[0].Title, second.chat.requests[0].Title)
}

func TestRoomOpenIngestsRoomOpenedCausedByTheTrigger(t *testing.T) {
	trig := triggeredEvent(t)
	k := newKit(t, trig)

	require.NoError(t, k.room.Execute(context.Background(), roomEffect(t, "person:ana")))
	require.Equal(t, 1, k.ingest.calls)
	require.Equal(t, ticket, k.ingest.change)
	require.Len(t, k.ingest.written, 1)

	ev := k.ingest.written[0]
	require.Equal(t, change.EventRoomOpened, ev.Type)
	require.Equal(t, change.ActorSwitch, ev.Actor.Kind, "the room comes from the chat adapter, which is the switch actor")
	require.Equal(t, change.ActorID("switch"), ev.Actor.ID)
	require.NotNil(t, ev.CausationID)
	require.Equal(t, trig.ID, *ev.CausationID)
	require.Equal(t, trig.CorrelationID, ev.CorrelationID)
	require.Equal(t, ticket, ev.ChangeID)
	require.True(t, ev.OccurredAt.Equal(now))
	require.Zero(t, ev.Version, "the engine numbers the event when it stores it")

	p, err := change.Decode(ev)
	require.NoError(t, err)
	require.Equal(t, change.RoomOpened{RoomID: "room-7"}, p)
}

func TestRoomOpenRetryAfterCrashIngestsNothingAndOpensNothing(t *testing.T) {
	k := newKit(t, triggeredEvent(t), roomOpenedEvent(t))

	require.NoError(t, k.room.Execute(context.Background(), roomEffect(t, "person:ana")))
	require.Zero(t, k.tickets.calls, "the title is not read when the room is already in the log")
	require.Empty(t, k.chat.requests, "no second room is requested")
	require.Zero(t, k.ingest.calls)
}

func TestRoomOpenReturnsTicketReaderErrorSoTheOutboxRetries(t *testing.T) {
	k := newKit(t, triggeredEvent(t))
	k.tickets.err = errTracker

	err := k.room.Execute(context.Background(), roomEffect(t, "person:ana"))
	require.ErrorIs(t, err, errTracker)
	require.Empty(t, k.chat.requests)
	require.Zero(t, k.ingest.calls)
}

func TestRoomOpenReturnsChatErrorAndIngestsNothing(t *testing.T) {
	k := newKit(t, triggeredEvent(t))
	sentinel := errors.New("switch refused the room")
	k.chat.err = sentinel

	err := k.room.Execute(context.Background(), roomEffect(t, "person:ana"))
	require.ErrorIs(t, err, sentinel)
	require.Zero(t, k.ingest.calls)
}

func TestRoomOpenReturnsIngestAndLoadErrors(t *testing.T) {
	k := newKit(t, triggeredEvent(t))
	k.ingest.err = errors.New("engine is down")
	require.ErrorIs(t, k.room.Execute(context.Background(), roomEffect(t, "person:ana")), k.ingest.err)

	k2 := newKit(t, triggeredEvent(t))
	k2.store.err = errors.New("database is down")
	require.ErrorIs(t, k2.room.Execute(context.Background(), roomEffect(t, "person:ana")), k2.store.err)
	require.Empty(t, k2.chat.requests)
}

func TestRoomOpenRefusesAPayloadThatDoesNotValidate(t *testing.T) {
	k := newKit(t, triggeredEvent(t))
	missing := change.Effect{
		Key: roomEffect(t).Key, Kind: change.EffectRoomOpen, Target: string(ticket),
		Payload: json.RawMessage(`{"invite":["person:ana"]}`),
	}
	require.Error(t, k.room.Execute(context.Background(), missing))

	wrongTarget := roomEffect(t, "person:ana")
	wrongTarget.Target = "KAN-8"
	require.Error(t, k.room.Execute(context.Background(), wrongTarget))

	wrongKind := roomEffect(t, "person:ana")
	wrongKind.Kind = change.EffectGitHubBranch
	require.Error(t, k.room.Execute(context.Background(), wrongKind))

	require.Empty(t, k.chat.requests)
	require.Zero(t, k.tickets.calls)
}

func TestRoomOpenRefusesChangeWithoutTrigger(t *testing.T) {
	k := newKit(t)
	require.Error(t, k.room.Execute(context.Background(), roomEffect(t, "person:ana")))
	require.Empty(t, k.chat.requests)
}

func TestRoomOpenTruncatesTitleToTheRoomLimit(t *testing.T) {
	k := newKit(t, triggeredEvent(t))
	k.tickets.ticket.Title = strings.Repeat("é", 200) // 400 bytes, all of them valid UTF-8

	require.NoError(t, k.room.Execute(context.Background(), roomEffect(t, "person:ana")))
	require.Len(t, k.chat.requests, 1)
	title := k.chat.requests[0].Title
	require.LessOrEqual(t, len(title), 256)
	require.NoError(t, k.chat.requests[0].Validate())
}

func TestRoomOpenErrorsCarryNoSecret(t *testing.T) {
	k := newKit(t, triggeredEvent(t))
	k.chat.err = fmt.Errorf("POST https://switch.example token=%s", canary)

	err := k.room.Execute(context.Background(), roomEffect(t, "person:ana"))
	require.Error(t, err)
	require.NotContains(t, err.Error(), canary)
	require.Contains(t, err.Error(), "KAN-7", "the error names the change")

	k2 := newKit(t, triggeredEvent(t))
	k2.tickets.err = fmt.Errorf("auth failed with key %s", canary)
	err = k2.room.Execute(context.Background(), roomEffect(t, "person:ana"))
	require.Error(t, err)
	require.NotContains(t, err.Error(), canary)

	k3 := newKit(t, triggeredEvent(t))
	k3.ingest.err = fmt.Errorf("engine said %s", canary)
	err = k3.room.Execute(context.Background(), roomEffect(t, "person:ana"))
	require.Error(t, err)
	require.NotContains(t, err.Error(), canary)
}

func TestNewRoomOpenRefusesMissingDependencies(t *testing.T) {
	_, err := effects.NewRoomOpen(effects.RoomOpenDeps{})
	require.Error(t, err)
}

func TestBranchWritesNameAtBaseAndIngestsBranchEnsured(t *testing.T) {
	k := newKit(t, triggeredEvent(t), roomOpenedEvent(t))
	room := roomOpenedEvent(t)

	fx := branchEffect(t, change.DefaultBranchTemplate)
	require.NoError(t, k.branch.Execute(context.Background(), fx))
	require.Equal(t, 1, k.branches.calls)
	require.Equal(t, repo, k.branches.repo)
	require.Equal(t, base, k.branches.from)
	require.Equal(t, "KAN-7-fix-refunds-on-partial-capture", k.branches.name)
	require.Equal(t, string(fx.Key), k.branches.key, "the effect key travels to the branch write (A10 item 3)")

	require.Equal(t, 1, k.ingest.calls)
	require.Len(t, k.ingest.written, 1)
	ev := k.ingest.written[0]
	require.Equal(t, change.EventBranchEnsured, ev.Type)
	require.Equal(t, change.ActorGitHub, ev.Actor.Kind)
	require.Equal(t, change.ActorID("github"), ev.Actor.ID)
	require.NotNil(t, ev.CausationID)
	require.Equal(t, room.ID, *ev.CausationID, "the branch is caused by room.opened")

	p, err := change.Decode(ev)
	require.NoError(t, err)
	require.Equal(t, change.BranchEnsured{
		Repo: repo, Branch: "KAN-7-fix-refunds-on-partial-capture", BaseSHA: base,
	}, p)
}

func TestBranchUsesTheTemplateFromThePayload(t *testing.T) {
	k := newKit(t, triggeredEvent(t), roomOpenedEvent(t))
	require.NoError(t, k.branch.Execute(context.Background(), branchEffect(t, "{ticket}-wip-{slug}")))
	require.Equal(t, "KAN-7-wip-fix-refunds-on-partial-capture", k.branches.name)
}

func TestBranchRetryAfterCrashWritesNothing(t *testing.T) {
	k := newKit(t, triggeredEvent(t), roomOpenedEvent(t), branchEnsuredEvent(t))
	require.NoError(t, k.branch.Execute(context.Background(), branchEffect(t, change.DefaultBranchTemplate)))
	require.Zero(t, k.branches.calls, "the branch is not written again")
	require.Zero(t, k.base.calls)
	require.Zero(t, k.ingest.calls)
}

func TestBranchNeedsRoomOpenedInTheLog(t *testing.T) {
	k := newKit(t, triggeredEvent(t))
	require.Error(t, k.branch.Execute(context.Background(), branchEffect(t, change.DefaultBranchTemplate)))
	require.Zero(t, k.branches.calls)
}

func TestBranchRefusesTitleWithoutLettersAndDigits(t *testing.T) {
	k := newKit(t, triggeredEvent(t), roomOpenedEvent(t))
	k.tickets.ticket.Title = "!!! ??? ..."
	require.Error(t, k.branch.Execute(context.Background(), branchEffect(t, change.DefaultBranchTemplate)))
	require.Zero(t, k.branches.calls, "no branch is written for a name it cannot make")
}

func TestBranchReturnsErrorsFromTheTrackerBaseAndWriter(t *testing.T) {
	k := newKit(t, triggeredEvent(t), roomOpenedEvent(t))
	k.tickets.err = errTracker
	require.ErrorIs(t, k.branch.Execute(context.Background(), branchEffect(t, change.DefaultBranchTemplate)), errTracker)
	require.Zero(t, k.branches.calls)

	k2 := newKit(t, triggeredEvent(t), roomOpenedEvent(t))
	k2.base.err = errors.New("github is slow")
	require.Error(t, k2.branch.Execute(context.Background(), branchEffect(t, change.DefaultBranchTemplate)))
	require.Zero(t, k2.branches.calls)

	k3 := newKit(t, triggeredEvent(t), roomOpenedEvent(t))
	writerErr := errors.New("writer refused")
	k3.branches.err = writerErr
	require.ErrorIs(t, k3.branch.Execute(context.Background(), branchEffect(t, change.DefaultBranchTemplate)), writerErr)
	require.Zero(t, k3.ingest.calls)
}

func TestBranchErrorsCarryNoSecret(t *testing.T) {
	k := newKit(t, triggeredEvent(t), roomOpenedEvent(t))
	k.branches.err = fmt.Errorf("PUT /repos/x/git/refs token=%s", canary)
	err := k.branch.Execute(context.Background(), branchEffect(t, change.DefaultBranchTemplate))
	require.Error(t, err)
	require.NotContains(t, err.Error(), canary)
	require.Contains(t, err.Error(), "KAN-7")
}

func TestBranchRefusesPayloadAndTargetThatDoNotValidate(t *testing.T) {
	k := newKit(t, triggeredEvent(t), roomOpenedEvent(t))
	bad := branchEffect(t, change.DefaultBranchTemplate)
	bad.Payload = json.RawMessage(`{"repo":"aarush323/heimdall-fixture-pay","ticket":"KAN-7"}`)
	require.Error(t, k.branch.Execute(context.Background(), bad))

	wrong := branchEffect(t, change.DefaultBranchTemplate)
	wrong.Target = "KAN-9"
	require.Error(t, k.branch.Execute(context.Background(), wrong))

	kind := branchEffect(t, change.DefaultBranchTemplate)
	kind.Kind = change.EffectRoomOpen
	require.Error(t, k.branch.Execute(context.Background(), kind))
	require.Zero(t, k.branches.calls)
}

func TestNewGitHubBranchRefusesMissingDependencies(t *testing.T) {
	_, err := effects.NewGitHubBranch(effects.GitHubBranchDeps{})
	require.Error(t, err)
}

func TestSlugFollowsTheBranchRule(t *testing.T) {
	cases := map[string]string{
		"Refund: fix ÉCLAIR & 2nd try!": "refund-fix-clair-2nd-try",
		"  Leading and trailing  ":      "leading-and-trailing",
		"UPPER lower 123":               "upper-lower-123",
		"a__b..c//d":                    "a-b-c-d",
	}
	for title, want := range cases {
		got, err := effects.Slug(title)
		require.NoError(t, err, title)
		require.Equal(t, want, got, title)
	}
}

func TestSlugIsAtMostFortyCharactersAndNeverEndsInHyphen(t *testing.T) {
	long := "Add refunds for partial captures across every merchant account in the EU region"
	got, err := effects.Slug(long)
	require.NoError(t, err)
	require.LessOrEqual(t, len(got), 40)
	require.NotEmpty(t, got)
	require.False(t, strings.HasSuffix(got, "-"), got)
	require.False(t, strings.HasPrefix(got, "-"), got)
}

func TestSlugRefusesTitleWithNothingLeft(t *testing.T) {
	for _, title := range []string{"", "!!!", "éé", "   "} {
		_, err := effects.Slug(title)
		require.Error(t, err, title)
	}
}
