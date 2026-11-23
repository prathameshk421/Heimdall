package switchchat

import (
	"context"
	"fmt"
	"net/http"
	"sort"
)

// DirectoryUser is one person or agent in the Switch directory. SenderID is the id a chat message carries, and it is
// what `heimdall connect people` maps to a policy person. Type is "user" or "agent".
type DirectoryUser struct {
	Name     string
	SenderID string
	Type     string
}

// ListUsers returns the Switch users and agents visible to this coordinator. It tries Switch's list_users operation
// first. Bridges that do not have that operation answer with an unknown-operation error (404, 405 or 400); in that
// case the directory is derived from the senders of recent room history and from the managed agents. The fallback is
// read-only, so a bridge without list_users still gets a usable directory.
func (c *Client) ListUsers(ctx context.Context) ([]DirectoryUser, error) {
	var users []DirectoryUser
	err := c.guarded(ctx, "list users", func(callCtx context.Context) error {
		var reply []directoryUserReply
		err := c.operation(callCtx, "list_users", map[string]any{}, &reply)
		if err == nil {
			users = directoryUsersFromReply(reply)
			return nil
		}
		if !isUnsupportedOperation(err) {
			return err
		}
		users, err = c.listUsersFallback(callCtx)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("switch list users: %w", err)
	}
	sort.Slice(users, func(i, j int) bool {
		if users[i].Name != users[j].Name {
			return users[i].Name < users[j].Name
		}
		if users[i].Type != users[j].Type {
			return users[i].Type < users[j].Type
		}
		return users[i].SenderID < users[j].SenderID
	})
	return users, nil
}

// directoryUserReply is one entry of a list_users reply. Switch versions have used several names for the same fields,
// so each is read under its common aliases. Missing pieces fall back to the display name.
type directoryUserReply struct {
	SenderID    string `json:"sender_id"`
	UserID      string `json:"user_id"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Type        string `json:"type"`
	Kind        string `json:"kind"`
	SenderKind  string `json:"sender_kind"`
}

func directoryUsersFromReply(reply []directoryUserReply) []DirectoryUser {
	out := make([]DirectoryUser, 0, len(reply))
	seen := map[string]bool{}
	for _, u := range reply {
		sender := firstNonEmpty(u.SenderID, u.UserID, u.ID)
		name := firstNonEmpty(u.DisplayName, u.Name, sender)
		if sender == "" {
			sender = name
		}
		if sender == "" || seen[sender] {
			continue
		}
		seen[sender] = true
		kind := firstNonEmpty(u.Type, u.Kind, u.SenderKind)
		if kind != "agent" {
			kind = "user"
		}
		out = append(out, DirectoryUser{Name: name, SenderID: sender, Type: kind})
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// isUnsupportedOperation reports whether Switch refused the operation because it does not implement it, rather than
// because the call failed. Only those statuses trigger the directory fallback.
func isUnsupportedOperation(err error) bool {
	if err == nil {
		return false
	}
	status, ok := StatusOf(err)
	return ok && (status == http.StatusNotFound || status == http.StatusMethodNotAllowed || status == http.StatusBadRequest)
}

// directorySender is one message author in a read_context thread.
type directorySender struct {
	Sender     string `json:"sender"`
	SenderName string `json:"sender_name"`
	Kind       string `json:"sender_kind"`
}

// listUsersFallback derives the directory from read-only data the coordinator already uses: the managed agents and the
// senders of recent messages in every non-archived room. Senders are deduplicated by id and agents are never reported
// as humans, even when a room history carries a message from one.
func (c *Client) listUsersFallback(ctx context.Context) ([]DirectoryUser, error) {
	users, seen, agentSenders, err := c.agentDirectoryUsers(ctx)
	if err != nil {
		return nil, err
	}
	rooms, err := c.listDirectoryRooms(ctx)
	if err != nil {
		return nil, err
	}
	for _, room := range rooms {
		senders, err := c.roomDirectorySenders(ctx, room)
		if err != nil {
			return nil, err
		}
		users = appendHumanUsers(users, seen, agentSenders, senders)
	}
	return users, nil
}

// agentDirectoryUsers reports the managed agents as agent entries, plus the id sets used to dedupe human senders.
func (c *Client) agentDirectoryUsers(ctx context.Context) ([]DirectoryUser, map[string]bool, map[string]bool, error) {
	var agents []managedAgentSummary
	if err := c.operation(ctx, "list_managed_agents", map[string]any{}, &agents); err != nil {
		return nil, nil, nil, err
	}
	users := make([]DirectoryUser, 0, len(agents))
	seen := map[string]bool{}
	agentSenders := map[string]bool{}
	for _, a := range agents {
		if a.Name == "" || seen[a.Name] {
			continue
		}
		seen[a.Name] = true
		agentSenders[a.Name] = true
		users = append(users, DirectoryUser{Name: a.Name, SenderID: a.Name, Type: "agent"})
	}
	return users, seen, agentSenders, nil
}

// listDirectoryRooms returns the ids of the non-archived rooms whose recent history carries human senders.
func (c *Client) listDirectoryRooms(ctx context.Context) ([]string, error) {
	var rooms []struct {
		RoomID   string `json:"room_id"`
		Archived bool   `json:"archived"`
	}
	if err := c.operation(ctx, "list_rooms", map[string]any{}, &rooms); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rooms))
	for _, rm := range rooms {
		if rm.RoomID != "" && !rm.Archived {
			out = append(out, rm.RoomID)
		}
	}
	return out, nil
}

// roomDirectorySenders flattens the roots and replies of one room's recent read_context page.
func (c *Client) roomDirectorySenders(ctx context.Context, roomID string) ([]directorySender, error) {
	var reply struct {
		Threads []struct {
			Root    directorySender   `json:"root"`
			Replies []directorySender `json:"replies"`
		} `json:"threads"`
	}
	args := map[string]any{"room_id": roomID, "limit": readPageSize}
	if err := c.operation(ctx, "read_context", args, &reply); err != nil {
		return nil, err
	}
	var out []directorySender
	for _, th := range reply.Threads {
		out = append(out, th.Root)
		out = append(out, th.Replies...)
	}
	return out, nil
}

// appendHumanUsers adds the human authors of one room to users, keeping the first display name seen for each sender.
func appendHumanUsers(users []DirectoryUser, seen, agentSenders map[string]bool, senders []directorySender) []DirectoryUser {
	for _, e := range senders {
		if e.Sender == "" || e.Kind == "agent" || agentSenders[e.Sender] || seen[e.Sender] {
			continue
		}
		name := e.SenderName
		if name == "" {
			name = e.Sender
		}
		seen[e.Sender] = true
		users = append(users, DirectoryUser{Name: name, SenderID: e.Sender, Type: "user"})
	}
	return users
}
