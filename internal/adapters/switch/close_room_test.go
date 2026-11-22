package switchchat

import (
	"context"
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
