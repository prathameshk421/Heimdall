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
	_ ports.DefaultHeads = (*fakeBase)(nil)
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
	ingest   *fakeIngest
	base     *fakeBase
	branches *fakeBranches
	room     effects.RoomOpen
	branch   effects.GitHubBranch
}

func newKit(t *testing.T) *kit {
	t.Helper()
	k := &kit{
		tickets:  &fakeTickets{ticket: ports.Ticket{Key: ticket, Title: "Fix refunds on partial capture", Status: "In Progress"}},
		chat:     &fakeChat{room: "room-7"},
		ingest:   &fakeIngest{},
		base:     &fakeBase{sha: base},
		branches: &fakeBranches{},
	}
	room, err := effects.NewRoomOpen(effects.RoomOpenDeps{
		Tickets: k.tickets, Chat: k.chat, Ingest: k.ingest, IDs: &fakeIDs{}, Clock: fixedClock{},
		Agents: []string{"test-author", "reviewer-1", "resolver"},
	})
	require.NoError(t, err)
	k.room = room
	branch, err := effects.NewGitHubBranch(effects.GitHubBranchDeps{
		Tickets: k.tickets, Base: k.base, Branches: k.branches, Ingest: k.ingest,
		IDs: &fakeIDs{}, Clock: fixedClock{},
	})
	require.NoError(t, err)
	k.branch = branch
	return k
}

func TestRoomOpenRequestsTheRoomAndRecordsRoomOpened(t *testing.T) {
	t.Parallel()
	k := newKit(t)
	fx := roomEffect(t, "role:test-author", "person:ana")

	require.NoError(t, k.room.Execute(context.Background(), fx))
	require.Len(t, k.chat.requests, 1)
	req := k.chat.requests[0]
	require.Equal(t, fx.Key, req.Key, "the effect key is the idempotency key of the room request")
	require.Equal(t, ticket, req.ChangeID)
	require.Equal(t, "KAN-7 Fix refunds on partial capture", req.Title, "the title leads with the ticket key")
	require.Equal(t, []string{"test-author", "reviewer-1", "resolver"}, req.Agents,
		"an agent that is not in the room never receives its @mention")

	require.Equal(t, 1, k.ingest.calls)
	require.Equal(t, ticket, k.ingest.change)
	require.Len(t, k.ingest.written, 1)
	ev := k.ingest.written[0]
	require.Equal(t, change.EventRoomOpened, ev.Type)
	require.Equal(t, change.ActorSwitch, ev.Actor.Kind, "the room comes from the chat adapter, which is the switch actor")
	require.Equal(t, change.ActorID("switch"), ev.Actor.ID)
	require.True(t, ev.OccurredAt.Equal(now))
	require.Zero(t, ev.Version, "the engine numbers the event when it stores it")
	p, err := change.Decode(ev)
	require.NoError(t, err)
	require.Equal(t, change.RoomOpened{RoomID: "room-7"}, p)
}

// Found live: KAN-9 and KAN-11 had the same summary, and OpenRoom adopts an open room by title, so both changes shared
// one room and one Discord channel. The ticket key in the title keeps each change in its own room.
func TestTwoTicketsWithTheSameSummaryGetDifferentRoomTitles(t *testing.T) {
	t.Parallel()
	first := newKit(t)
	require.NoError(t, first.room.Execute(context.Background(), roomEffect(t, "person:ana")))
	second := newKit(t)
	second.tickets.ticket.Key = "KAN-8"
	require.NoError(t, second.room.Execute(context.Background(), roomEffect(t, "person:ana")))
	require.Equal(t, first.tickets.ticket.Title, second.tickets.ticket.Title, "same summary")
	require.NotEqual(t, first.chat.requests[0].Title, second.chat.requests[0].Title)
}

func TestRoomOpenPropagatesDependencyErrorsWithoutIngesting(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("dependency down")
	cases := map[string]struct {
		setup       func(*kit)
		chatCalls   int
		ingestCalls int
	}{
		"ticket reader": {func(k *kit) { k.tickets.err = sentinel }, 0, 0},
		"chat":          {func(k *kit) { k.chat.err = sentinel }, 1, 0},
		"ingest":        {func(k *kit) { k.ingest.err = sentinel }, 1, 1},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			k := newKit(t)
			c.setup(k)
			require.ErrorIs(t, k.room.Execute(context.Background(), roomEffect(t, "person:ana")), sentinel)
			require.Len(t, k.chat.requests, c.chatCalls)
			require.Equal(t, c.ingestCalls, k.ingest.calls)
		})
	}
}

func TestRoomOpenRefusesAPayloadThatDoesNotValidate(t *testing.T) {
	t.Parallel()
	k := newKit(t)
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

func TestRoomOpenTruncatesTitleToTheRoomLimit(t *testing.T) {
	t.Parallel()
	k := newKit(t)
	k.tickets.ticket.Title = strings.Repeat("é", 200) // 400 bytes, all of them valid UTF-8

	require.NoError(t, k.room.Execute(context.Background(), roomEffect(t, "person:ana")))
	require.Len(t, k.chat.requests, 1)
	title := k.chat.requests[0].Title
	require.LessOrEqual(t, len(title), 256)
	require.NoError(t, k.chat.requests[0].Validate())
}

// A pull request that names no ticket has a room too (HARNESS_DESIGN B1): the room follows the pull request that
// started the change and is named after it, with no tracker read.
func TestRoomOpenForAPullRequestOnlyChange(t *testing.T) {
	t.Parallel()
	const prChange = id.ChangeID("pr-acme-pay-7")
	k := newKit(t)
	raw, err := json.Marshal(change.RoomOpenPayload{Ticket: prChange, Invite: []string{}})
	require.NoError(t, err)
	key, err := change.IdempotencyKey(prChange, 1, change.EffectRoomOpen, "room")
	require.NoError(t, err)

	require.NoError(t, k.room.Execute(context.Background(), change.Effect{Key: key, Kind: change.EffectRoomOpen, Target: string(prChange), Payload: raw}))
	require.Zero(t, k.tickets.calls, "there is no ticket to read")
	require.Equal(t, "PR acme/pay#7", k.chat.requests[0].Title)
}

func TestBranchWritesNameAtBaseAndIngestsBranchEnsured(t *testing.T) {
	t.Parallel()
	k := newKit(t)

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
	p, err := change.Decode(ev)
	require.NoError(t, err)
	require.Equal(t, change.BranchEnsured{Repo: repo, Branch: "KAN-7-fix-refunds-on-partial-capture", BaseSHA: base}, p)

	require.NoError(t, k.branch.Execute(context.Background(), branchEffect(t, "{ticket}-wip-{slug}")))
	require.Equal(t, "KAN-7-wip-fix-refunds-on-partial-capture", k.branches.name, "the template comes from the payload")
}

func TestBranchRefusesTitleWithoutLettersAndDigits(t *testing.T) {
	t.Parallel()
	k := newKit(t)
	k.tickets.ticket.Title = "!!! ??? ..."
	require.Error(t, k.branch.Execute(context.Background(), branchEffect(t, change.DefaultBranchTemplate)))
	require.Zero(t, k.branches.calls, "no branch is written for a name it cannot make")
}

func TestBranchReturnsErrorsFromTheTrackerBaseAndWriter(t *testing.T) {
	t.Parallel()
	k := newKit(t)
	k.tickets.err = errTracker
	require.ErrorIs(t, k.branch.Execute(context.Background(), branchEffect(t, change.DefaultBranchTemplate)), errTracker)
	require.Zero(t, k.branches.calls)

	k2 := newKit(t)
	k2.base.err = errors.New("github is slow")
	require.Error(t, k2.branch.Execute(context.Background(), branchEffect(t, change.DefaultBranchTemplate)))
	require.Zero(t, k2.branches.calls)

	k3 := newKit(t)
	writerErr := errors.New("writer refused")
	k3.branches.err = writerErr
	require.ErrorIs(t, k3.branch.Execute(context.Background(), branchEffect(t, change.DefaultBranchTemplate)), writerErr)
	require.Zero(t, k3.ingest.calls)
}

func TestBranchRefusesPayloadAndTargetThatDoNotValidate(t *testing.T) {
	t.Parallel()
	k := newKit(t)
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

func TestRoomAndBranchErrorsCarryNoSecret(t *testing.T) {
	t.Parallel()
	leak := fmt.Errorf("upstream said token=%s", canary)
	cases := map[string]struct {
		setup    func(*kit)
		run      func(*kit) error
		namesKey bool
	}{
		"room chat": {func(k *kit) { k.chat.err = leak },
			func(k *kit) error { return k.room.Execute(context.Background(), roomEffect(t, "person:ana")) }, true},
		"room tickets": {func(k *kit) { k.tickets.err = leak },
			func(k *kit) error { return k.room.Execute(context.Background(), roomEffect(t, "person:ana")) }, false},
		"room ingest": {func(k *kit) { k.ingest.err = leak },
			func(k *kit) error { return k.room.Execute(context.Background(), roomEffect(t, "person:ana")) }, false},
		"branch writer": {func(k *kit) { k.branches.err = leak },
			func(k *kit) error {
				return k.branch.Execute(context.Background(), branchEffect(t, change.DefaultBranchTemplate))
			}, true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			k := newKit(t)
			c.setup(k)
			err := c.run(k)
			require.Error(t, err)
			require.NotContains(t, err.Error(), canary)
			if c.namesKey {
				require.Contains(t, err.Error(), "KAN-7", "the error names the change")
			}
		})
	}
}

func TestSlug(t *testing.T) {
	t.Parallel()
	for title, want := range map[string]string{
		"Refund: fix ÉCLAIR & 2nd try!": "refund-fix-clair-2nd-try",
		"  Leading and trailing  ":      "leading-and-trailing",
		"UPPER lower 123":               "upper-lower-123",
		"a__b..c//d":                    "a-b-c-d",
	} {
		got, err := effects.Slug(title)
		require.NoError(t, err, title)
		require.Equal(t, want, got, title)
	}

	long := "Add refunds for partial captures across every merchant account in the EU region"
	got, err := effects.Slug(long)
	require.NoError(t, err)
	require.LessOrEqual(t, len(got), 40)
	require.NotEmpty(t, got)
	require.False(t, strings.HasSuffix(got, "-"), got)
	require.False(t, strings.HasPrefix(got, "-"), got)

	for _, title := range []string{"", "!!!", "éé", "   "} {
		_, err := effects.Slug(title)
		require.Error(t, err, title)
	}
}
