package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

// OpenRoom is the step switch.open_room: it asks for the Switch room of a change once the ticket is In Progress.
// Its with object is {"invite": [...]}; an entry is "role:<name>" or "person:<id>", and "person:$assignee" becomes the
// assignee of change.triggered.
type OpenRoom struct {
	invite []string
}

var _ Step = OpenRoom{}

// openRoomWith is the with object of switch.open_room.
type openRoomWith struct {
	Invite []string `json:"invite"`
}

// NewOpenRoom reads the with object of the step and refuses one that does not validate.
func NewOpenRoom(with json.RawMessage) (OpenRoom, error) {
	var w openRoomWith
	if err := decodeWith(with, []string{"invite"}, &w); err != nil {
		return OpenRoom{}, fmt.Errorf("switch.open_room with: %w", err)
	}
	if err := change.CheckInviteTemplate(w.Invite); err != nil {
		return OpenRoom{}, fmt.Errorf("switch.open_room with: %w", err)
	}
	return OpenRoom{invite: w.Invite}, nil
}

// Type returns the do name of the step.
func (OpenRoom) Type() string { return "switch.open_room" }

// Done reports whether the room exists.
func (OpenRoom) Done(c ChangeView) bool { return c.Has(change.EventRoomOpened) }

// Start returns nothing: the room is requested when change.triggered arrives.
func (OpenRoom) Start(context.Context, ChangeView, json.RawMessage) (Result, error) {
	return Result{}, nil
}

// OnEvent requests the room on change.triggered. The effect has no event of its own: the engine attaches it to the
// incoming event, so the request is committed with that event (TECH_SPEC 2.4).
func (o OpenRoom) OnEvent(_ context.Context, c ChangeView, e change.Event) (Result, error) {
	if e.Type != change.EventChangeTriggered || o.Done(c) {
		return Result{}, nil
	}
	p, err := change.Decode(e)
	if err != nil {
		return Result{}, fmt.Errorf("switch.open_room: read %s: %w", e.Type, err)
	}
	trig, ok := p.(change.Triggered)
	if !ok {
		return Result{}, fmt.Errorf("switch.open_room: payload of %s is not change.Triggered", e.Type)
	}
	if trig.Ticket != c.ID {
		return Result{}, fmt.Errorf("switch.open_room: trigger names %s, the change is %s", trig.Ticket, c.ID)
	}
	key, err := change.IdempotencyKey(c.ID, e.Version, change.EffectRoomOpen, "room")
	if err != nil {
		return Result{}, fmt.Errorf("switch.open_room: %w", err)
	}
	payload := change.RoomOpenPayload{
		Ticket: trig.Ticket,
		Invite: change.InviteWithAssignee(o.invite, trig.Assignee),
	}
	if err := payload.Validate(); err != nil {
		return Result{}, fmt.Errorf("switch.open_room: %w", err)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return Result{}, fmt.Errorf("switch.open_room: encode payload: %w", err)
	}
	return Result{Effects: []change.Effect{{
		Key:     key,
		Kind:    change.EffectRoomOpen,
		Target:  string(trig.Ticket),
		Payload: raw,
	}}}, nil
}

// OnTimeout returns nothing: the step has no deadline.
func (OpenRoom) OnTimeout(context.Context, ChangeView) (Result, error) { return Result{}, nil }

// decodeWith strictly decodes a with object: one JSON object, the required keys present, no unknown key, and nothing
// after the object. An empty with is refused.
func decodeWith(raw json.RawMessage, required []string, v any) error {
	if len(raw) == 0 {
		return errors.New("with is empty, send the keys of the step")
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		return fmt.Errorf("not a JSON object: %w", err)
	}
	if keys == nil {
		return errors.New("must be a JSON object, not null")
	}
	for _, k := range required {
		if _, ok := keys[k]; !ok {
			return fmt.Errorf("missing key %q", k)
		}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("has data after the object")
	}
	return nil
}
