package switchchat

import (
	"context"
	"errors"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Post writes text to a room. It implements ports.Chat.
func (c *Client) Post(ctx context.Context, room change.RoomID, key change.EffectKey, text string) error {
	return errors.New("not implemented")
}

// Read returns the messages after the cursor. It implements ports.Chat.
func (c *Client) Read(ctx context.Context, room change.RoomID, after ports.Cursor) ([]ports.Message, ports.Cursor, error) {
	return nil, "", errors.New("not implemented")
}
