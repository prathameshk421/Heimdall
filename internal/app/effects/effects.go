package effects

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Ingest hands the events of an executor to the engine. The engine stores them in one append, numbers them, and runs
// the steps that follow (TECH_SPEC 2.4). The executor leaves Version and the hash fields to the engine.
type Ingest interface {
	Ingest(ctx context.Context, changeID id.ChangeID, evs []change.Event) error
}

// BaseReader reads the head commit of a repository's default branch. No port has this method yet: the branch writer
// needs the base commit and the VCS port has no way to read it.
type BaseReader interface {
	DefaultHead(ctx context.Context, repo change.Repo) (id.SHA, error)
}

// Actor ids of the events that executors write. They name the adapter that reported the result.
const (
	switchActorID = change.ActorID("switch")
	githubActorID = change.ActorID("github")
)

// loadedChange is the stored events of a change, read before an executor acts.
type loadedChange struct {
	events []change.Event
}

// load returns the stored events of the change. Every executor loads first, so it can see whether its result is
// already in the log.
func load(ctx context.Context, store ports.EventStore, cid id.ChangeID) (loadedChange, error) {
	evs, err := store.Load(ctx, cid)
	if err != nil {
		return loadedChange{}, failf(err, "load change %s", cid)
	}
	return loadedChange{events: evs}, nil
}

// has reports whether an event of type t is stored.
func (l loadedChange) has(t change.EventType) bool {
	return l.first(t) != nil
}

// first returns the first stored event of type t, or nil.
func (l loadedChange) first(t change.EventType) *change.Event {
	for i := range l.events {
		if l.events[i].Type == t {
			return &l.events[i]
		}
	}
	return nil
}

// newEvent builds an event caused by cause. The id comes from the IDs port and the time from the clock.
func newEvent(ids ports.IDs, clock ports.Clock, cause change.Event, typ change.EventType, actor change.Actor, p change.Payload) (change.Event, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return change.Event{}, fmt.Errorf("encode %s: %w", typ, err)
	}
	causation := cause.ID
	return change.Event{
		ID:            ids.NewEvent(),
		ChangeID:      cause.ChangeID,
		Type:          typ,
		Actor:         actor,
		OccurredAt:    clock.Now(),
		CausationID:   &causation,
		CorrelationID: cause.CorrelationID,
		Payload:       raw,
	}, nil
}

// failure is the error an executor returns. Its text holds only fixed words and ids, so it is safe to store as the
// reason of a failed outbox row. The cause stays reachable through Unwrap for errors.Is and errors.As.
type failure struct {
	msg   string
	cause error
}

// Error returns the fixed text. It never includes the text of the cause.
func (f failure) Error() string { return f.msg }

// Unwrap returns the cause, which branches on errors.Is see.
func (f failure) Unwrap() error { return f.cause }

// failf builds a failure with a message made from format and args, and the cause kept for Unwrap.
func failf(cause error, format string, args ...any) error {
	return failure{msg: fmt.Sprintf(format, args...), cause: cause}
}

// checkTarget refuses an effect whose target is not the ticket its payload names.
func checkTarget(fx change.Effect, cid id.ChangeID) error {
	if fx.Target != string(cid) {
		return failf(nil, "effect %s: target %q is not the ticket %s", fx.Key, fx.Target, cid)
	}
	return nil
}

// RoomOpenDeps are the dependencies of RoomOpen.
type RoomOpenDeps struct {
	Tickets ports.TicketReader
	Chat    ports.Chat
	Store   ports.EventStore
	Ingest  Ingest
	IDs     ports.IDs
	Clock   ports.Clock
}

// RoomOpen performs room_open effects: it asks Switch for the room of the change and records room.opened.
type RoomOpen struct {
	deps RoomOpenDeps
}

// NewRoomOpen builds the executor, or refuses when a dependency is missing.
func NewRoomOpen(d RoomOpenDeps) (RoomOpen, error) {
	var missing []string
	if d.Tickets == nil {
		missing = append(missing, "tickets")
	}
	if d.Chat == nil {
		missing = append(missing, "chat")
	}
	if d.Store == nil {
		missing = append(missing, "store")
	}
	if d.Ingest == nil {
		missing = append(missing, "ingest")
	}
	if d.IDs == nil {
		missing = append(missing, "ids")
	}
	if d.Clock == nil {
		missing = append(missing, "clock")
	}
	if len(missing) > 0 {
		return RoomOpen{}, fmt.Errorf("room_open executor: missing %v", missing)
	}
	return RoomOpen{deps: d}, nil
}

// Execute opens the room unless room.opened is already stored. The effect key is the idempotency key of the room
// request, so Switch adopts a room that an earlier attempt made.
func (r RoomOpen) Execute(ctx context.Context, fx change.Effect) error {
	p, err := roomPayload(fx)
	if err != nil {
		return err
	}
	l, err := load(ctx, r.deps.Store, p.Ticket)
	if err != nil {
		return err
	}
	if l.has(change.EventRoomOpened) {
		return nil
	}
	cause := l.first(change.EventChangeTriggered)
	if cause == nil {
		return failf(nil, "room_open %s: no change.triggered in the change", p.Ticket)
	}
	req, err := r.request(ctx, p.Ticket, fx.Key)
	if err != nil {
		return err
	}
	room, err := r.deps.Chat.OpenRoom(ctx, req)
	if err != nil {
		return failf(err, "room_open %s: open room", p.Ticket)
	}
	payload := change.RoomOpened{RoomID: room}
	if err := payload.Validate(); err != nil {
		return failf(err, "room_open %s: room id refused", p.Ticket)
	}
	ev, err := newEvent(r.deps.IDs, r.deps.Clock, *cause, change.EventRoomOpened,
		change.Actor{Kind: change.ActorSwitch, ID: switchActorID}, payload)
	if err != nil {
		return failf(err, "room_open %s: build event", p.Ticket)
	}
	if err := r.deps.Ingest.Ingest(ctx, p.Ticket, []change.Event{ev}); err != nil {
		return failf(err, "room_open %s: ingest room.opened", p.Ticket)
	}
	return nil
}

// request reads the title of the ticket and builds the room request with the effect key.
func (r RoomOpen) request(ctx context.Context, cid id.ChangeID, key change.EffectKey) (ports.RoomRequest, error) {
	t, err := r.deps.Tickets.Ticket(ctx, cid)
	if err != nil {
		return ports.RoomRequest{}, failf(err, "room_open %s: read ticket", cid)
	}
	req := ports.RoomRequest{ChangeID: cid, Title: truncate(t.Title, maxRoomTitle), Key: key}
	if err := req.Validate(); err != nil {
		return ports.RoomRequest{}, failf(err, "room_open %s: room request refused", cid)
	}
	return req, nil
}

// roomPayload checks the kind and the target of a room_open effect and decodes its payload.
func roomPayload(fx change.Effect) (change.RoomOpenPayload, error) {
	if fx.Kind != change.EffectRoomOpen {
		return change.RoomOpenPayload{}, failf(nil, "room_open executor: effect %s has kind %s", fx.Key, fx.Kind)
	}
	p, err := change.DecodeRoomOpenPayload(fx.Payload)
	if err != nil {
		return change.RoomOpenPayload{}, failf(err, "room_open effect %s: payload refused", fx.Key)
	}
	if err := checkTarget(fx, p.Ticket); err != nil {
		return change.RoomOpenPayload{}, err
	}
	return p, nil
}

// maxRoomTitle is the limit of the title in a room request (ports.RoomRequest.Validate).
const maxRoomTitle = 256
