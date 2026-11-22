package effects

import (
	"context"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// RoomPostDeps are the dependencies of RoomPost.
type RoomPostDeps struct {
	Chat ports.Chat
}

// RoomPost performs room_post effects: it posts one message to one Switch room with the effect key for idempotency
// (U1 asks and F2 close summaries).
type RoomPost struct {
	deps RoomPostDeps
}

// NewRoomPost builds the executor, or refuses when a dependency is missing.
func NewRoomPost(d RoomPostDeps) (RoomPost, error) {
	if d.Chat == nil {
		return RoomPost{}, fmt.Errorf("room_post executor: missing chat")
	}
	return RoomPost{deps: d}, nil
}

// Execute validates the effect and posts its message to the named room.
func (r RoomPost) Execute(ctx context.Context, fx change.Effect) error {
	if fx.Kind != change.EffectRoomPost {
		return failf(nil, "room_post executor: effect %s has kind %s", fx.Key, fx.Kind)
	}
	p, err := change.DecodeRoomPostPayload(fx.Payload)
	if err != nil {
		return failf(err, "room_post effect %s: payload refused", fx.Key)
	}
	if fx.Target != string(p.RoomID) {
		return failf(nil, "room_post effect %s: target %q is not room %s", fx.Key, fx.Target, p.RoomID)
	}
	if err := r.deps.Chat.Post(ctx, p.RoomID, fx.Key, p.Text); err != nil {
		return failf(err, "room_post %s: post message", p.RoomID)
	}
	return nil
}
