package switchchat

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const (
	connA = "conn-a"
	connB = "conn-b"
)

// waitLog records every wait the client makes. It returns at once unless ctx is done, so no test sleeps.
type waitLog struct {
	mu        sync.Mutex
	durations []time.Duration
}

func (w *waitLog) wait(ctx context.Context, d time.Duration) error {
	w.mu.Lock()
	w.durations = append(w.durations, d)
	w.mu.Unlock()
	return ctx.Err()
}

func (w *waitLog) all() []time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]time.Duration(nil), w.durations...)
}

// deliveryLog collects what Stream hands to the caller.
type deliveryLog struct {
	mu  sync.Mutex
	got []Delivery
}

func (l *deliveryLog) deliver(_ context.Context, d Delivery) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.got = append(l.got, d)
	return nil
}

func (l *deliveryLog) all() []Delivery {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Delivery(nil), l.got...)
}

// streamTest is a client on the fake with a recording wait.
func streamTest(t *testing.T) (*fakeSwitch, *Client, *waitLog) {
	t.Helper()
	f := newFakeSwitch(t)
	c, _ := newTestClient(t, f)
	log := &waitLog{}
	c.wait = log.wait
	return f, c, log
}

// streamCtx bounds a Stream call. Stream reconnects until its context ends, so a regression that retries forever
// must fail the test instead of hanging it. The happy paths return long before the bound.
func streamCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// setStream installs the handler of the event stream under the fake's lock.
func setStream(f *fakeSwitch, fn func(w http.ResponseWriter, r *http.Request, n int)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.streamFn = fn
}

// setHistory replaces the fake's read_context history under the fake's lock.
func setHistory(f *fakeSwitch, h ...fakeEntry) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.history = h
}

// withHeartbeat sets the heartbeat interval a connection_state frame names.
func withHeartbeat(state map[string]any, seconds int64) map[string]any {
	state["heartbeat_interval_seconds"] = seconds
	return state
}

// streamOpens returns the requests that opened the event stream, in order.
func streamOpens(f *fakeSwitch) []recorded {
	var out []recorded
	for _, r := range f.requests() {
		if r.Method == http.MethodGet && r.Path == "/agents/"+testAgentID+"/events" {
			out = append(out, r)
		}
	}
	return out
}

// awaitBeat blocks the handler until one beat arrives, or until the request ends.
func awaitBeat(f *fakeSwitch, r *http.Request) {
	select {
	case <-f.beatCh:
	case <-r.Context().Done():
	}
}

func TestStreamOpensWithFilterAllTheRoomsAndTheKeyAndNoCursor(t *testing.T) {
	f, c, _ := streamTest(t)
	setStream(f, func(w http.ResponseWriter, _ *http.Request, _ int) {
		frame(w, 0, "connection_state", connectionState(connA, 1, testRoomUUID))
		frame(w, 0, "evicted", map[string]any{})
	})
	var got deliveryLog
	err := c.Stream(streamCtx(t), []change.RoomID{roomID(t, testRoomUUID)}, nil, got.deliver)
	require.ErrorIs(t, err, ErrEvicted)

	opens := streamOpens(f)
	require.Len(t, opens, 1)
	require.Equal(t, "text/event-stream", opens[0].Accept)
	require.Equal(t, testAPIKey, opens[0].Auth)
	require.Contains(t, opens[0].Query, "filter=all")
	require.Contains(t, opens[0].Query, "room_id="+testRoomUUID)
	require.Empty(t, opens[0].LastEventID, "the first connection has nothing to resume")
	require.Empty(t, opens[0].Connection)
	require.Empty(t, f.opCalls("read_context"), "no resume cursor means no history read")
}

func TestStreamDeliversSequenceAndCursorPerRoom(t *testing.T) {
	f, c, _ := streamTest(t)
	rooms := []change.RoomID{roomID(t, testRoomUUID), roomID(t, testOtherRoom)}
	setStream(f, func(w http.ResponseWriter, _ *http.Request, _ int) {
		frame(w, 0, "connection_state", connectionState(connA, 1, testRoomUUID, testOtherRoom))
		frame(w, 1, "message", messageFrame(testRoomUUID, "msg-1", testHumanSender, "first", at(1).UnixMilli()))
		frame(w, 2, "message", messageFrame(testOtherRoom, "msg-2", testHumanSender, "second", at(2).UnixMilli()))
		frame(w, 3, "message", messageFrame(testRoomUUID, "msg-3", testHumanSender, "third", at(3).UnixMilli()))
		frame(w, 0, "evicted", map[string]any{})
	})
	var got deliveryLog
	require.ErrorIs(t, c.Stream(streamCtx(t), rooms, nil, got.deliver), ErrEvicted)

	opens := streamOpens(f)
	require.Len(t, opens, 1)
	require.Contains(t, opens[0].Query, "room_id="+testRoomUUID)
	require.Contains(t, opens[0].Query, "room_id="+testOtherRoom, "every declared room is on the URL")

	d := got.all()
	require.Len(t, d, 3)
	require.Equal(t, int64(1), d[0].Sequence)
	require.Equal(t, change.MessageID("msg-1"), d[0].Message.ID)
	require.Equal(t, "first", d[0].Message.Text)
	require.Equal(t, change.SenderID(testHumanSender), d[0].Message.Sender)
	require.False(t, d[0].Replayed, "a message on the first connection is live")
	require.Equal(t, ports.Cursor("2026-11-04T12:00:01Z"), d[0].Cursor)

	require.Equal(t, int64(2), d[1].Sequence)
	require.Equal(t, testOtherRoom, d[1].Message.Room.String())
	require.Equal(t, ports.Cursor("2026-11-04T12:00:02Z"), d[1].Cursor, "the second room has its own cursor")

	require.Equal(t, ports.Cursor("2026-11-04T12:00:03Z"), d[2].Cursor, "the first room's cursor moved to its newest message")
}

func TestStreamSendsOneClientConnectionIDOnEveryOpenAsSwitchRequires(t *testing.T) {
	f, c, _ := streamTest(t)
	setStream(f, func(w http.ResponseWriter, _ *http.Request, n int) {
		if n == 1 {
			frame(w, 0, "connection_state", connectionState(connA, 1, testRoomUUID))
			return // a dropped connection
		}
		frame(w, 0, "connection_state", connectionState(connB, 1, testRoomUUID))
		frame(w, 0, "evicted", map[string]any{})
	})
	var got deliveryLog
	require.ErrorIs(t, c.Stream(streamCtx(t), []change.RoomID{roomID(t, testRoomUUID)}, nil, got.deliver), ErrEvicted)

	opens := streamOpens(f)
	require.Len(t, opens, 2)
	ids := make([]string, 0, 2)
	for _, o := range opens {
		q, err := url.ParseQuery(o.Query)
		require.NoError(t, err)
		id := q.Get("connection_id")
		_, perr := uuid.Parse(id)
		require.NoError(t, perr, "Switch 0.31 refuses a stream with no UUID connection_id, got %q", id)
		ids = append(ids, id)
	}
	require.Equal(t, ids[0], ids[1], "a reconnect reuses the id so Switch keeps the connection across the drop")
}

func TestStreamReconnectResumesWithLastEventIDAndMarksReplayed(t *testing.T) {
	f, c, log := streamTest(t)
	setStream(f, func(w http.ResponseWriter, _ *http.Request, n int) {
		if n == 1 {
			frame(w, 0, "connection_state", connectionState(connA, 1, testRoomUUID))
			frame(w, 5, "message", messageFrame(testRoomUUID, "msg-5", testHumanSender, "five", at(5).UnixMilli()))
			return // the body ends here: a dropped connection
		}
		frame(w, 0, "connection_state", connectionState(connB, 1, testRoomUUID))
		frame(w, 6, "message", messageFrame(testRoomUUID, "msg-6", testHumanSender, "six", at(6).UnixMilli()))
		frame(w, 0, "evicted", map[string]any{})
	})
	var got deliveryLog
	require.ErrorIs(t, c.Stream(streamCtx(t), []change.RoomID{roomID(t, testRoomUUID)}, nil, got.deliver), ErrEvicted)

	opens := streamOpens(f)
	require.Len(t, opens, 2)
	require.Empty(t, opens[0].LastEventID)
	require.Equal(t, int64(5), atoi(t, opens[1].LastEventID), "the reconnect resumes after the last sequence delivered")
	require.Equal(t, connA, opens[1].Connection, "the reconnect names the connection it replaces")
	require.Contains(t, log.all(), time.Second, "the first reconnect waits the base backoff through the injected wait")

	d := got.all()
	require.Len(t, d, 2)
	require.False(t, d[0].Replayed)
	require.Equal(t, int64(6), d[1].Sequence)
	require.True(t, d[1].Replayed, "a message on a resumed connection is marked replayed")
}

func TestStreamBacksOffDoublingWhileSwitchIsUnavailable(t *testing.T) {
	f, c, log := streamTest(t)
	setStream(f, func(w http.ResponseWriter, _ *http.Request, n int) {
		if n <= 2 {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "down"})
			return
		}
		frame(w, 0, "connection_state", withHeartbeat(connectionState(connA, 1, testRoomUUID), 7))
		frame(w, 0, "evicted", map[string]any{})
	})
	var got deliveryLog
	require.ErrorIs(t, c.Stream(streamCtx(t), []change.RoomID{roomID(t, testRoomUUID)}, nil, got.deliver), ErrEvicted)

	require.Len(t, streamOpens(f), 3)
	waits := log.all()
	require.GreaterOrEqual(t, len(waits), 2)
	require.Equal(t, time.Second, waits[0], "the first failure waits 1 s")
	require.Equal(t, 2*time.Second, waits[1], "the second failure waits 2 s")
}

func TestStreamGapStopsWithErrGapAndTheNextStartReadsTheMissedHistory(t *testing.T) {
	f, c, _ := streamTest(t)
	room := roomID(t, testRoomUUID)
	setHistory(f,
		fakeEntry{ID: "msg-3", Room: testRoomUUID, Sender: testHumanSender, Body: "three", TSms: at(3).UnixMilli()},
		fakeEntry{ID: "msg-4", Room: testRoomUUID, Sender: testHumanSender, Body: "four", TSms: at(4).UnixMilli()},
	)
	setStream(f, func(w http.ResponseWriter, _ *http.Request, _ int) {
		frame(w, 0, "connection_state", connectionState(connA, 1, testRoomUUID))
		frame(w, 3, "message", messageFrame(testRoomUUID, "msg-3", testHumanSender, "three", at(3).UnixMilli()))
		frame(w, 0, "gap", map[string]any{})
	})
	var first deliveryLog
	err := c.Stream(streamCtx(t), []change.RoomID{room}, nil, first.deliver)
	require.ErrorIs(t, err, ErrGap, "a gap is surfaced, not eaten")
	require.Len(t, streamOpens(f), 1, "a gap does not reconnect on its own")
	require.Equal(t, ports.Cursor("2026-11-04T12:00:03Z"), first.all()[0].Cursor)

	setStream(f, func(w http.ResponseWriter, _ *http.Request, _ int) {
		frame(w, 0, "connection_state", connectionState(connB, 1, testRoomUUID))
		frame(w, 0, "evicted", map[string]any{})
	})
	var second deliveryLog
	resume := map[change.RoomID]ports.Cursor{room: first.all()[0].Cursor}
	require.ErrorIs(t, c.Stream(streamCtx(t), []change.RoomID{room}, resume, second.deliver), ErrEvicted)

	reads := f.opCalls("read_context")
	require.Len(t, reads, 1, "the new start reads the history once from the stored cursor")
	require.Equal(t, "2026-11-04T12:00:03Z", reads[0]["since"])

	d := second.all()
	require.Len(t, d, 2)
	require.Equal(t, change.MessageID("msg-3"), d[0].Message.ID, "the message at the cursor comes back; callers dedupe")
	require.True(t, d[0].Replayed)
	require.Equal(t, int64(0), d[0].Sequence, "history read back has no SSE sequence")
	require.Equal(t, change.MessageID("msg-4"), d[1].Message.ID)
	require.Equal(t, ports.Cursor("2026-11-04T12:00:04Z"), d[1].Cursor)
}

func TestStreamEvictionEndsTheStreamWithoutReconnecting(t *testing.T) {
	f, c, log := streamTest(t)
	setStream(f, func(w http.ResponseWriter, _ *http.Request, _ int) {
		frame(w, 0, "evicted", map[string]any{})
	})
	var got deliveryLog
	err := c.Stream(streamCtx(t), []change.RoomID{roomID(t, testRoomUUID)}, nil, got.deliver)
	require.ErrorIs(t, err, ErrEvicted)
	require.Len(t, streamOpens(f), 1, "an evicted connection is not reopened")
	require.Empty(t, log.all(), "no backoff wait either")
}

func TestStreamRefusesAMalformedMessageWithoutReconnecting(t *testing.T) {
	f, c, _ := streamTest(t)
	setStream(f, func(w http.ResponseWriter, _ *http.Request, _ int) {
		frame(w, 0, "connection_state", connectionState(connA, 1, testRoomUUID))
		frame(w, 1, "message", messageFrame(testRoomUUID, "msg-bad", "", "no sender", at(1).UnixMilli()))
		frame(w, 0, "evicted", map[string]any{})
	})
	var got deliveryLog
	err := c.Stream(streamCtx(t), []change.RoomID{roomID(t, testRoomUUID)}, nil, got.deliver)
	require.Error(t, err)
	require.NotErrorIs(t, err, ports.ErrUnavailable, "retrying would deliver the same frame again")
	require.Len(t, streamOpens(f), 1)
	require.Empty(t, got.all())
}

func TestStreamDeliverErrorStopsTheStreamEvenWhenItWrapsUnavailable(t *testing.T) {
	f, c, _ := streamTest(t)
	setStream(f, func(w http.ResponseWriter, _ *http.Request, _ int) {
		frame(w, 0, "connection_state", connectionState(connA, 1, testRoomUUID))
		frame(w, 1, "message", messageFrame(testRoomUUID, "msg-1", testHumanSender, "one", at(1).UnixMilli()))
		frame(w, 0, "evicted", map[string]any{})
	})
	deliverErr := fmt.Errorf("%w: store down: %w", ports.ErrUnavailable, errTestStop)
	err := c.Stream(streamCtx(t), []change.RoomID{roomID(t, testRoomUUID)}, nil,
		func(context.Context, Delivery) error { return deliverErr })
	require.ErrorIs(t, err, deliverErr)
	require.ErrorIs(t, err, errTestStop)
	require.Len(t, streamOpens(f), 1, "the caller stopped the stream, so it is not retried")
}

func TestStreamUnauthorisedIsTerminal(t *testing.T) {
	f, c, log := streamTest(t)
	setStream(f, func(w http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"detail": "bad key"})
	})
	var got deliveryLog
	err := c.Stream(streamCtx(t), []change.RoomID{roomID(t, testRoomUUID)}, nil, got.deliver)
	require.ErrorIs(t, err, ports.ErrUnauthorized)
	require.Len(t, streamOpens(f), 1)
	require.Empty(t, log.all())
}

func TestStreamRefusesAResumeCursorThatIsNotRFC3339(t *testing.T) {
	f, c, _ := streamTest(t)
	var got deliveryLog
	resume := map[change.RoomID]ports.Cursor{roomID(t, testRoomUUID): "yesterday"}
	err := c.Stream(streamCtx(t), []change.RoomID{roomID(t, testRoomUUID)}, resume, got.deliver)
	require.Error(t, err)
	require.Empty(t, streamOpens(f), "a bad cursor is refused before any connection")
}

func TestStreamBeatsEchoTheGenerationAtTheHeartbeatInterval(t *testing.T) {
	f, c, log := streamTest(t)
	setStream(f, func(w http.ResponseWriter, r *http.Request, _ int) {
		frame(w, 0, "connection_state", withHeartbeat(connectionState(connA, 42, testRoomUUID), 7))
		awaitBeat(f, r)
		frame(w, 0, "evicted", map[string]any{})
	})
	var got deliveryLog
	require.ErrorIs(t, c.Stream(streamCtx(t), []change.RoomID{roomID(t, testRoomUUID)}, nil, got.deliver), ErrEvicted)

	var beats []recorded
	for _, r := range f.requests() {
		if r.Path == "/agents/"+testAgentID+"/connection/beat" {
			beats = append(beats, r)
		}
	}
	require.NotEmpty(t, beats, "at least one beat was posted before the eviction")
	for _, b := range beats {
		require.InDelta(t, float64(42), b.Body["generation"], 0, "a beat echoes the generation from connection_state")
		require.Equal(t, connA, b.Connection)
		require.Equal(t, testAPIKey, b.Auth)
	}
	waits := log.all()
	require.NotEmpty(t, waits)
	for _, d := range waits {
		require.Equal(t, 7*time.Second, d, "beats wait the interval connection_state names")
	}
}

func TestStreamCancellationReturnsCanceledAndStopsTheBeats(t *testing.T) {
	f, c, _ := streamTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	setStream(f, func(w http.ResponseWriter, r *http.Request, _ int) {
		frame(w, 0, "connection_state", connectionState(connA, 1, testRoomUUID))
		awaitBeat(f, r)
		cancel()
		<-r.Context().Done()
	})
	var got deliveryLog
	err := c.Stream(ctx, []change.RoomID{roomID(t, testRoomUUID)}, nil, got.deliver)
	require.ErrorIs(t, err, context.Canceled)
	require.Len(t, streamOpens(f), 1, "a cancelled caller does not reconnect")
}

func TestStreamFailedBeatEndsTheConnectionAndReconnectsNamingIt(t *testing.T) {
	f, c, _ := streamTest(t)
	f.mu.Lock()
	f.beatCode = http.StatusInternalServerError
	f.mu.Unlock()
	setStream(f, func(w http.ResponseWriter, r *http.Request, n int) {
		if n == 1 {
			frame(w, 0, "connection_state", connectionState(connA, 1, testRoomUUID))
			<-r.Context().Done() // the failed beat cancels the connection
			return
		}
		f.mu.Lock()
		f.beatCode = http.StatusOK
		f.mu.Unlock()
		frame(w, 0, "connection_state", connectionState(connB, 1, testRoomUUID))
		frame(w, 0, "evicted", map[string]any{})
	})
	var got deliveryLog
	require.ErrorIs(t, c.Stream(streamCtx(t), []change.RoomID{roomID(t, testRoomUUID)}, nil, got.deliver), ErrEvicted)

	opens := streamOpens(f)
	require.Len(t, opens, 2, "a beat that Switch refuses ends the connection, and the stream reconnects")
	require.Equal(t, connA, opens[1].Connection)
}
