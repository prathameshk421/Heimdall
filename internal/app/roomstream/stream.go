// Package roomstream reconciles persisted room bindings and cursors before consuming Switch deliveries.
package roomstream

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const (
	cursorPrefix = "switch:room:"
	// goneprefix marks a room Switch no longer serves (deleted or left), so the stream stops asking for it.
	gonePrefix = "switch:room-gone:"
)

// Handler processes a message for the change whose room received it. It must be idempotent by Message.ID because a
// process can stop after applying a message but before its cursor write becomes durable.
type Handler interface {
	HandleRoomMessage(ctx context.Context, room ports.RoomBinding, message ports.Message) error
}

// SharedHandler is a Handler that can also take a message from a room that serves several changes. The handler finds
// the ticket the message is about and, when it cannot, asks the sender which one.
type SharedHandler interface {
	HandleSharedRoomMessage(ctx context.Context, room change.RoomID, candidates []ports.RoomBinding, message ports.Message) error
}

// Config supplies the durable room/cursor stores and the Switch stream.
type Config struct {
	Rooms   ports.RoomIndex
	Cursors ports.CursorTable
	Stream  ports.ChatStream
	Handler Handler
	// Standing lists rooms that serve every open change besides the room it opened: the reviews channel, and the
	// Telegram and Mattermost rooms. A command typed there names its ticket.
	Standing     []change.RoomID
	RefreshEvery time.Duration
	Wait         func(context.Context, time.Duration) error
	// Probe reads one room. When Switch refuses the whole stream, the service probes each room, and a room the probe
	// finds missing or refused is dropped, so one deleted room cannot stop every other room. Nil turns this off.
	Probe func(ctx context.Context, room change.RoomID) error
}

// Service runs the coordinator stream. Call Run once; the Switch adapter reconnects dropped streams, while this
// service periodically refreshes the room set and reloads cursors so new rooms and restarts are reconciled.
type Service struct {
	rooms        ports.RoomIndex
	cursors      ports.CursorTable
	stream       ports.ChatStream
	handler      Handler
	standing     []change.RoomID
	refreshEvery time.Duration
	wait         func(context.Context, time.Duration) error
	probe        func(ctx context.Context, room change.RoomID) error
}

// New validates dependencies and builds the stream service.
func New(cfg Config) (*Service, error) {
	var missing []string
	if cfg.Rooms == nil {
		missing = append(missing, "Rooms")
	}
	if cfg.Cursors == nil {
		missing = append(missing, "Cursors")
	}
	if cfg.Stream == nil {
		missing = append(missing, "Stream")
	}
	if cfg.Handler == nil {
		missing = append(missing, "Handler")
	}
	if cfg.RefreshEvery <= 0 {
		missing = append(missing, "RefreshEvery")
	}
	if cfg.Wait == nil {
		missing = append(missing, "Wait")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("room stream: missing or invalid: %s", strings.Join(missing, ", "))
	}
	return &Service{
		rooms: cfg.Rooms, cursors: cfg.Cursors, stream: cfg.Stream, handler: cfg.Handler, standing: cfg.Standing,
		refreshEvery: cfg.RefreshEvery, wait: cfg.Wait, probe: cfg.Probe,
	}, nil
}

// Run refreshes the room list at a bounded interval. Stream errors are returned to the server loop, which logs them
// and restarts with the durable cursors.
func (s *Service) Run(ctx context.Context) error {
	for {
		cycleCtx, cancel := context.WithTimeout(ctx, s.refreshEvery)
		err := s.Cycle(cycleCtx)
		cancel()
		if ctx.Err() != nil {
			return fmt.Errorf("room stream: %w", ctx.Err())
		}
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if err := s.wait(ctx, time.Second); err != nil {
			return fmt.Errorf("room stream wait: %w", err)
		}
	}
}

// Cycle loads the active rooms and their saved cursors, then consumes one stream interval. A handler error stops the
// cycle before saving that delivery's cursor, allowing read_context to replay it on the next cycle.
func (s *Service) Cycle(ctx context.Context) error {
	rooms, err := s.rooms.OpenRooms(ctx)
	if err != nil {
		return fmt.Errorf("load active rooms: %w", err)
	}
	rooms = s.withStanding(rooms)
	rooms, err = s.withoutGone(ctx, rooms)
	if err != nil {
		return err
	}
	byRoom, cursors, err := s.restoreRoomCursors(ctx, rooms)
	if err != nil {
		return err
	}
	err = s.stream.StreamRooms(ctx, uniqueRooms(rooms), cursors, func(ctx context.Context, d ports.ChatDelivery) error {
		return s.deliver(ctx, byRoom, d)
	})
	if errors.Is(err, ports.ErrUnauthorized) || errors.Is(err, ports.ErrNotFound) {
		if perr := s.markGone(ctx, uniqueRooms(rooms)); perr != nil {
			return errors.Join(fmt.Errorf("consume Switch stream: %w", err), perr)
		}
	}
	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("consume Switch stream: %w", err)
	}
	return nil
}

// withoutGone drops the rooms marked gone.
func (s *Service) withoutGone(ctx context.Context, rooms []ports.RoomBinding) ([]ports.RoomBinding, error) {
	kept := rooms[:0:0]
	for _, r := range rooms {
		_, err := s.cursors.GetCursor(ctx, gonePrefix+string(r.RoomID))
		if errors.Is(err, ports.ErrNotFound) {
			kept = append(kept, r)
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read gone mark of room %s: %w", r.RoomID, err)
		}
	}
	return kept, nil
}

// markGone probes each room after Switch refused the stream and marks the rooms it no longer serves. A room the probe
// cannot judge (Switch unavailable) stays, so a passing outage never drops a room.
func (s *Service) markGone(ctx context.Context, rooms []ports.RoomBinding) error {
	if s.probe == nil {
		return nil
	}
	for _, r := range rooms {
		room := r.RoomID
		err := s.probe(ctx, room)
		if !errors.Is(err, ports.ErrUnauthorized) && !errors.Is(err, ports.ErrNotFound) && !errors.Is(err, ports.ErrInvalidRequest) {
			continue
		}
		if err := s.cursors.SetCursor(ctx, gonePrefix+string(room), "1"); err != nil {
			return fmt.Errorf("mark room %s gone: %w", room, err)
		}
	}
	return nil
}

func (s *Service) restoreRoomCursors(ctx context.Context, rooms []ports.RoomBinding) (map[change.RoomID][]ports.RoomBinding, map[change.RoomID]ports.Cursor, error) {
	byRoom := make(map[change.RoomID][]ports.RoomBinding, len(rooms))
	cursors := make(map[change.RoomID]ports.Cursor, len(rooms))
	for _, room := range rooms {
		if _, err := idForRoom(room); err != nil {
			return nil, nil, err
		}
		byRoom[room.RoomID] = append(byRoom[room.RoomID], room)
		value, err := s.cursors.GetCursor(ctx, cursorPrefix+string(room.RoomID))
		if err != nil && !errors.Is(err, ports.ErrNotFound) {
			return nil, nil, fmt.Errorf("load cursor for room %s: %w", room.RoomID, err)
		}
		if err == nil {
			cursors[room.RoomID] = ports.Cursor(value)
		}
	}
	return byRoom, cursors, nil
}

func (s *Service) deliver(ctx context.Context, rooms map[change.RoomID][]ports.RoomBinding, d ports.ChatDelivery) error {
	candidates, ok := rooms[d.Message.Room]
	if !ok || len(candidates) == 0 {
		return fmt.Errorf("message %s arrived for unindexed room %s", d.Message.ID, d.Message.Room)
	}
	room := candidates[0]
	var err error
	if shared, isShared := s.handler.(SharedHandler); isShared && len(candidates) > 1 {
		err = shared.HandleSharedRoomMessage(ctx, d.Message.Room, candidates, d.Message)
	} else {
		err = s.handler.HandleRoomMessage(ctx, room, d.Message)
	}
	if err != nil {
		return fmt.Errorf("handle room %s message %s: %w", room.RoomID, d.Message.ID, err)
	}
	if d.Cursor == "" {
		return errors.New("switch delivery has an empty cursor")
	}
	if err := s.cursors.SetCursor(ctx, cursorPrefix+string(room.RoomID), string(d.Cursor)); err != nil {
		return fmt.Errorf("save cursor for room %s: %w", room.RoomID, err)
	}
	return nil
}

// withStanding adds, for every open change, a binding in each standing room it is not already bound to.
func (s *Service) withStanding(rooms []ports.RoomBinding) []ports.RoomBinding {
	if len(s.standing) == 0 {
		return rooms
	}
	have := make(map[ports.RoomBinding]bool, len(rooms))
	var changes []id.ChangeID
	seen := map[id.ChangeID]bool{}
	for _, r := range rooms {
		have[r] = true
		if !seen[r.ChangeID] {
			seen[r.ChangeID] = true
			changes = append(changes, r.ChangeID)
		}
	}
	out := append([]ports.RoomBinding(nil), rooms...)
	for _, cid := range changes {
		for _, room := range s.standing {
			b := ports.RoomBinding{ChangeID: cid, RoomID: room}
			if !have[b] {
				have[b] = true
				out = append(out, b)
			}
		}
	}
	return out
}

// uniqueRooms lists each room once, in the order first seen: a room that serves several changes is streamed once.
func uniqueRooms(rooms []ports.RoomBinding) []ports.RoomBinding {
	seen := make(map[change.RoomID]bool, len(rooms))
	out := make([]ports.RoomBinding, 0, len(rooms))
	for _, r := range rooms {
		if !seen[r.RoomID] {
			seen[r.RoomID] = true
			out = append(out, r)
		}
	}
	return out
}

func idForRoom(room ports.RoomBinding) (change.RoomID, error) {
	if _, err := change.ParseRoomID(string(room.RoomID)); err != nil {
		return "", fmt.Errorf("room index returned invalid room id: %w", err)
	}
	return room.RoomID, nil
}
