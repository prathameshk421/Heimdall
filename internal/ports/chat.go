package ports

import (
	"context"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// RoomBinding connects one persisted change to its Switch room.
type RoomBinding struct {
	ChangeID id.ChangeID
	RoomID   change.RoomID
}

// RoomIndex lists rooms whose changes are still active. Consumer: the Switch coordinator stream.
type RoomIndex interface {
	OpenRooms(ctx context.Context) ([]RoomBinding, error)
}

// ChatDelivery is one message received by the coordinator stream.
type ChatDelivery struct {
	Message  Message
	Cursor   Cursor
	Replayed bool
}

// ChatStream streams messages for the configured rooms and reconciles each room from its stored cursor.
type ChatStream interface {
	StreamRooms(ctx context.Context, rooms []RoomBinding, cursors map[change.RoomID]Cursor, deliver func(context.Context, ChatDelivery) error) error
}

// CursorTable persists opaque cursors by source name. Cursor values are interpreted only by their producer.
type CursorTable interface {
	GetCursor(ctx context.Context, source string) (string, error)
	SetCursor(ctx context.Context, source, value string) error
}

// Chat is the Switch room of a change (TECH_SPEC 13, D127): create or adopt a room, post, and read. Every command is a
// text command, so nothing else is needed. Consumer: the use cases and the Switch stream. Adapter:
// internal/adapters/switch (task T26).
type Chat interface {
	// OpenRoom creates the room for r.ChangeID, or adopts the one an earlier request with the same key made.
	OpenRoom(ctx context.Context, r RoomRequest) (change.RoomID, error)
	// Post writes text to room. A repeat with the same key is not posted twice (TECH_SPEC 2.4).
	Post(ctx context.Context, room change.RoomID, key change.EffectKey, text string) error
	// Read returns the messages after the cursor and the cursor to continue from.
	Read(ctx context.Context, room change.RoomID, after Cursor) ([]Message, Cursor, error)
}
