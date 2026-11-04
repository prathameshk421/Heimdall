package switchchat

import (
	"context"
	"errors"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// ErrEvicted says Switch took the connection over. Stream returns it and does not reconnect.
var ErrEvicted = errors.New("switch connection evicted")

// Delivery is one message the stream hands to the caller.
type Delivery struct {
	Message  ports.Message
	Sequence int64
	Replayed bool
	Cursor   ports.Cursor
}

// DeliverFunc receives each delivery. A returned error stops the stream.
type DeliverFunc func(ctx context.Context, d Delivery) error

// Stream holds one connection for rooms and delivers messages until ctx ends or Switch evicts it.
func (c *Client) Stream(ctx context.Context, rooms []change.RoomID, resume map[change.RoomID]ports.Cursor, deliver DeliverFunc) error {
	return errors.New("not implemented")
}
