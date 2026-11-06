package switchchat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Check authenticates to Switch and performs a read-only coordinator room listing.
func (c *Client) Check(ctx context.Context) error {
	return c.guarded(ctx, "check", func(callCtx context.Context) error {
		var rooms []json.RawMessage
		return c.operation(callCtx, "list_rooms", map[string]any{}, &rooms)
	})
}

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

// Format hides the key for every verb. Without it, %#v would print the unexported field.
func (k Key) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, "[redacted]") }

// String hides the key from fmt and logs.
func (r Registration) String() string {
	return "registration{agent=" + r.AgentID + ", api_key=" + r.APIKey.String() + "}"
}

// RegisterCoordinator registers the coordinator agent with the registration token. It reads the token file, not the
// API key file, because the key does not exist until this call returns it.
func (c *Client) RegisterCoordinator(ctx context.Context, agentType, name, description string) (Registration, error) {
	var reg Registration
	err := c.guarded(ctx, "register", func(ctx context.Context) error {
		token, err := c.secrets.Secret(c.regSecret)
		if err != nil {
			return fmt.Errorf("switch: read registration secret: %w", err)
		}
		raw, err := c.send(ctx, request{
			method: http.MethodPost,
			path:   "/agents/register-known",
			bearer: token,
			in:     map[string]string{"agent_type": agentType, "name": name, "description": description},
		})
		if err != nil {
			return err
		}
		var reply struct {
			ID     string `json:"id"`
			APIKey string `json:"api_key"`
		}
		if err := json.Unmarshal(raw, &reply); err != nil {
			return fmt.Errorf("switch register: decode reply: %w", err)
		}
		if reply.ID == "" || reply.APIKey == "" {
			return errors.New("switch register: reply has no agent id or key")
		}
		reg = Registration{AgentID: reply.ID, APIKey: Key{value: reply.APIKey}}
		return nil
	})
	return reg, err
}

// OpenRoom creates the change room, or adopts the unarchived room with the same title. It implements ports.Chat. The
// adoption is by title, so a repeat request after a lost reply finds the room it made.
func (c *Client) OpenRoom(ctx context.Context, r ports.RoomRequest) (change.RoomID, error) {
	if err := r.Validate(); err != nil {
		return "", fmt.Errorf("switch open room: %w", err)
	}
	var room change.RoomID
	err := c.guarded(ctx, "open room", func(ctx context.Context) error {
		id, found, err := c.findRoom(ctx, r.Title)
		if err != nil {
			return err
		}
		if !found {
			id, err = c.createRoom(ctx, r.Title, fmt.Sprint(r.ChangeID))
			if err != nil {
				return err
			}
		}
		room = id
		return nil
	})
	return room, err
}

// findRoom returns the first unarchived room with the title.
func (c *Client) findRoom(ctx context.Context, title string) (change.RoomID, bool, error) {
	var rooms []struct {
		RoomID   string `json:"room_id"`
		Name     string `json:"name"`
		Archived bool   `json:"archived"`
	}
	if err := c.operation(ctx, "list_rooms", map[string]any{}, &rooms); err != nil {
		return "", false, err
	}
	for _, rm := range rooms {
		if rm.Name != title || rm.Archived {
			continue
		}
		id, err := change.ParseRoomID(rm.RoomID)
		if err != nil {
			return "", false, fmt.Errorf("switch list_rooms: room id: %w", err)
		}
		return id, true, nil
	}
	return "", false, nil
}

// createRoom makes a public channel on the configured bridge with the coordinator listed, so it joins its own room.
func (c *Client) createRoom(ctx context.Context, title, changeID string) (change.RoomID, error) {
	var reply struct {
		ID string `json:"id"`
	}
	args := map[string]any{
		"name": title,
		// Switch 0.31 rejects create_room without a description.
		"description":  "Heimdall review room for " + changeID,
		"agent_names":  []string{c.agentName},
		"channel_type": "channel_public",
		"bridge_id":    c.bridgeID,
	}
	if err := c.operation(ctx, "create_room", args, &reply); err != nil {
		return "", err
	}
	id, err := change.ParseRoomID(reply.ID)
	if err != nil {
		return "", fmt.Errorf("switch create_room: room id: %w", err)
	}
	return id, nil
}

// InviteAgent adds an agent to a room by its Switch name.
func (c *Client) InviteAgent(ctx context.Context, room change.RoomID, agentName string) error {
	return c.guarded(ctx, "invite agent", func(ctx context.Context) error {
		var reply struct {
			OK bool `json:"ok"`
		}
		args := map[string]any{"room_id": string(room), "agent_name": agentName}
		if err := c.operation(ctx, "invite_agent_to_room", args, &reply); err != nil {
			return err
		}
		if !reply.OK {
			return errors.New("switch invite_agent_to_room: refused")
		}
		return nil
	})
}

// Reference is a pointer to external content a room can carry.
type Reference struct {
	Type, Name, Description, Instructions string
	Value                                 map[string]any
}

// CreateReference creates a reference and returns its id.
func (c *Client) CreateReference(ctx context.Context, ref Reference) (string, error) {
	var id string
	err := c.guarded(ctx, "create reference", func(ctx context.Context) error {
		var reply struct {
			ID string `json:"id"`
		}
		args := map[string]any{
			"type":         ref.Type,
			"name":         ref.Name,
			"description":  ref.Description,
			"instructions": ref.Instructions,
			"value":        ref.Value,
		}
		if err := c.operation(ctx, "create_reference", args, &reply); err != nil {
			return err
		}
		if reply.ID == "" {
			return errors.New("switch create_reference: reply has no id")
		}
		id = reply.ID
		return nil
	})
	return id, err
}

// AttachReference attaches an existing reference to a room.
func (c *Client) AttachReference(ctx context.Context, room change.RoomID, referenceID string) error {
	return c.guarded(ctx, "attach reference", func(ctx context.Context) error {
		var reply struct {
			OK bool `json:"ok"`
		}
		args := map[string]any{"room_id": string(room), "reference_id": referenceID}
		if err := c.operation(ctx, "attach_reference_to_room", args, &reply); err != nil {
			return err
		}
		if !reply.OK {
			return errors.New("switch attach_reference_to_room: refused")
		}
		return nil
	})
}
