package collaboration

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/commands"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// proposeIdentity maps chat senders to people; agents are the senders in agents.
type proposeIdentity struct {
	people mappedPeople
	agents map[string]bool
}

func (f proposeIdentity) People(context.Context, Target) (commands.People, error) {
	return f.people, nil
}
func (f proposeIdentity) IsAgent(_ context.Context, _ Target, _ commands.Source, sender string) (bool, error) {
	return f.agents[sender], nil
}
func (f proposeIdentity) Authorize(_ context.Context, _ Target, src commands.Source, sender string, _ commands.Command) (string, error) {
	name, _ := f.people.Lookup(src, sender)
	return name, nil
}

// proposalStore keeps the held-command facts in memory, as the event log would.
type proposalStore struct {
	events []change.Event
	head   id.SHA
	at     time.Time
}

func (s *proposalStore) Events(context.Context, Target) ([]change.Event, error) { return s.events, nil }
func (s *proposalStore) Head(context.Context, Target) (id.SHA, error)           { return s.head, nil }
func (s *proposalStore) Hold(_ context.Context, _ Target, p change.CommandProposed) error {
	return s.add(change.EventCommandProposed, p)
}
func (s *proposalStore) Confirm(_ context.Context, _ Target, c change.CommandConfirmed) error {
	return s.add(change.EventCommandConfirmed, c)
}

func (s *proposalStore) add(t change.EventType, p any) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	s.events = append(s.events, change.Event{Type: t, OccurredAt: s.at, Payload: raw})
	return nil
}

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

const (
	room       = change.RoomID("room-kan-22")
	person     = "sw-88"   // prathamesh, a mapped person
	stranger   = "sw-99"   // not in people
	lead       = "sw-lead" // the lead agent's own sender id
	headBefore = id.SHA("59effae547d768799972d6e54eff32e82dbb7912")
)

type proposeRig struct {
	proc     *Processor
	executor *executorFake
	replies  *replyFake
	store    *proposalStore
	target   Target
	now      time.Time
}

func newProposeRig(t *testing.T) *proposeRig {
	t.Helper()
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	r := &proposeRig{executor: &executorFake{}, replies: &replyFake{}, store: &proposalStore{head: headBefore, at: now}, now: now,
		target: Target{Room: ports.RoomBinding{RoomID: room, ChangeID: "KAN-22"}, ChangeID: "KAN-22"}}
	identity := proposeIdentity{people: mappedPeople{"chat:" + person: "prathamesh"}, agents: map[string]bool{lead: true}}
	proc, err := New(Config{Identity: identity, Executor: r.executor, Replies: r.replies, Proposals: r.store, Clock: fixedClock{now}})
	require.NoError(t, err)
	r.proc = proc
	return r
}

func (r *proposeRig) trigger() ports.Message {
	return ports.Message{ID: "msg_0050", Room: room, Sender: person, Text: "dismiss F2, it's the same as F1", At: r.now.Add(-time.Minute)}
}

func (r *proposeRig) yes(t *testing.T, sender, msgID string) {
	t.Helper()
	require.NoError(t, r.proc.Handle(context.Background(), Input{Target: r.target, SourceRef: msgID,
		Command: commands.Input{Text: "yes", Source: commands.SourceChat, Sender: sender}}))
}

const dismiss = "dismiss F2 duplicate of F1, same lines and cause"

// BUILD_PLAN M5 done-when: "dismiss F2" proposed for a mapped sender is held until that sender's yes, and runs as
// that person.
func TestADecidingProposalIsHeldUntilTheSendersOwnYes(t *testing.T) {
	r := newProposeRig(t)
	out, err := r.proc.Propose(context.Background(), r.target, "lead", dismiss, r.trigger(), r.now)
	require.NoError(t, err)
	require.Contains(t, out, "held")
	require.Zero(t, r.executor.calls, "nothing runs before the yes")
	require.Contains(t, r.replies.body, "I'll run: `heimdall "+dismiss+"`")

	r.yes(t, stranger, "msg_0051")
	require.Zero(t, r.executor.calls, "another person's yes confirms nothing")

	r.yes(t, person, "msg_0052")
	require.Equal(t, 1, r.executor.calls)
	require.Equal(t, "prathamesh", r.executor.person, "the person, never the agent, is the actor")
	require.Equal(t, "dismiss", r.executor.command.Verb())

	r.yes(t, person, "msg_0053")
	require.Equal(t, 1, r.executor.calls, "a confirmed proposal is not open any more")
}

func TestAPushBeforeTheYesMakesTheProposalStale(t *testing.T) {
	r := newProposeRig(t)
	_, err := r.proc.Propose(context.Background(), r.target, "lead", dismiss, r.trigger(), r.now)
	require.NoError(t, err)
	r.store.head = "1111111111111111111111111111111111111111"
	r.yes(t, person, "msg_0052")
	require.Zero(t, r.executor.calls)
}

func TestALightProposalRunsAtOnceAsTheSender(t *testing.T) {
	r := newProposeRig(t)
	out, err := r.proc.Propose(context.Background(), r.target, "lead", "remember refunds use the ledger clock", r.trigger(), r.now)
	require.NoError(t, err)
	require.Contains(t, out, "Ran for prathamesh")
	require.Equal(t, 1, r.executor.calls)
	require.Equal(t, "prathamesh", r.executor.person)
	require.Empty(t, r.store.events, "a light verb is not held")
}

// The injection cases of section 10: text with no trigger from a mapped person never becomes a command.
func TestAProposalWithoutAMappedPersonsTriggerChangesNothing(t *testing.T) {
	for name, mutate := range map[string]func(*ports.Message){
		"the trigger was written by an agent":           func(m *ports.Message) { m.Sender = lead },
		"the trigger was written by an unmapped sender": func(m *ports.Message) { m.Sender = stranger },
		"the trigger is in another room":                func(m *ports.Message) { m.Room = "room-elsewhere" },
		"the trigger is too old":                        func(m *ports.Message) { m.At = m.At.Add(-commands.ProposalTTL) },
	} {
		t.Run(name, func(t *testing.T) {
			r := newProposeRig(t)
			trigger := r.trigger()
			mutate(&trigger)
			_, err := r.proc.Propose(context.Background(), r.target, "lead", dismiss, trigger, r.now)
			require.ErrorIs(t, err, ErrRefused)
			require.Zero(t, r.executor.calls)
			require.Empty(t, r.store.events)
		})
	}
}
