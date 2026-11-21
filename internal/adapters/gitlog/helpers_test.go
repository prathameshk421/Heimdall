package gitlog

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// testKey is a valid MAC key. otherKey is another valid one.
var (
	testKey  = []byte("0123456789abcdef0123456789abcdef")
	otherKey = []byte("fedcba9876543210fedcba9876543210")
)

var eventSeq atomic.Int64

func newEventID() change.EventID {
	return change.EventID(fmt.Sprintf("00000000-0000-4000-8000-%012d", eventSeq.Add(1)))
}

var base = time.Date(2026, 11, 21, 9, 30, 0, 0, time.UTC)

func prChange(t *testing.T, n int) id.ChangeID {
	t.Helper()
	c, err := id.PRChangeID(testRepo, n)
	require.NoError(t, err)
	return c
}

// roomEvent is a valid room.opened event at version.
func roomEvent(cid id.ChangeID, version int64, room string) change.Event {
	return change.Event{
		ID:            newEventID(),
		ChangeID:      cid,
		Version:       version,
		Type:          change.EventRoomOpened,
		Actor:         change.Actor{Kind: change.ActorSystem, ID: "engine"},
		OccurredAt:    base.Add(time.Duration(version) * time.Second),
		CorrelationID: "corr-1",
		Payload:       json.RawMessage(`{"room_id":"` + room + `"}`),
	}
}

// roomEvents builds one room.opened event per room, at the versions after expected.
func roomEvents(cid id.ChangeID, expected int64, rooms ...string) []change.Event {
	out := make([]change.Event, 0, len(rooms))
	for i, room := range rooms {
		out = append(out, roomEvent(cid, expected+int64(i)+1, room))
	}
	return out
}

// roomEffect is an outbox row keyed for the change and version.
func roomEffect(t *testing.T, cid id.ChangeID, version int64, disc, payload string) change.Effect {
	t.Helper()
	key, err := change.IdempotencyKey(cid, version, change.EffectRoomPost, disc)
	require.NoError(t, err)
	return change.Effect{Key: key, Kind: change.EffectRoomPost, Target: "room-42", Payload: json.RawMessage(payload)}
}

// seed appends the rooms as one batch at version 0 and returns the new version.
func seed(ctx context.Context, t *testing.T, s *Store, cid id.ChangeID, rooms ...string) int64 {
	t.Helper()
	n, err := s.Append(ctx, cid, 0, roomEvents(cid, 0, rooms...), nil)
	require.NoError(t, err)
	return n
}

// moveOtherFile makes another writer commit a file that no change reads.
func moveOtherFile(g *fakeGit, n int) {
	g.writeFiles(map[string][]byte{"other/note.txt": []byte(fmt.Sprintf("write %d", n))})
}

// flipHex changes the first hex digit of s.
func flipHex(s string) string {
	if strings.HasPrefix(s, "0") {
		return "1" + s[1:]
	}
	return "0" + s[1:]
}

// noteEvent is a valid tests.submitted event whose payload carries a note of n bytes. It is how a test makes a large
// event, because the room id and most other fields are short by rule.
func noteEvent(cid id.ChangeID, version int64, n int) change.Event {
	e := roomEvent(cid, version, "x")
	e.Type = change.EventTestsSubmitted
	e.Payload = json.RawMessage(`{"refs":["x"],"ac_map":{},"notes":"` + strings.Repeat("a", n) + `"}`)
	return e
}
