package switchchat

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

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
