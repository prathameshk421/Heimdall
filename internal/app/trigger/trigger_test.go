package trigger_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/trigger"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const (
	repoPay     change.Repo    = "acme/pay"
	statusInPrg                = "In Progress"
	statusInRev                = "In Review"
	ana         change.ActorID = "acct-ana"
)

var now = time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)

// fakeTickets is a TicketReader. With reportOnce it returns each change once, as the Jira adapter does after it has
// recorded a change as seen.
type fakeTickets struct {
	mu          sync.Mutex
	changes     []ports.TicketChange
	next        ports.Cursor
	reportOnce  bool
	changesErr  error
	tickets     map[id.ChangeID]ports.Ticket
	ticketErr   map[id.ChangeID]error
	ticketCalls []id.ChangeID
	sinces      []ports.Cursor
}

func newFakeTickets() *fakeTickets {
	return &fakeTickets{tickets: map[id.ChangeID]ports.Ticket{}, ticketErr: map[id.ChangeID]error{}}
}

func (f *fakeTickets) Changes(_ context.Context, since ports.Cursor) ([]ports.TicketChange, ports.Cursor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sinces = append(f.sinces, since)
	if f.changesErr != nil {
		return nil, "", f.changesErr
	}
	out := append([]ports.TicketChange(nil), f.changes...)
	if f.reportOnce {
		f.changes = nil
	}
	return out, f.next, nil
}

func (f *fakeTickets) Ticket(_ context.Context, key id.ChangeID) (ports.Ticket, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ticketCalls = append(f.ticketCalls, key)
	if err := f.ticketErr[key]; err != nil {
		return ports.Ticket{}, err
	}
	tk, ok := f.tickets[key]
	if !ok {
		return ports.Ticket{}, ports.ErrNotFound
	}
	return tk, nil
}

// fakeStarter is a Starter over one stream per change. Start refuses a change that already has a stream with
// ports.ErrVersionConflict, as the engine does when the first append finds events. failNext makes the next Start calls
// return failErr.
type fakeStarter struct {
	mu       sync.Mutex
	streams  map[id.ChangeID][]change.Event
	calls    int
	failNext int
	failErr  error
}

func newFakeStarter() *fakeStarter {
	return &fakeStarter{streams: map[id.ChangeID][]change.Event{}}
}

func (f *fakeStarter) Start(_ context.Context, ev change.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.failNext > 0 {
		f.failNext--
		return f.failErr
	}
	if _, ok := f.streams[ev.ChangeID]; ok {
		return fmt.Errorf("start %s: change exists: %w", ev.ChangeID, ports.ErrVersionConflict)
	}
	f.streams[ev.ChangeID] = []change.Event{ev}
	return nil
}

func (f *fakeStarter) stream(c id.ChangeID) []change.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]change.Event(nil), f.streams[c]...)
}

// startCalls counts the Start calls, refused ones included.
func (f *fakeStarter) startCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// fakeCursors is a CursorStore. It records every saved cursor.
type fakeCursors struct {
	mu      sync.Mutex
	value   ports.Cursor
	saved   []ports.Cursor
	loadErr error
	saveErr error
}

func (f *fakeCursors) Load(context.Context) (ports.Cursor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.loadErr != nil {
		return "", f.loadErr
	}
	return f.value, nil
}

func (f *fakeCursors) Save(_ context.Context, c ports.Cursor) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = append(f.saved, c)
	f.value = c
	return nil
}

func (f *fakeCursors) savedCursors() []ports.Cursor {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ports.Cursor(nil), f.saved...)
}

func (f *fakeCursors) current() ports.Cursor {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.value
}

// fakeProjects maps the KAN project to acme/pay. Any other project is unmapped.
type fakeProjects map[string]change.Repo

func (p fakeProjects) RepoFor(project string) (change.Repo, bool) {
	r, ok := p[project]
	return r, ok
}

type fakeClock struct{ at time.Time }

func (c fakeClock) Now() time.Time { return c.at }

// seqIDs makes sequential event ids. The other id kinds are not used by the trigger.
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

// rig is one set of fakes and a Poller built on them.
type rig struct {
	tickets *fakeTickets
	starter *fakeStarter
	cursors *fakeCursors
	ids     *seqIDs
	poller  *trigger.Poller
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{tickets: newFakeTickets(), starter: newFakeStarter(), cursors: &fakeCursors{}, ids: &seqIDs{}}
	r.poller = r.build(t, r.cursors)
	return r
}

// build makes a Poller on the rig's fakes with the given cursor store, so a test can model a restart.
func (r *rig) build(t *testing.T, cursors trigger.CursorStore) *trigger.Poller {
	t.Helper()
	p, err := trigger.New(trigger.Config{
		Tickets:  r.tickets,
		Starter:  r.starter,
		Cursors:  cursors,
		Projects: fakeProjects{"KAN": repoPay},
		Clock:    fakeClock{at: now},
		IDs:      r.ids,
	})
	require.NoError(t, err)
	return p
}

func (r *rig) poll(t *testing.T) trigger.PollResult {
	t.Helper()
	res, err := r.poller.PollOnce(context.Background())
	require.NoError(t, err)
	return res
}

// statusChange is a changelog entry that touched the status field.
func statusChange(key id.ChangeID, changelog string) ports.TicketChange {
	return ports.TicketChange{Key: key, ChangelogID: changelog, Fields: []string{"status"}, At: now}
}

func ticket(key id.ChangeID, status string, assignee change.ActorID) ports.Ticket {
	return ports.Ticket{Key: key, Title: "Refund rule", Assignee: assignee, Status: status}
}

// assertIgnored checks that a clean poll ignored one change for reason, appended nothing and saved its cursor.
func assertIgnored(t *testing.T, r *rig, res trigger.PollResult, reason trigger.Reason, key id.ChangeID) {
	t.Helper()
	require.Equal(t, 0, res.Triggered)
	require.Equal(t, 0, res.AlreadyTriggered)
	require.Equal(t, map[trigger.Reason]int{reason: 1}, res.Ignored)
	require.Empty(t, r.starter.stream(key))
	require.Equal(t, []ports.Cursor{"c1"}, r.cursors.savedCursors())
}

func TestTriggersOnceForAValidChange(t *testing.T) {
	r := newRig(t)
	r.tickets.changes = []ports.TicketChange{statusChange("KAN-1", "10001")}
	r.tickets.next = "c1"
	r.tickets.tickets["KAN-1"] = ticket("KAN-1", statusInPrg, ana)

	res := r.poll(t)

	require.Equal(t, 1, res.Changes)
	require.Equal(t, 1, res.Triggered)
	require.Equal(t, 0, res.AlreadyTriggered)
	require.Empty(t, res.Ignored)
	evs := r.starter.stream("KAN-1")
	require.Len(t, evs, 1)
	ev := evs[0]
	require.Equal(t, change.EventChangeTriggered, ev.Type)
	require.Equal(t, int64(1), ev.Version)
	require.Equal(t, change.Actor{Kind: change.ActorJira, ID: "jira"}, ev.Actor)
	require.True(t, ev.OccurredAt.Equal(now), "occurred at %s", ev.OccurredAt)
	require.Equal(t, 1, r.starter.startCalls())
	p, err := change.Decode(ev)
	require.NoError(t, err)
	require.Equal(t, change.Triggered{Ticket: "KAN-1", Repo: repoPay, Assignee: ana, Source: change.TriggerJira}, p)
	require.Equal(t, []ports.Cursor{"c1"}, r.cursors.savedCursors())
}

func TestIgnoresAChangeThatIsNotInProgress(t *testing.T) {
	r := newRig(t)
	r.tickets.changes = []ports.TicketChange{statusChange("KAN-1", "10001")}
	r.tickets.next = "c1"
	r.tickets.tickets["KAN-1"] = ticket("KAN-1", statusInRev, ana)

	assertIgnored(t, r, r.poll(t), trigger.ReasonNotInProgress, "KAN-1")
}

func TestIgnoresAnUnassignedTicket(t *testing.T) {
	r := newRig(t)
	r.tickets.changes = []ports.TicketChange{statusChange("KAN-1", "10001")}
	r.tickets.next = "c1"
	r.tickets.tickets["KAN-1"] = ticket("KAN-1", statusInPrg, "")

	assertIgnored(t, r, r.poll(t), trigger.ReasonUnassigned, "KAN-1")
}

func TestIgnoresAnUnmappedProject(t *testing.T) {
	r := newRig(t)
	r.tickets.changes = []ports.TicketChange{statusChange("OPS-9", "10001")}
	r.tickets.next = "c1"
	r.tickets.tickets["OPS-9"] = ticket("OPS-9", statusInPrg, ana)

	res := r.poll(t)

	assertIgnored(t, r, res, trigger.ReasonUnmappedProject, "OPS-9")
	require.Empty(t, r.tickets.ticketCalls, "an unmapped project must not read the ticket")
}

func TestIgnoresAChangeThatDidNotTouchStatus(t *testing.T) {
	r := newRig(t)
	summary := ports.TicketChange{Key: "KAN-1", ChangelogID: "10001", Fields: []string{"summary"}, At: now}
	r.tickets.changes = []ports.TicketChange{summary}
	r.tickets.next = "c1"
	r.tickets.tickets["KAN-1"] = ticket("KAN-1", statusInPrg, ana)

	res := r.poll(t)

	assertIgnored(t, r, res, trigger.ReasonNoStatusChange, "KAN-1")
	require.Empty(t, r.tickets.ticketCalls, "a change without a status move must not read the ticket")
}

func TestIgnoresATicketThatIsGone(t *testing.T) {
	r := newRig(t)
	r.tickets.changes = []ports.TicketChange{statusChange("KAN-1", "10001")}
	r.tickets.next = "c1"

	assertIgnored(t, r, r.poll(t), trigger.ReasonTicketNotFound, "KAN-1")
}

func TestARepeatInTheNextPollAppendsNothing(t *testing.T) {
	r := newRig(t)
	r.tickets.changes = []ports.TicketChange{statusChange("KAN-1", "10001")}
	r.tickets.next = "c1"
	r.tickets.tickets["KAN-1"] = ticket("KAN-1", statusInPrg, ana)
	require.Equal(t, 1, r.poll(t).Triggered)

	res := r.poll(t)

	require.Equal(t, 0, res.Triggered)
	require.Equal(t, 1, res.AlreadyTriggered)
	require.Len(t, r.starter.stream("KAN-1"), 1)
	require.Equal(t, 2, r.starter.startCalls(), "the repeat is started and refused, not retried")
}

func TestAChangeMovingToInProgressAgainAppendsNothing(t *testing.T) {
	r := newRig(t)
	r.tickets.changes = []ports.TicketChange{statusChange("KAN-1", "10001")}
	r.tickets.next = "c1"
	r.tickets.tickets["KAN-1"] = ticket("KAN-1", statusInPrg, ana)
	require.Equal(t, 1, r.poll(t).Triggered)

	r.tickets.changes = []ports.TicketChange{statusChange("KAN-1", "10002")}
	res := r.poll(t)

	require.Equal(t, 0, res.Triggered)
	require.Equal(t, 1, res.AlreadyTriggered)
	require.Len(t, r.starter.stream("KAN-1"), 1)
}

func TestARestartAppendsNothing(t *testing.T) {
	r := newRig(t)
	r.tickets.changes = []ports.TicketChange{statusChange("KAN-1", "10001")}
	r.tickets.next = "c1"
	r.tickets.tickets["KAN-1"] = ticket("KAN-1", statusInPrg, ana)
	require.Equal(t, 1, r.poll(t).Triggered)

	// A new poller with an empty cursor store reads the same change again, as after a lost cursor.
	restarted := r.build(t, &fakeCursors{})
	res, err := restarted.PollOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 0, res.Triggered)
	require.Equal(t, 1, res.AlreadyTriggered)
	require.Len(t, r.starter.stream("KAN-1"), 1)
}

func TestTwoPollersRacingEndWithOneEvent(t *testing.T) {
	r := newRig(t)
	r.tickets.changes = []ports.TicketChange{statusChange("KAN-1", "10001")}
	r.tickets.next = "c1"
	r.tickets.tickets["KAN-1"] = ticket("KAN-1", statusInPrg, ana)
	pollers := []*trigger.Poller{r.poller, r.build(t, &fakeCursors{})}

	results := make([]trigger.PollResult, len(pollers))
	errs := make([]error, len(pollers))
	var wg sync.WaitGroup
	for i, p := range pollers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = p.PollOnce(context.Background())
		}()
	}
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, 1, results[0].Triggered+results[1].Triggered)
	require.Equal(t, 1, results[0].AlreadyTriggered+results[1].AlreadyTriggered)
	require.Len(t, r.starter.stream("KAN-1"), 1)
}

func TestCursorIsNotSavedWhenTheReaderIsUnavailable(t *testing.T) {
	r := newRig(t)
	r.cursors.value = "c0"
	r.tickets.changesErr = fmt.Errorf("jira changes: status 503: %w", ports.ErrUnavailable)

	_, err := r.poller.PollOnce(context.Background())

	require.ErrorIs(t, err, ports.ErrUnavailable)
	require.Empty(t, r.cursors.savedCursors())
	require.Equal(t, ports.Cursor("c0"), r.cursors.current())
}

func TestCursorIsNotSavedWhenAnAppendIsUnavailable(t *testing.T) {
	r := newRig(t)
	r.cursors.value = "c0"
	r.tickets.changes = []ports.TicketChange{statusChange("KAN-1", "10001")}
	r.tickets.next = "c1"
	r.tickets.tickets["KAN-1"] = ticket("KAN-1", statusInPrg, ana)
	r.starter.failNext = 1
	r.starter.failErr = fmt.Errorf("append: %w", ports.ErrUnavailable)

	_, err := r.poller.PollOnce(context.Background())

	require.ErrorIs(t, err, ports.ErrUnavailable)
	require.Empty(t, r.cursors.savedCursors())
	require.Equal(t, ports.Cursor("c0"), r.cursors.current())
}

func TestAChangeThatFailedOnAnOutageIsTriggeredOnTheNextPoll(t *testing.T) {
	r := newRig(t)
	r.tickets.reportOnce = true
	r.tickets.changes = []ports.TicketChange{statusChange("KAN-1", "10001")}
	r.tickets.next = "c1"
	r.tickets.tickets["KAN-1"] = ticket("KAN-1", statusInPrg, ana)
	r.starter.failNext = 1
	r.starter.failErr = fmt.Errorf("append: %w", ports.ErrUnavailable)

	_, err := r.poller.PollOnce(context.Background())
	require.ErrorIs(t, err, ports.ErrUnavailable)

	// The reader has already reported the change, so the next poll returns nothing new.
	res := r.poll(t)

	require.Equal(t, 1, res.Triggered)
	require.Len(t, r.starter.stream("KAN-1"), 1)
	require.Equal(t, []ports.Cursor{"c1"}, r.cursors.savedCursors())
}

func TestCursorIsNotSavedAfterAPermanentError(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*rig)
	}{
		{"ticket read refused", func(r *rig) {
			r.tickets.ticketErr["KAN-1"] = fmt.Errorf("status 401: %w", ports.ErrUnauthorized)
		}},
		{"malformed change", func(r *rig) {
			r.tickets.changes[0].ChangelogID = ""
		}},
		{"event store refuses", func(r *rig) {
			r.starter.failNext = 1
			r.starter.failErr = errors.New("payload rejected")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			r.cursors.value = "c0"
			r.tickets.changes = []ports.TicketChange{statusChange("KAN-1", "10001")}
			r.tickets.next = "c1"
			r.tickets.tickets["KAN-1"] = ticket("KAN-1", statusInPrg, ana)
			tc.setup(r)

			_, err := r.poller.PollOnce(context.Background())

			require.Error(t, err)
			require.ErrorContains(t, err, "KAN-1")
			require.Empty(t, r.cursors.savedCursors())
			require.Equal(t, ports.Cursor("c0"), r.cursors.current())
		})
	}
}

func TestCursorIsSavedAfterACleanPoll(t *testing.T) {
	r := newRig(t)
	r.cursors.value = "c0"
	r.tickets.next = "c5"

	res := r.poll(t)

	require.Equal(t, 0, res.Changes)
	require.Equal(t, []ports.Cursor{"c5"}, r.cursors.savedCursors())
	require.Equal(t, []ports.Cursor{"c0"}, r.tickets.sinces)
}

func TestReasonsAreCountedInThePollResult(t *testing.T) {
	r := newRig(t)
	r.tickets.changes = []ports.TicketChange{
		statusChange("KAN-1", "1"),
		statusChange("KAN-2", "2"),
		statusChange("KAN-3", "3"),
		statusChange("OPS-1", "4"),
		{Key: "KAN-5", ChangelogID: "5", Fields: []string{"summary"}, At: now},
		statusChange("KAN-6", "6"),
		statusChange("KAN-1", "7"),
	}
	r.tickets.next = "c1"
	r.tickets.tickets["KAN-1"] = ticket("KAN-1", statusInPrg, ana)
	r.tickets.tickets["KAN-2"] = ticket("KAN-2", statusInRev, ana)
	r.tickets.tickets["KAN-3"] = ticket("KAN-3", statusInPrg, "")
	r.tickets.tickets["OPS-1"] = ticket("OPS-1", statusInPrg, ana)
	r.tickets.tickets["KAN-5"] = ticket("KAN-5", statusInPrg, ana)

	res := r.poll(t)

	require.Equal(t, 7, res.Changes)
	require.Equal(t, 1, res.Triggered)
	require.Equal(t, 1, res.AlreadyTriggered)
	require.Equal(t, map[trigger.Reason]int{
		trigger.ReasonNotInProgress:   1,
		trigger.ReasonUnassigned:      1,
		trigger.ReasonUnmappedProject: 1,
		trigger.ReasonNoStatusChange:  1,
		trigger.ReasonTicketNotFound:  1,
	}, res.Ignored)
}

func TestNewRefusesANilPort(t *testing.T) {
	full := func(r *rig) trigger.Config {
		return trigger.Config{
			Tickets:  r.tickets,
			Starter:  r.starter,
			Cursors:  r.cursors,
			Projects: fakeProjects{"KAN": repoPay},
			Clock:    fakeClock{at: now},
			IDs:      r.ids,
		}
	}
	cases := []struct {
		name string
		drop func(*trigger.Config)
	}{
		{"Tickets", func(c *trigger.Config) { c.Tickets = nil }},
		{"Starter", func(c *trigger.Config) { c.Starter = nil }},
		{"Cursors", func(c *trigger.Config) { c.Cursors = nil }},
		{"Projects", func(c *trigger.Config) { c.Projects = nil }},
		{"Clock", func(c *trigger.Config) { c.Clock = nil }},
		{"IDs", func(c *trigger.Config) { c.IDs = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := full(newRig(t))
			tc.drop(&cfg)
			_, err := trigger.New(cfg)
			require.ErrorContains(t, err, tc.name)
		})
	}
}
