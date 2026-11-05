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

type streamFake struct {
	rooms    []ports.RoomBinding
	cursors  map[change.RoomID]ports.Cursor
	delivery ports.ChatDelivery
	end      error
}

func (f *streamFake) StreamRooms(ctx context.Context, rooms []ports.RoomBinding, cursors map[change.RoomID]ports.Cursor, deliver func(context.Context, ports.ChatDelivery) error) error {
	f.rooms, f.cursors = rooms, cursors
	if err := deliver(ctx, f.delivery); err != nil {
		return err
	}
	return f.end
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

func TestCycleRestoresRoomCursorAndPersistsItAfterHandler(t *testing.T) {
	room := ports.RoomBinding{ChangeID: id.ChangeID("PAY-1"), RoomID: change.RoomID("room-1")}
	msg := ports.Message{ID: change.MessageID("message-1"), Room: room.RoomID, Sender: change.SenderID("sender-1"), Text: "heimdall remember fact", At: time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)}
	cursors := &cursorFake{values: map[string]string{cursorPrefix + "room-1": "old-cursor"}}
	stream := &streamFake{delivery: ports.ChatDelivery{Message: msg, Cursor: "new-cursor", Replayed: true}, end: context.DeadlineExceeded}
	handler := &handlerFake{}
	svc, err := New(Config{
		Rooms: roomIndexFake{rooms: []ports.RoomBinding{room}}, Cursors: cursors, Stream: stream,
		Handler: handler, RefreshEvery: time.Minute, Wait: func(context.Context, time.Duration) error { return nil },
	})
	require.NoError(t, err)
	require.NoError(t, svc.Cycle(context.Background()))
	require.Equal(t, []ports.RoomBinding{room}, stream.rooms)
	require.Equal(t, ports.Cursor("old-cursor"), stream.cursors[room.RoomID])
	require.Equal(t, room, handler.room)
	require.Equal(t, msg.ID, handler.message.ID)
	require.Equal(t, "new-cursor", cursors.values[cursorPrefix+"room-1"])
}

func TestCycleDoesNotAdvanceCursorWhenHandlerFails(t *testing.T) {
	room := ports.RoomBinding{ChangeID: id.ChangeID("PAY-2"), RoomID: change.RoomID("room-2")}
	cursors := &cursorFake{values: map[string]string{}}
	stream := &streamFake{delivery: ports.ChatDelivery{
		Message: ports.Message{ID: "message-2", Room: room.RoomID, Sender: "sender-1", Text: "heimdall remember fact", At: time.Now()},
		Cursor:  "after-message",
	}}
	handler := &handlerFake{err: errors.New("action refused")}
	svc, err := New(Config{
		Rooms: roomIndexFake{rooms: []ports.RoomBinding{room}}, Cursors: cursors, Stream: stream,
		Handler: handler, RefreshEvery: time.Minute, Wait: func(context.Context, time.Duration) error { return nil },
	})
	require.NoError(t, err)
	err = svc.Cycle(context.Background())
	require.ErrorContains(t, err, "action refused")
	require.Empty(t, cursors.sets, "failed handling must be replayed after restart")
}

func TestNewRefusesMissingDependencies(t *testing.T) {
	_, err := New(Config{})
	require.ErrorContains(t, err, "Rooms")
	require.ErrorContains(t, err, "Cursors")
	require.ErrorContains(t, err, "Stream")
	require.ErrorContains(t, err, "Handler")
}
