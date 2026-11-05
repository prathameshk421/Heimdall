//go:build component

package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

func TestOpenRooms(t *testing.T) {
	ctx := context.Background()
	srv := startServer(t)
	s := NewStore(srv.database(ctx, t).pool)

	for _, tc := range []struct {
		id, room string
	}{
		{"PAY-143", "room_01HZX3K9QW7M2P5R8T0V4YB6CE"},
		{"PAY-142", "room_01HZX3K9QW7M2P5R8T0V4YB6CD"},
	} {
		cid := id.ChangeID(tc.id)
		trigger := discoveryEvent(cid, 1, change.EventChangeTriggered, `{"ticket":"`+tc.id+`","repo":"acme/pay-demo","assignee":"engine","source":"manual"}`)
		room := discoveryEvent(cid, 2, change.EventRoomOpened, `{"room_id":"`+tc.room+`"}`)
		_, err := s.Append(ctx, cid, 0, []change.Event{trigger, room}, nil)
		require.NoError(t, err)
	}

	// A terminal change is persisted but must not be returned as an open room.
	cid := id.ChangeID("PAY-144")
	trigger := discoveryEvent(cid, 1, change.EventChangeTriggered, `{"ticket":"PAY-144","repo":"acme/pay-demo","assignee":"engine","source":"manual"}`)
	terminal := discoveryEvent(cid, 2, change.EventChangeRejected, `{"reason":"closed"}`)
	_, err := s.Append(ctx, cid, 0, []change.Event{trigger, terminal}, nil)
	require.NoError(t, err)

	got, err := s.OpenRooms(ctx)
	require.NoError(t, err)
	require.Equal(t, []OpenRoom{
		{ChangeID: "PAY-142", RoomID: "room_01HZX3K9QW7M2P5R8T0V4YB6CD"},
		{ChangeID: "PAY-143", RoomID: "room_01HZX3K9QW7M2P5R8T0V4YB6CE"},
	}, got)

	// Append permits storage of catalog-valid events; discovery must still refuse a reducer-invalid history.
	badID := id.ChangeID("PAY-145")
	first := discoveryEvent(badID, 1, change.EventChangeTriggered, `{"ticket":"PAY-145","repo":"acme/pay-demo","assignee":"engine","source":"manual"}`)
	second := discoveryEvent(badID, 2, change.EventChangeTriggered, `{"ticket":"PAY-145","repo":"acme/pay-demo","assignee":"engine","source":"manual"}`)
	_, err = s.Append(ctx, badID, 0, []change.Event{first, second}, nil)
	require.NoError(t, err)
	_, err = s.OpenRooms(ctx)
	require.Error(t, err, "an inconsistent persisted history must fail discovery")
}

func discoveryEvent(cid id.ChangeID, version int64, typ change.EventType, payload string) change.Event {
	return change.Event{
		ID: newEventID(), ChangeID: cid, Version: version, Type: typ,
		Actor:         change.Actor{Kind: change.ActorSystem, ID: "engine"},
		OccurredAt:    base.Add(time.Duration(version) * time.Second),
		CorrelationID: "corr-1", Payload: json.RawMessage(payload),
	}
}
