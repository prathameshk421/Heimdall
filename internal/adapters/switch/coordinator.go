package switchchat

import (
	"context"
	"errors"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Registration is what Switch returns once for a new agent. APIKey is shown once; the caller writes it to a file.
type Registration struct {
	AgentID string
	APIKey  Key
}

// Key is an agent API key. It prints as [redacted], so formatting a Registration never shows it.
type Key struct {
	value string
}

// Value returns the key for the caller to write to its secret file.
func (k Key) Value() string { return k.value }

// String hides the key from fmt and logs.
func (k Key) String() string { return "[redacted]" }

// String hides the key from fmt and logs.
func (r Registration) String() string { return "registration{agent=" + r.AgentID + "}" }

// RegisterCoordinator registers the coordinator agent with the registration token.
func (c *Client) RegisterCoordinator(ctx context.Context, agentType, name, description string) (Registration, error) {
	return Registration{}, errors.New("not implemented")
}

// OpenRoom creates the change room, or adopts the room with the same title. It implements ports.Chat.
func (c *Client) OpenRoom(ctx context.Context, r ports.RoomRequest) (change.RoomID, error) {
	return "", errors.New("not implemented")
}

// InviteAgent adds an agent to a room by its Switch name.
func (c *Client) InviteAgent(ctx context.Context, room change.RoomID, agentName string) error {
	return errors.New("not implemented")
}

// Reference is a pointer to external content a room can carry.
type Reference struct {
	Type, Name, Description, Instructions string
	Value                                 map[string]any
}

// CreateReference creates a reference and returns its id.
func (c *Client) CreateReference(ctx context.Context, ref Reference) (string, error) {
	return "", errors.New("not implemented")
}

// AttachReference attaches an existing reference to a room.
func (c *Client) AttachReference(ctx context.Context, room change.RoomID, referenceID string) error {
	return errors.New("not implemented")
}
