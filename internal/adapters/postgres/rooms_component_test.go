//go:build component

package postgres

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

type openRoomLister interface {
	OpenRooms(context.Context) ([]ports.RoomBinding, error)
}

func TestOpenRoomsListsTheLatestRoomForEachChange(t *testing.T) {
	ctx := context.Background()
	srv := startServer(t)
	db := srv.database(ctx, t)
	store := NewStore(db.pool)
	payEvents := []change.Event{
		discoveryEvent(id.ChangeID("PAY-1"), 1, change.EventChangeTriggered, `{"ticket":"PAY-1","repo":"acme/pay-demo","assignee":"engine","source":"manual"}`),
		discoveryEvent(id.ChangeID("PAY-1"), 2, change.EventRoomOpened, `{"room_id":"room-current"}`),
	}
	_, err := store.Append(ctx, id.ChangeID("PAY-1"), 0, payEvents, nil)
	require.NoError(t, err)
	kanEvents := []change.Event{
		discoveryEvent(id.ChangeID("KAN-2"), 1, change.EventChangeTriggered, `{"ticket":"KAN-2","repo":"acme/pay-demo","assignee":"engine","source":"manual"}`),
		discoveryEvent(id.ChangeID("KAN-2"), 2, change.EventRoomOpened, `{"room_id":"room-other"}`),
	}
	_, err = store.Append(ctx, id.ChangeID("KAN-2"), 0, kanEvents, nil)
	require.NoError(t, err)

	lister, ok := any(store).(openRoomLister)
	require.True(t, ok, "Postgres store must expose room bindings for the coordinator stream")
	rooms, err := lister.OpenRooms(ctx)
	require.NoError(t, err)
	require.Equal(t, []ports.RoomBinding{
		{ChangeID: id.ChangeID("KAN-2"), RoomID: change.RoomID("room-other")},
		{ChangeID: id.ChangeID("PAY-1"), RoomID: change.RoomID("room-current")},
	}, rooms)
}
