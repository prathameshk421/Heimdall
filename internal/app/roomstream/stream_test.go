package roomstream

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

type roomIndexFake struct{ rooms []ports.RoomBinding }

func (f roomIndexFake) OpenRooms(context.Context) ([]ports.RoomBinding, error) { return f.rooms, nil }

type cursorFake struct {
	values map[string]string
	sets   []string
}

func (f *cursorFake) GetCursor(_ context.Context, key string) (string, error) {
	if value, ok := f.values[key]; ok {
		return value, nil
	}
	return "", ports.ErrNotFound
}
func (f *cursorFake) SetCursor(_ context.Context, key, value string) error {
	if f.values == nil {
		f.values = make(map[string]string)
	}
	f.values[key] = value
	f.sets = append(f.sets, key+"="+value)
	return nil
}

type handlerFake struct {
	room    ports.RoomBinding
	message ports.Message
	err     error
}

func (f *handlerFake) HandleRoomMessage(_ context.Context, room ports.RoomBinding, msg ports.Message) error {
	f.room, f.message = room, msg
	return f.err
}

// multiStream replays all messages after the room's current cursor, as Switch's read_context does.
type multiStream struct {
	deliveries []ports.ChatDelivery
}

func (s *multiStream) StreamRooms(ctx context.Context, _ []ports.RoomBinding, cursors map[change.RoomID]ports.Cursor, deliver func(context.Context, ports.ChatDelivery) error) error {
	seenCursor := false
	for _, d := range s.deliveries {
		cur := cursors[d.Message.Room]
		if cur != "" && !seenCursor {
			if d.Cursor == cur {
				seenCursor = true
			}
			continue
		}
		if err := deliver(ctx, d); err != nil {
			return err
		}
	}
	return context.DeadlineExceeded
}

type selectiveHandler struct {
	failID  change.MessageID
	handled []change.MessageID
}

func (h *selectiveHandler) HandleRoomMessage(_ context.Context, _ ports.RoomBinding, msg ports.Message) error {
	if msg.ID == h.failID {
		return errors.New("permanent boom")
	}
	h.handled = append(h.handled, msg.ID)
	return nil
}

func TestFailingMessageDeadLettersAfterCapAndStreamContinues(t *testing.T) {
	room := ports.RoomBinding{ChangeID: id.ChangeID("PAY-3"), RoomID: change.RoomID("room-3")}
	now := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	stream := &multiStream{deliveries: []ports.ChatDelivery{
		{Message: ports.Message{ID: "m1", Room: room.RoomID, Sender: "alice", Text: "heimdall status", At: now}, Cursor: "c1"},
		{Message: ports.Message{ID: "m2", Room: room.RoomID, Sender: "alice", Text: "heimdall broken", At: now.Add(time.Second)}, Cursor: "c2"},
		{Message: ports.Message{ID: "m3", Room: room.RoomID, Sender: "alice", Text: "heimdall status", At: now.Add(2 * time.Second)}, Cursor: "c3"},
	}}
	cursors := &cursorFake{values: map[string]string{}}
	handler := &selectiveHandler{failID: "m2"}
	var deadIDs []change.MessageID
	svc, err := New(Config{
		Rooms: roomIndexFake{rooms: []ports.RoomBinding{room}}, Cursors: cursors, Stream: stream,
		Handler: handler, RefreshEvery: time.Minute, Wait: func(context.Context, time.Duration) error { return nil },
		MaxAttempts: 3,
		OnDead: func(_ context.Context, _ ports.RoomBinding, m ports.Message, _ error) {
			deadIDs = append(deadIDs, m.ID)
		},
	})
	require.NoError(t, err)

	// Attempts 1 and 2 fail on m2 after m1 succeeds.
	require.ErrorContains(t, svc.Cycle(context.Background()), "permanent boom")
	require.ErrorContains(t, svc.Cycle(context.Background()), "permanent boom")
	require.Equal(t, []change.MessageID{"m1"}, handler.handled)
	require.Empty(t, deadIDs)

	// Attempt 3 dead-letters m2, advances the cursor, and handles m3 in the same cycle.
	require.NoError(t, svc.Cycle(context.Background()))
	require.Equal(t, []change.MessageID{"m1", "m3"}, handler.handled)
	require.Equal(t, []change.MessageID{"m2"}, deadIDs)
	require.Contains(t, cursors.values[deadPrefix+"room-3:m2"], "permanent boom")
	require.Equal(t, "c3", cursors.values[cursorPrefix+"room-3"])
}

func TestNewRefusesMissingDependencies(t *testing.T) {
	_, err := New(Config{})
	require.ErrorContains(t, err, "Rooms")
	require.ErrorContains(t, err, "Cursors")
	require.ErrorContains(t, err, "Stream")
	require.ErrorContains(t, err, "Handler")
}

type refusingStream struct {
	calls [][]change.RoomID
	gone  change.RoomID
}

func (f *refusingStream) StreamRooms(_ context.Context, rooms []ports.RoomBinding, _ map[change.RoomID]ports.Cursor, _ func(context.Context, ports.ChatDelivery) error) error {
	var ids []change.RoomID
	for _, r := range rooms {
		ids = append(ids, r.RoomID)
	}
	f.calls = append(f.calls, ids)
	for _, id := range ids {
		if id == f.gone {
			return ports.ErrUnauthorized
		}
	}
	return context.DeadlineExceeded
}

// TestADeletedRoomIsDroppedSoTheOtherRoomsKeepStreaming is the live failure after old Discord rooms were deleted:
// Switch refused the whole stream (403) because one declared room was gone, and no room got messages.
func TestADeletedRoomIsDroppedSoTheOtherRoomsKeepStreaming(t *testing.T) {
	live := ports.RoomBinding{ChangeID: "KAN-19", RoomID: "room-live"}
	deleted := ports.RoomBinding{ChangeID: "KAN-12", RoomID: "room-deleted"}
	stream := &refusingStream{gone: deleted.RoomID}
	svc, err := New(Config{
		Rooms: roomIndexFake{rooms: []ports.RoomBinding{live, deleted}}, Cursors: &cursorFake{}, Stream: stream,
		Handler: &handlerFake{}, RefreshEvery: time.Minute, Wait: func(context.Context, time.Duration) error { return nil },
		Probe: func(_ context.Context, room change.RoomID) error {
			if room == deleted.RoomID {
				return ports.ErrNotFound
			}
			return nil
		},
	})
	require.NoError(t, err)
	require.ErrorIs(t, svc.Cycle(context.Background()), ports.ErrUnauthorized)
	require.NoError(t, svc.Cycle(context.Background()))
	require.Equal(t, []change.RoomID{live.RoomID}, stream.calls[1])
}
