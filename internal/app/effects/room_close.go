package effects

import (
	"context"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// RoomCloseDeps are the dependencies of RoomClose.
type RoomCloseDeps struct {
	Closer ports.RoomCloser
	Ingest Ingest
	IDs    ports.IDs
	Clock  ports.Clock
}

// RoomClose performs room_close effects: it archives the change's Switch room and records room.closed (F2).
type RoomClose struct {
	deps RoomCloseDeps
}

// NewRoomClose builds the executor, or refuses when a dependency is missing.
func NewRoomClose(d RoomCloseDeps) (RoomClose, error) {
	var missing []string
	if d.Closer == nil {
		missing = append(missing, "closer")
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
		return RoomClose{}, fmt.Errorf("room_close executor: missing %v", missing)
	}
	return RoomClose{deps: d}, nil
}

// Execute archives the room and ingests room.closed.
func (r RoomClose) Execute(ctx context.Context, fx change.Effect) error {
	if fx.Kind != change.EffectRoomClose {
		return failf(nil, "room_close executor: effect %s has kind %s", fx.Key, fx.Kind)
	}
	p, err := change.DecodeRoomClosePayload(fx.Payload)
	if err != nil {
		return failf(err, "room_close effect %s: payload refused", fx.Key)
	}
	if fx.Target != string(p.RoomID) {
		return failf(nil, "room_close effect %s: target %q is not room %s", fx.Key, fx.Target, p.RoomID)
	}
	if err := r.deps.Closer.CloseRoom(ctx, p.RoomID); err != nil {
		return failf(err, "room_close %s: archive room", p.RoomID)
	}
	payload := change.RoomClosed{RoomID: p.RoomID}
	ev, err := newFact(r.deps.IDs, r.deps.Clock, change.EventRoomClosed,
		change.Actor{Kind: change.ActorSwitch, ID: switchActorID}, payload)
	if err != nil {
		return failf(err, "room_close %s: build event", p.Change)
	}
	if err := r.deps.Ingest.Ingest(ctx, p.Change, []change.Event{ev}); err != nil {
		return failf(err, "room_close %s: ingest room.closed", p.Change)
	}
	return nil
}
