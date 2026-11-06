package switchchat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Post writes text to a room. It implements ports.Chat. This process does not send a key twice. The key is recorded
// only after Switch accepts the post, so the outbox can retry a failed one.
func (c *Client) Post(ctx context.Context, room change.RoomID, key change.EffectKey, text string) error {
	if key == "" {
		return errors.New("switch post: effect key is required")
	}
	c.postMu.Lock()
	defer c.postMu.Unlock()
	if c.posted[key] {
		return nil
	}
	err := c.guarded(ctx, "post", func(ctx context.Context) error {
		bearer, err := c.key()
		if err != nil {
			return err
		}
		raw, err := c.send(ctx, request{
			method: http.MethodPost,
			path:   "/agents/" + c.agentID + "/message",
			bearer: bearer,
			in:     map[string]string{"room_id": string(room), "content": text},
		})
		if err != nil {
			return err
		}
		var reply struct {
			OK bool `json:"ok"`
		}
		if err := json.Unmarshal(raw, &reply); err != nil {
			return fmt.Errorf("switch post: decode reply: %w", err)
		}
		if !reply.OK {
			return fmt.Errorf("%w: switch post: refused", ports.ErrUnavailable)
		}
		return nil
	})
	if err != nil {
		return err
	}
	c.posted[key] = true
	return nil
}

// readEntry is one message in a read_context thread.
type readEntry struct {
	ID        string `json:"id"`
	Sender    string `json:"sender"`
	Body      string `json:"body"`
	Timestamp *int64 `json:"timestamp"`
}

// readThread is one root message with its replies.
type readThread struct {
	Root    readEntry   `json:"root"`
	Replies []readEntry `json:"replies"`
}

// Read returns the messages at or after the cursor, oldest first, and the cursor of the newest one. Switch returns the
// message at the cursor again, so callers dedupe by ID. A truncated history is unavailable: nothing is returned, and
// the caller reads again from an earlier cursor.
func (c *Client) Read(ctx context.Context, room change.RoomID, after ports.Cursor) ([]ports.Message, ports.Cursor, error) {
	var msgs []ports.Message
	err := c.guarded(ctx, "read", func(ctx context.Context) error {
		args := map[string]any{"room_id": string(room)}
		if after != "" {
			args["since"] = string(after)
		}
		var reply struct {
			Threads   []readThread `json:"threads"`
			Truncated bool         `json:"truncated"`
		}
		if err := c.operation(ctx, "read_context", args, &reply); err != nil {
			return err
		}
		if reply.Truncated {
			return fmt.Errorf("%w: switch read_context: history truncated, read again from an earlier cursor", ports.ErrUnavailable)
		}
		out, err := flattenThreads(room, reply.Threads)
		if err != nil {
			return err
		}
		msgs = out
		return nil
	})
	if err != nil {
		return nil, after, err
	}
	next := after
	if n := len(msgs); n > 0 {
		next = cursorOf(msgs[n-1].At)
	}
	return msgs, next, nil
}

// flattenThreads turns each root and its replies into messages, ordered by time.
func flattenThreads(room change.RoomID, threads []readThread) ([]ports.Message, error) {
	var out []ports.Message
	for _, th := range threads {
		for _, e := range append([]readEntry{th.Root}, th.Replies...) {
			m, err := toMessage(room, e)
			if err != nil {
				return nil, err
			}
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out, nil
}

// toMessage turns one read_context entry into a message. A missing timestamp, or an id or sender that fails
// validation, is refused, not dropped, so no message is lost without an error.
func toMessage(room change.RoomID, e readEntry) (ports.Message, error) {
	if e.Timestamp == nil {
		return ports.Message{}, fmt.Errorf("switch read_context: message %q has no timestamp", e.ID)
	}
	m := ports.Message{
		ID:     change.MessageID(e.ID),
		Room:   room,
		Sender: change.SenderID(e.Sender),
		Text:   e.Body,
		At:     time.UnixMilli(*e.Timestamp).UTC(),
	}
	if err := m.Validate(); err != nil {
		return ports.Message{}, fmt.Errorf("switch read_context: message %q: %w", e.ID, err)
	}
	return m, nil
}

// cursorOf is the Switch cursor for a message time: RFC 3339 with nanoseconds, the form read_context takes as since.
func cursorOf(t time.Time) ports.Cursor {
	return ports.Cursor(t.UTC().Format(time.RFC3339Nano))
}
