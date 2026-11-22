package switchchat

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCloseRoomCallsArchiveRoom(t *testing.T) {
	f := newFakeSwitch(t)
	c, _ := newTestClient(t, f)
	require.NoError(t, c.CloseRoom(context.Background(), "switch:r1"))

	calls := f.opCalls("archive_room")
	require.Len(t, calls, 1)
	require.Equal(t, "switch:r1", calls[0]["room_id"])
}

func TestCloseRoomAcceptsArchivedRoomObjectWithoutOKFieldAndRefusesExplicitFalse(t *testing.T) {
	f := newFakeSwitch(t)
	f.ops["archive_room"] = func(map[string]any) (int, any) {
		return http.StatusOK, map[string]any{"id": "switch:r1", "archived": true}
	}
	c, _ := newTestClient(t, f)
	require.NoError(t, c.CloseRoom(context.Background(), "switch:r1"))

	f.ops["archive_room"] = func(map[string]any) (int, any) {
		return http.StatusOK, map[string]any{"ok": false}
	}
	require.Error(t, c.CloseRoom(context.Background(), "switch:r1"))
}
