package change

import (
	"encoding/json"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// RoomPostPayload is the payload of a room_post effect: one plain message to one Switch room (U1, F2).
type RoomPostPayload struct {
	RoomID RoomID `json:"room_id"`
	Text   string `json:"text"`
}

// Validate returns every problem with the payload.
func (p RoomPostPayload) Validate() error {
	var v problems
	_, err := ParseRoomID(string(p.RoomID))
	v.add("room_id", err)
	v.add("text", checkText(p.Text))
	return v.join()
}

// DecodeRoomPostPayload strictly decodes and validates a room_post payload.
func DecodeRoomPostPayload(raw json.RawMessage) (RoomPostPayload, error) {
	var p RoomPostPayload
	if err := decodeEffectPayload(raw, []string{"room_id", "text"}, &p); err != nil {
		return RoomPostPayload{}, fmt.Errorf("room_post payload: %w", err)
	}
	return p, nil
}

// RoomClosePayload is the payload of a room_close effect: archive the change's Switch room after merge (F2).
type RoomClosePayload struct {
	Change id.ChangeID `json:"change"`
	RoomID RoomID      `json:"room_id"`
}

// Validate returns every problem with the payload.
func (p RoomClosePayload) Validate() error {
	var v problems
	_, err := id.ParseChangeID(string(p.Change))
	v.add("change", err)
	_, err = ParseRoomID(string(p.RoomID))
	v.add("room_id", err)
	return v.join()
}

// DecodeRoomClosePayload strictly decodes and validates a room_close payload.
func DecodeRoomClosePayload(raw json.RawMessage) (RoomClosePayload, error) {
	var p RoomClosePayload
	if err := decodeEffectPayload(raw, []string{"change", "room_id"}, &p); err != nil {
		return RoomClosePayload{}, fmt.Errorf("room_close payload: %w", err)
	}
	return p, nil
}
