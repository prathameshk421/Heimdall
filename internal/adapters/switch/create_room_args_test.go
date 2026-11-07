package switchchat

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Switch's create_room requires a description (operations/definitions.py): a call without one is refused, so a
// change's room would never open. The client sends it (A3, WP1).
func TestOpenRoomSendsTheDescriptionSwitchRequires(t *testing.T) {
	f := newFakeSwitch(t)
	var got map[string]any
	f.ops["create_room"] = func(args map[string]any) (int, any) {
		got = args
		return http.StatusOK, map[string]any{"id": testRoomUUID}
	}
	c, _ := newTestClient(t, f)
	_, err := c.OpenRoom(context.Background(), ports.RoomRequest{ChangeID: "PAY-142", Title: "PAY-142 refunds", Key: "k1"})
	require.NoError(t, err)
	description, ok := got["description"].(string)
	require.True(t, ok, "create_room needs a description")
	require.NotEmpty(t, description)
}
