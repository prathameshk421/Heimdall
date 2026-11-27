package switchchat

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListUsersUsesListUsersOperation(t *testing.T) {
	f := newFakeSwitch(t)
	f.ops["list_users"] = func(map[string]any) (int, any) {
		return http.StatusOK, []any{
			map[string]any{"sender_id": "@bob", "display_name": "Bob", "sender_kind": "user"},
			map[string]any{"sender_id": "@alice", "display_name": "Alice", "type": "user"},
			map[string]any{"sender_id": "reviewer", "name": "reviewer", "type": "agent"},
		}
	}
	c, _ := newTestClient(t, f)

	users, err := c.ListUsers(t.Context())
	require.NoError(t, err)
	require.Equal(t, []DirectoryUser{
		{Name: "Alice", SenderID: "@alice", Type: "user"},
		{Name: "Bob", SenderID: "@bob", Type: "user"},
		{Name: "reviewer", SenderID: "reviewer", Type: "agent"},
	}, users)
	require.Len(t, f.opCalls("list_users"), 1, "the native operation must be tried first")
	require.Empty(t, f.opCalls("list_rooms"), "no fallback when list_users works")
}

func TestListUsersFallsBackWhenOperationIsUnsupported(t *testing.T) {
	f := newFakeSwitch(t)
	// No list_users hook: the fake answers an unknown operation with 404.
	f.ops["list_managed_agents"] = func(map[string]any) (int, any) {
		return http.StatusOK, []any{map[string]any{"agent_id": "a1", "name": "reviewer"}}
	}
	f.rooms = []map[string]any{{"room_id": testRoomUUID, "name": "engineering"}}
	f.ops["read_context"] = func(map[string]any) (int, any) {
		return http.StatusOK, map[string]any{"threads": []any{
			map[string]any{
				"root": map[string]any{"sender": "@alice", "sender_name": "Alice"},
				"replies": []any{
					map[string]any{"sender": "@bob", "sender_name": "Bob", "sender_kind": "user"},
					map[string]any{"sender": "reviewer", "sender_name": "reviewer", "sender_kind": "agent"},
				},
			},
		}}
	}
	c, _ := newTestClient(t, f)

	users, err := c.ListUsers(t.Context())
	require.NoError(t, err)
	require.Equal(t, []DirectoryUser{
		{Name: "Alice", SenderID: "@alice", Type: "user"},
		{Name: "Bob", SenderID: "@bob", Type: "user"},
		{Name: "reviewer", SenderID: "reviewer", Type: "agent"},
	}, users, "human senders come from history, agents from the managed-agent list, both deduped and sorted")
	require.Len(t, f.opCalls("list_users"), 1)
	require.Len(t, f.opCalls("list_rooms"), 1)
}

func TestListUsersPropagatesRealErrorsWithoutFallingBack(t *testing.T) {
	f := newFakeSwitch(t)
	f.ops["list_users"] = func(map[string]any) (int, any) {
		return http.StatusInternalServerError, map[string]any{"detail": "server error"}
	}
	c, _ := newTestClient(t, f)

	users, err := c.ListUsers(t.Context())
	require.Error(t, err)
	require.Contains(t, err.Error(), "list users")
	require.Nil(t, users)
	require.Empty(t, f.opCalls("list_rooms"), "a real failure must not reach the fallback")
}

func TestIsUnsupportedOperation(t *testing.T) {
	require.False(t, isUnsupportedOperation(nil))
	for _, code := range []int{http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusBadRequest} {
		require.True(t, isUnsupportedOperation(statusError("/ops/list_users", code)), "status %d", code)
	}
	require.False(t, isUnsupportedOperation(statusError("/ops/list_users", http.StatusInternalServerError)))
	require.False(t, isUnsupportedOperation(statusError("/ops/list_users", http.StatusUnauthorized)))
}
