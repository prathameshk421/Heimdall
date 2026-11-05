package switchchat

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

func TestRegisterCoordinatorUsesTheRegistrationTokenAndReturnsTheKeyOnce(t *testing.T) {
	f := newFakeSwitch(t)
	c, _ := newTestClient(t, f)
	reg, err := c.RegisterCoordinator(context.Background(), "coordinator", testAgentName, "Heimdall engine")
	require.NoError(t, err)
	require.Equal(t, testAgentID, reg.AgentID)
	require.Equal(t, testAPIKey, reg.APIKey.Value())

	reqs := f.requests()
	require.Len(t, reqs, 1)
	require.Equal(t, http.MethodPost, reqs[0].Method)
	require.Equal(t, "/agents/register-known", reqs[0].Path)
	require.Equal(t, testRegToken, reqs[0].Auth, "registration uses the registration token, not the API key")
	require.Equal(t, "coordinator", reqs[0].Body["agent_type"])
	require.Equal(t, testAgentName, reqs[0].Body["name"])
}

func TestRegistrationKeyDoesNotPrintInFormatting(t *testing.T) {
	f := newFakeSwitch(t)
	c, _ := newTestClient(t, f)
	reg, err := c.RegisterCoordinator(context.Background(), "coordinator", testAgentName, "Heimdall engine")
	require.NoError(t, err)
	require.NotContains(t, reg.String(), testAPIKey)
	require.Contains(t, reg.String(), "redacted")
}

func TestOpenRoomCreatesTheRoomWithTheCoordinatorAsMember(t *testing.T) {
	f := newFakeSwitch(t)
	c, _ := newTestClient(t, f)
	room, err := c.OpenRoom(context.Background(), ports.RoomRequest{
		ChangeID: "PAY-142", Title: "PAY-142 refunds", Key: "room_open:PAY-142:1:room",
	})
	require.NoError(t, err)
	require.NotEmpty(t, room)

	creates := f.opCalls("create_room")
	require.Len(t, creates, 1)
	require.Equal(t, "PAY-142 refunds", creates[0]["name"])
	require.Equal(t, []any{testAgentName}, creates[0]["agent_names"], "the coordinator must be listed to join its own room")
	require.Equal(t, "channel_public", creates[0]["channel_type"])
}

func TestOpenRoomAdoptsTheRoomItAlreadyMadeInsteadOfCreatingAnother(t *testing.T) {
	f := newFakeSwitch(t)
	c, _ := newTestClient(t, f)
	first, err := c.OpenRoom(context.Background(), ports.RoomRequest{
		ChangeID: "PAY-142", Title: "PAY-142 refunds", Key: "room_open:PAY-142:1:room",
	})
	require.NoError(t, err)

	second, err := c.OpenRoom(context.Background(), ports.RoomRequest{
		ChangeID: "PAY-142", Title: "PAY-142 refunds", Key: "room_open:PAY-142:1:room",
	})
	require.NoError(t, err)
	require.Equal(t, first, second, "a repeat with the same title adopts the room")
	require.Len(t, f.opCalls("create_room"), 1, "no second room is created")
}

func TestOpenRoomPassesTheConfiguredBridge(t *testing.T) {
	f := newFakeSwitch(t)
	c, _ := newTestClient(t, f)
	c.bridgeID = testConnBridge
	_, err := c.OpenRoom(context.Background(), ports.RoomRequest{
		ChangeID: "PAY-142", Title: "PAY-142 refunds", Key: "k1",
	})
	require.NoError(t, err)
	creates := f.opCalls("create_room")
	require.Len(t, creates, 1)
	require.Equal(t, testConnBridge, creates[0]["bridge_id"])
}

func TestInviteAgentSendsRoomAndName(t *testing.T) {
	f := newFakeSwitch(t)
	c, _ := newTestClient(t, f)
	require.NoError(t, c.InviteAgent(context.Background(), roomID(t, testRoomUUID), "test-author"))
	calls := f.opCalls("invite_agent_to_room")
	require.Len(t, calls, 1)
	require.Equal(t, testRoomUUID, calls[0]["room_id"])
	require.Equal(t, "test-author", calls[0]["agent_name"])
}

func TestInviteAgentRefusesAnOkFalseReply(t *testing.T) {
	f := newFakeSwitch(t)
	f.ops["invite_agent_to_room"] = func(map[string]any) (int, any) { return http.StatusOK, map[string]any{"ok": false} }
	c, _ := newTestClient(t, f)
	require.Error(t, c.InviteAgent(context.Background(), roomID(t, testRoomUUID), "test-author"))
}

func TestOpenRoomDoesNotAdoptAnArchivedRoomWithTheSameTitle(t *testing.T) {
	f := newFakeSwitch(t)
	f.mu.Lock()
	f.rooms = []map[string]any{{"room_id": testOtherRoom, "name": "PAY-142 refunds", "archived": true}}
	f.mu.Unlock()
	c, _ := newTestClient(t, f)
	room, err := c.OpenRoom(context.Background(), ports.RoomRequest{
		ChangeID: "PAY-142", Title: "PAY-142 refunds", Key: "room_open:PAY-142:1:room",
	})
	require.NoError(t, err)
	require.NotEqual(t, testOtherRoom, string(room), "the archived room is not adopted")
	require.Len(t, f.opCalls("create_room"), 1, "a new room is made in its place")
}

func TestCreateAndAttachReferenceSendTheOperationArguments(t *testing.T) {
	f := newFakeSwitch(t)
	c, _ := newTestClient(t, f)
	ctx := context.Background()
	id, err := c.CreateReference(ctx, Reference{
		Type: "github_repo", Name: "PAY-142", Description: "change repo", Instructions: "read only",
		Value: map[string]any{"repo": "acme/pay-demo"},
	})
	require.NoError(t, err)
	require.Equal(t, "ref-1", id)

	require.NoError(t, c.AttachReference(ctx, roomID(t, testRoomUUID), id))
	refs := f.opCalls("create_reference")
	require.Len(t, refs, 1)
	require.Equal(t, "github_repo", refs[0]["type"])
	att := f.opCalls("attach_reference_to_room")
	require.Len(t, att, 1)
	require.Equal(t, testRoomUUID, att[0]["room_id"])
	require.Equal(t, "ref-1", att[0]["reference_id"])
}
