package effects

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Ingest hands the events of an executor to the log through reconcile.Recorder, which numbers them, appends them in
// one append and kicks the change (TECH_SPEC 5.2). The executor leaves Version and the hash fields to the recorder.
type Ingest interface {
	Ingest(ctx context.Context, changeID id.ChangeID, evs []change.Event) error
}

// Actor ids of the events that executors write. They name the adapter that reported the result.
const (
	switchActorID = change.ActorID("switch")
	githubActorID = change.ActorID("github")
)

// newFact builds an event that an executor records. The id comes from the IDs port and the time from the clock. The
// change id, the version, the causation and the correlation are set when the event is recorded (reconcile.Recorder).
func newFact(ids ports.IDs, clock ports.Clock, typ change.EventType, actor change.Actor, p change.Payload) (change.Event, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return change.Event{}, fmt.Errorf("encode %s: %w", typ, err)
	}
	return change.Event{ID: ids.NewEvent(), Type: typ, Actor: actor, OccurredAt: clock.Now(), Payload: raw}, nil
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
	Ingest  Ingest
	IDs     ports.IDs
	Clock   ports.Clock
	// Agents are the workflow agents the room is created with, so Switch delivers an @mention to each.
	Agents []string
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

// Execute opens the room and records room.opened. The effect key is the idempotency key of the room request, so Switch
// adopts a room that an earlier attempt made.
func (r RoomOpen) Execute(ctx context.Context, fx change.Effect) error {
	p, err := roomPayload(fx)
	if err != nil {
		return err
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
	ev, err := newFact(r.deps.IDs, r.deps.Clock, change.EventRoomOpened,
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
	if repo, n, err := cid.PullRequest(); err == nil {
		// A pull request that names no ticket has no tracker entry: the room is named after the pull request.
		req := ports.RoomRequest{ChangeID: cid, Title: truncate(fmt.Sprintf("PR %s#%d", repo, n), maxRoomTitle), Key: key, Agents: r.deps.Agents}
		if err := req.Validate(); err != nil {
			return ports.RoomRequest{}, failf(err, "room_open %s: room request refused", cid)
		}
		return req, nil
	}
	t, err := r.deps.Tickets.Ticket(ctx, cid)
	if err != nil {
		return ports.RoomRequest{}, failf(err, "room_open %s: read ticket", cid)
	}
	req := ports.RoomRequest{ChangeID: cid, Title: roomTitle(t), Key: key, Agents: r.deps.Agents}
	if err := req.Validate(); err != nil {
		return ports.RoomRequest{}, failf(err, "room_open %s: room request refused", cid)
	}
	return req, nil
}

// roomTitle leads with the ticket key. OpenRoom adopts an open room by its title, so two tickets with the same summary
// would otherwise share one room (found live on KAN-9 and KAN-11). The key also names the chat channel.
func roomTitle(t ports.Ticket) string {
	return truncate(string(t.Key)+" "+t.Title, maxRoomTitle)
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
