package switchchat

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// testStart is the time of the first fake message. Later messages are offset from it in whole seconds.
var testStart = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

// at returns the time sec seconds after testStart.
func at(sec int) time.Time { return testStart.Add(time.Duration(sec) * time.Second) }

// entryMap is one read_context entry as the bridge sends it.
func entryMap(id, sender, body string, t time.Time) map[string]any {
	return map[string]any{
		"id": id, "kind": "message", "sender": sender, "sender_name": "dev",
		"body": body, "timestamp": t.UnixMilli(), "attachments": []any{},
	}
}

func TestPostSendsRoomAndBodyWithTheKeyFromTheSecretFile(t *testing.T) {
	f := newFakeSwitch(t)
	c, _ := newTestClient(t, f)
	err := c.Post(context.Background(), roomID(t, testRoomUUID), "room_post:PAY-142:1", "heimdall uphold F1")
	require.NoError(t, err)

	sent := f.sentMessages()
	require.Len(t, sent, 1)
	require.Equal(t, testRoomUUID, sent[0]["room_id"])
	require.Equal(t, "heimdall uphold F1", sent[0]["content"], "Switch names the text content")
	for _, r := range f.requests() {
		require.Equal(t, testAPIKey, r.Auth, "every call after registration uses the key file")
	}
}

func TestPostDoesNotSendTheSameKeyTwiceInOneProcess(t *testing.T) {
	f := newFakeSwitch(t)
	c, _ := newTestClient(t, f)
	ctx := context.Background()
	room := roomID(t, testRoomUUID)
	require.NoError(t, c.Post(ctx, room, "k1", "first"))
	require.NoError(t, c.Post(ctx, room, "k1", "first, retried by the outbox"))
	require.Len(t, f.sentMessages(), 1, "a repeat with the same key is not posted again")

	require.NoError(t, c.Post(ctx, roomID(t, testOtherRoom), "k2", "second"))
	sent := f.sentMessages()
	require.Len(t, sent, 2, "a different key is a different post")
	require.Equal(t, testOtherRoom, sent[1]["room_id"], "the post goes to the room it names")
}

func TestPostRetriesAKeyThatSwitchRefused(t *testing.T) {
	f := newFakeSwitch(t)
	c, _ := newTestClient(t, f)
	ctx := context.Background()
	room := roomID(t, testRoomUUID)

	f.mu.Lock()
	f.messageCode = http.StatusServiceUnavailable
	f.mu.Unlock()
	require.ErrorIs(t, c.Post(ctx, room, "k1", "milestone"), ports.ErrUnavailable)

	f.mu.Lock()
	f.messageCode = http.StatusOK
	f.mu.Unlock()
	require.NoError(t, c.Post(ctx, room, "k1", "milestone"), "a failed key is sent again, so the outbox can retry")
	require.Len(t, f.sentMessages(), 2)

	require.NoError(t, c.Post(ctx, room, "k1", "milestone"))
	require.Len(t, f.sentMessages(), 2, "after success the key is not sent again")
}

func TestPostStatusesMapToPortsErrors(t *testing.T) {
	f := newFakeSwitch(t)
	c, _ := newTestClient(t, f)
	ctx := context.Background()
	room := roomID(t, testRoomUUID)

	f.mu.Lock()
	f.messageCode = http.StatusUnauthorized
	f.mu.Unlock()
	require.ErrorIs(t, c.Post(ctx, room, "k1", "x"), ports.ErrUnauthorized)

	f.mu.Lock()
	f.messageCode = http.StatusBadGateway
	f.mu.Unlock()
	require.ErrorIs(t, c.Post(ctx, room, "k2", "x"), ports.ErrUnavailable)
}

func TestPostRequiresAnEffectKeyBeforeAnyRequest(t *testing.T) {
	f := newFakeSwitch(t)
	c, _ := newTestClient(t, f)
	require.Error(t, c.Post(context.Background(), roomID(t, testRoomUUID), "", "x"))
	require.Empty(t, f.sentMessages())
}

func TestReadDecodesThreadsOldestFirstAndFlattensReplies(t *testing.T) {
	f := newFakeSwitch(t)
	f.ops["read_context"] = func(map[string]any) (int, any) {
		return http.StatusOK, map[string]any{
			"threads": []any{
				map[string]any{
					"root":    entryMap("msg-2", testHumanSender, "second", at(2)),
					"replies": []any{entryMap("msg-3", testHumanSender, "third", at(3))},
				},
				map[string]any{
					"root":    entryMap("msg-1", testHumanSender, "first", at(1)),
					"replies": []any{},
				},
			},
			"truncated": false, "oldest_timestamp": nil,
		}
	}
	c, _ := newTestClient(t, f)
	room := roomID(t, testRoomUUID)
	msgs, _, err := c.Read(context.Background(), room, "")
	require.NoError(t, err)
	require.Len(t, msgs, 3)
	require.Equal(t, change.MessageID("msg-1"), msgs[0].ID)
	require.Equal(t, change.MessageID("msg-2"), msgs[1].ID)
	require.Equal(t, change.MessageID("msg-3"), msgs[2].ID, "a reply is flattened in time order")
	require.Equal(t, "third", msgs[2].Text)
	require.Equal(t, room, msgs[2].Room)
	require.Equal(t, change.SenderID(testHumanSender), msgs[2].Sender)
	require.True(t, msgs[0].At.Equal(at(1)))
}

func TestReadCursorIsTheNewestTimeAndGoesBackAsSince(t *testing.T) {
	f := newFakeSwitch(t)
	f.mu.Lock()
	f.history = []fakeEntry{
		{ID: "msg-1", Room: testRoomUUID, Sender: testHumanSender, Body: "first", TSms: at(1).UnixMilli()},
		{ID: "msg-2", Room: testRoomUUID, Sender: testHumanSender, Body: "second", TSms: at(2).UnixMilli()},
	}
	f.mu.Unlock()
	c, _ := newTestClient(t, f)
	room := roomID(t, testRoomUUID)
	ctx := context.Background()

	msgs, cur, err := c.Read(ctx, room, "")
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	require.Equal(t, ports.Cursor("2026-11-04T12:00:02Z"), cur, "the cursor is the newest message time, RFC 3339")

	again, next, err := c.Read(ctx, room, cur)
	require.NoError(t, err)
	require.Len(t, again, 1, "Switch returns the message at the cursor again; callers dedupe by ID")
	require.Equal(t, change.MessageID("msg-2"), again[0].ID)
	require.Equal(t, cur, next)

	calls := f.opCalls("read_context")
	require.Len(t, calls, 2)
	require.NotContains(t, calls[0], "since", "a first read sends no cursor")
	require.Equal(t, string(cur), calls[1]["since"])
}

func TestReadRefusesAMalformedSenderInsteadOfDroppingIt(t *testing.T) {
	f := newFakeSwitch(t)
	f.ops["read_context"] = func(map[string]any) (int, any) {
		return http.StatusOK, map[string]any{"threads": []any{
			map[string]any{"root": entryMap("msg-1", "", "no sender", at(1)), "replies": []any{}},
		}, "truncated": false}
	}
	c, _ := newTestClient(t, f)
	msgs, cur, err := c.Read(context.Background(), roomID(t, testRoomUUID), "")
	require.Error(t, err)
	require.Nil(t, msgs)
	require.Empty(t, cur)
}

func TestReadRefusesAMessageWithoutATimestamp(t *testing.T) {
	f := newFakeSwitch(t)
	noTime := entryMap("msg-1", testHumanSender, "no time", at(1))
	delete(noTime, "timestamp")
	f.ops["read_context"] = func(map[string]any) (int, any) {
		return http.StatusOK, map[string]any{"threads": []any{
			map[string]any{"root": noTime, "replies": []any{}},
		}, "truncated": false}
	}
	c, _ := newTestClient(t, f)
	_, _, err := c.Read(context.Background(), roomID(t, testRoomUUID), "")
	require.Error(t, err)
	require.NotErrorIs(t, err, ports.ErrUnavailable, "malformed data is an error, not a retry")
}

func TestReadTruncatedHistoryIsUnavailable(t *testing.T) {
	f := newFakeSwitch(t)
	f.ops["read_context"] = func(map[string]any) (int, any) {
		return http.StatusOK, map[string]any{"threads": []any{}, "truncated": true, "oldest_timestamp": at(9).UnixMilli()}
	}
	c, _ := newTestClient(t, f)
	msgs, cur, err := c.Read(context.Background(), roomID(t, testRoomUUID), "2026-11-04T12:00:01Z")
	require.ErrorIs(t, err, ErrTruncated, "the caller escalates; a read again from the same cursor cannot succeed")
	require.False(t, retryable(err), "a truncated history is not retried as an outage")
	require.Nil(t, msgs)
	require.Equal(t, ports.Cursor("2026-11-04T12:00:01Z"), cur, "a failed read returns the cursor it was given")
}
