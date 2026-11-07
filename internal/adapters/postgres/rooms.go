package postgres

import (
	"context"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// OpenRoom identifies a persisted, nonterminal change and its Switch room.
type OpenRoom = ports.RoomBinding

// OpenRooms discovers changes with an opened room that have not reached a terminal state. It validates the complete
// persisted history before returning any projection; malformed chains, event envelopes, payloads, or transitions
// fail the whole call. Results are ordered by change ID. A room may appear for several changes.
func (s *Store) OpenRooms(ctx context.Context) ([]ports.RoomBinding, error) {
	ids, err := s.changeIDs(ctx)
	if err != nil {
		return nil, err
	}
	var out []ports.RoomBinding
	for _, cid := range ids {
		room, ok, err := s.openRoom(ctx, cid)
		if err != nil {
			return nil, err
		}
		if ok {
			// Several changes can share one room: the standing tests and reviews channels serve every ticket.
			out = append(out, room)
		}
	}
	return out, nil
}

func (s *Store) changeIDs(ctx context.Context) ([]id.ChangeID, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT change_id COLLATE "C" AS change_id FROM event ORDER BY change_id`)
	if err != nil {
		return nil, fmt.Errorf("list changes with events: %w", err)
	}
	defer rows.Close()
	var ids []id.ChangeID
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("read change ID: %w", err)
		}
		cid, err := id.ParseChangeID(raw)
		if err != nil {
			return nil, fmt.Errorf("stored change ID %q: %w", raw, err)
		}
		ids = append(ids, cid)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read change IDs: %w", err)
	}
	return ids, nil
}

// ChangeIDs lists every change with durable events in deterministic order for replayable projections.
func (s *Store) ChangeIDs(ctx context.Context) ([]id.ChangeID, error) { return s.changeIDs(ctx) }

func (s *Store) openRoom(ctx context.Context, cid id.ChangeID) (OpenRoom, bool, error) {
	events, err := s.Load(ctx, cid)
	if err != nil {
		return OpenRoom{}, false, fmt.Errorf("discover room for %s: %w", cid, err)
	}
	if err := validateHistory(cid, events); err != nil {
		return OpenRoom{}, false, fmt.Errorf("discover room for %s: %w", cid, err)
	}
	state, err := change.Replay(events)
	if err != nil && !state.IsTerminal() {
		return OpenRoom{}, false, fmt.Errorf("replay history for %s: %w", cid, err)
	}
	// A refused event after a terminal state (an agent wrote to a change the operator had already closed) leaves the
	// change closed. Replay returns the state before the refused event, so a terminal state means the change ended
	// there, and it must not stop room discovery for every other change.
	if state.IsTerminal() {
		return OpenRoom{}, false, nil
	}
	roomID, err := roomIDFromEvents(cid, events)
	if err != nil {
		return OpenRoom{}, false, err
	}
	if roomID == "" {
		return OpenRoom{}, false, nil
	}
	return OpenRoom{ChangeID: cid, RoomID: roomID}, true, nil
}

func roomIDFromEvents(cid id.ChangeID, events []change.Event) (change.RoomID, error) {
	var roomID change.RoomID
	for _, event := range events {
		if event.Type != change.EventRoomOpened {
			continue
		}
		payload, err := change.Decode(event)
		if err != nil {
			return "", fmt.Errorf("decode room.opened for %s: %w", cid, err)
		}
		opened, ok := payload.(change.RoomOpened)
		if !ok {
			return "", fmt.Errorf("decode room.opened for %s: unexpected payload %T", cid, payload)
		}
		if roomID != "" {
			return "", fmt.Errorf("change %s has multiple room.opened events", cid)
		}
		roomID = opened.RoomID
	}
	return roomID, nil
}

// validateHistory checks the stored event rows before replaying them. VerifyChain checks versions and change IDs too.
func validateHistory(cid id.ChangeID, events []change.Event) error {
	if len(events) == 0 {
		return fmt.Errorf("change has no events")
	}
	for i, event := range events {
		if event.ChangeID != cid {
			return fmt.Errorf("event %d belongs to change %s", i+1, event.ChangeID)
		}
		if err := event.Validate(); err != nil {
			return fmt.Errorf("event %d is invalid: %w", i+1, err)
		}
	}
	if err := change.VerifyChain(events); err != nil {
		return err
	}
	return nil
}
