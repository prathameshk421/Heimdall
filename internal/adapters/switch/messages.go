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

// readPageSize is how many entries one read_context call asks for: Switch's HISTORY_MAX_LIMIT, so a catch-up needs as
// few pages as Switch allows.
const readPageSize = 500

// Read returns the messages at or after the cursor, oldest first, and the cursor of the newest one. Switch returns the
// message at the cursor again, so callers dedupe by ID.
//
// Switch lists a room newest first and sets truncated when older entries in the window were left out (agent_core
// read_context: it asks for one row more than the limit). That is a page boundary, not lost history, so Read pages
// back with before until a page is not truncated. before is strict, so it is one millisecond past the oldest entry
// seen and the overlap is deduped by ID. A truncated page that brings nothing new cannot be paged past: ErrTruncated.
func (c *Client) Read(ctx context.Context, room change.RoomID, after ports.Cursor) ([]ports.Message, ports.Cursor, error) {
	seen := map[change.MessageID]bool{}
	var msgs []ports.Message
	var before time.Time
	for {
		page, oldest, err := c.readPage(ctx, room, after, before)
		if err != nil {
			return nil, after, err
		}
		added := 0
		for _, m := range page {
			if !seen[m.ID] {
				seen[m.ID] = true
				msgs = append(msgs, m)
				added++
			}
		}
		if oldest == nil {
			break
		}
		if added == 0 {
			return nil, after, fmt.Errorf("%w: room %s: a page before %s brought no new message", ErrTruncated, room, before.Format(time.RFC3339Nano))
		}
		before = time.UnixMilli(*oldest).Add(time.Millisecond)
	}
	sort.SliceStable(msgs, func(i, j int) bool { return msgs[i].At.Before(msgs[j].At) })
	next := after
	if n := len(msgs); n > 0 {
		next = cursorOf(msgs[n-1].At)
	}
	return msgs, next, nil
}

// readPage is one read_context call: the messages at or after the cursor and before before (no upper bound when
// before is zero), oldest first. When Switch left older ones out it also returns oldest_timestamp, the oldest entry of
// the page itself; a thread root fetched from outside the page does not count, so paging from it skips nothing.
func (c *Client) readPage(ctx context.Context, room change.RoomID, after ports.Cursor, before time.Time) ([]ports.Message, *int64, error) {
	var msgs []ports.Message
	var oldest *int64
	err := c.guarded(ctx, "read", func(ctx context.Context) error {
		args := map[string]any{"room_id": string(room), "limit": readPageSize}
		if after != "" {
			args["since"] = string(after)
		}
		if !before.IsZero() {
			args["before"] = string(cursorOf(before))
		}
		var reply struct {
			Threads   []readThread `json:"threads"`
			Truncated bool         `json:"truncated"`
			Oldest    *int64       `json:"oldest_timestamp"`
		}
		if err := c.operation(ctx, "read_context", args, &reply); err != nil {
			return err
		}
		out, err := flattenThreads(room, reply.Threads)
		if err != nil {
			return err
		}
		msgs = out
		if reply.Truncated {
			if reply.Oldest == nil {
				return fmt.Errorf("%w: room %s: a truncated page has no oldest_timestamp", ErrTruncated, room)
			}
			oldest = reply.Oldest
		}
		return nil
	})
	return msgs, oldest, err
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
