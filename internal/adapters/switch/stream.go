package switchchat

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// ErrEvicted says Switch took the connection over. Stream returns it and does not reconnect.
var ErrEvicted = errors.New("switch connection evicted")

// ErrTruncated says Switch no longer holds the history from the cursor. It is an escalation, not an outage: retrying
// the same read cannot succeed, so the stream does not retry it.
var ErrTruncated = errors.New("switch history truncated")

// ErrGap says Switch cannot replay from the last sequence this stream saw, for example after a Switch restart (V13).
// Stream returns it and does not reconnect. The caller calls Stream again with its stored cursors, and that call reads
// the missed history with read_context.
var ErrGap = errors.New("switch stream gap")

// Delivery is one message the stream hands to the caller. Sequence is the SSE id, or 0 for a message read back from
// read_context. Replayed is true for a message that came on a resumed connection, or from read_context, so the caller
// checks it by MessageID before it acts. Cursor is the newest message time seen in the room. The caller stores it and
// passes it back to Stream.
type Delivery struct {
	Message  ports.Message
	Sequence int64
	Replayed bool
	Cursor   ports.Cursor
}

// StreamRooms adapts Switch deliveries to the shared ports.ChatStream contract.
func (c *Client) StreamRooms(
	ctx context.Context,
	rooms []ports.RoomBinding,
	cursors map[change.RoomID]ports.Cursor,
	deliver func(context.Context, ports.ChatDelivery) error,
) error {
	if deliver == nil {
		return errors.New("switch stream: delivery handler is required")
	}
	roomIDs := make([]change.RoomID, 0, len(rooms))
	for _, room := range rooms {
		if _, err := change.ParseRoomID(string(room.RoomID)); err != nil {
			return fmt.Errorf("switch stream: invalid room binding: %w", err)
		}
		roomIDs = append(roomIDs, room.RoomID)
	}
	return c.Stream(ctx, roomIDs, cursors, func(ctx context.Context, d Delivery) error {
		return deliver(ctx, ports.ChatDelivery{Message: d.Message, Cursor: d.Cursor, Replayed: d.Replayed})
	})
}

// DeliverFunc receives each delivery. A returned error stops the stream.
type DeliverFunc func(ctx context.Context, d Delivery) error

const (
	reconnectBase = time.Second
	reconnectMax  = 30 * time.Second
	// maxFrame bounds one SSE frame. A longer frame is a fault, and a retry would fail the same way.
	maxFrame = 1 << 20
)

// Stream holds one connection for rooms and delivers messages until ctx ends, Switch evicts the connection, or Switch
// reports a gap. Each room's resume cursor is read back with read_context once, when the call starts. A dropped
// connection or an unavailable Switch reconnects after a backoff through the injected wait, and the reconnect sends
// Last-Event-ID with the last sequence this call delivered.
func (c *Client) Stream(ctx context.Context, rooms []change.RoomID, resume map[change.RoomID]ports.Cursor, deliver DeliverFunc) error {
	if deliver == nil {
		return errors.New("switch stream: deliver is required")
	}
	s := &streamRun{
		c: c, rooms: rooms, resume: resume, deliver: deliver,
		newest: map[change.RoomID]time.Time{}, stopBeats: func() {},
	}
	if err := s.parseResume(); err != nil {
		return err
	}
	for {
		err := s.connect(ctx)
		if ctx.Err() != nil {
			return fmt.Errorf("switch stream: %w", ctx.Err())
		}
		if !retryable(err) {
			return err
		}
		if werr := c.wait(ctx, s.backoff()); werr != nil {
			return fmt.Errorf("switch stream: %w", werr)
		}
	}
}

// streamRun is the state of one Stream call. It lives in memory; the caller keeps the cursors.
type streamRun struct {
	c       *Client
	rooms   []change.RoomID
	resume  map[change.RoomID]ports.Cursor
	deliver DeliverFunc

	newest     map[change.RoomID]time.Time // newest message time per room
	seq        int64                       // highest SSE id delivered by this call
	resumed    bool                        // the current connection was opened with Last-Event-ID
	reconciled bool                        // read_context has run for the resume cursors
	connID     string
	failures   int
	cancelConn context.CancelCauseFunc
	stopBeats  func()
}

// stopError marks an error returned by DeliverFunc. It ends the stream, even when the error wraps ports.ErrUnavailable.
type stopError struct{ err error }

func (e *stopError) Error() string { return e.err.Error() }

// Unwrap returns the error from the caller.
func (e *stopError) Unwrap() error { return e.err }

// retryable says a connection failure may reconnect: Switch is unavailable, and the caller did not stop the stream.
func retryable(err error) bool {
	var se *stopError
	return errors.Is(err, ports.ErrUnavailable) && !errors.As(err, &se)
}

// parseResume reads the resume cursors. A cursor that is not RFC 3339 is refused, not ignored.
func (s *streamRun) parseResume() error {
	for room, cur := range s.resume {
		if cur == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, string(cur))
		if err != nil {
			return fmt.Errorf("switch stream: resume cursor for room %s: %w", room, err)
		}
		s.newest[room] = t
	}
	return nil
}

// backoff returns the wait before the next reconnect: 1 s, doubling to 30 s. A connection that reaches
// connection_state resets it.
func (s *streamRun) backoff() time.Duration {
	d := reconnectBase << min(s.failures, 5)
	if d > reconnectMax {
		d = reconnectMax
	}
	s.failures++
	return d
}

// connect runs one connection from open to close and returns the error that ended it. The breaker guards the open
// only. The 10 s deadline does not apply, because the stream is long-lived.
func (s *streamRun) connect(ctx context.Context) error {
	c := s.c
	if err := c.breaker.allow(c.clock.Now()); err != nil {
		return fmt.Errorf("switch stream: %w", err)
	}
	connCtx, cancel := context.WithCancelCause(ctx)
	s.cancelConn = cancel
	defer func() {
		s.stopBeats()
		s.stopBeats = func() {}
		cancel(nil)
	}()
	resp, err := s.open(connCtx)
	if err != nil && ctx.Err() != nil {
		err = fmt.Errorf("switch stream: %w", ctx.Err())
	}
	c.breaker.record(c.clock.Now(), err)
	if err != nil {
		return err
	}
	defer closeBody(resp)
	err = s.read(connCtx, resp.Body)
	// A failed beat cancels the connection with its own error. That error is the reason the stream ended.
	if cause := context.Cause(connCtx); ctx.Err() == nil && cause != nil && !errors.Is(cause, context.Canceled) {
		return cause
	}
	return err
}

// open starts the event stream. A reconnect names the previous connection and the last sequence, so Switch can
// resume it.
func (s *streamRun) open(ctx context.Context) (*http.Response, error) {
	c := s.c
	bearer, err := c.key()
	if err != nil {
		return nil, err
	}
	q := url.Values{"filter": {"all"}}
	for _, r := range s.rooms {
		q.Add("room_id", string(r))
	}
	path := "/agents/" + c.agentID + "/events"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path+"?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("switch stream: build request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+bearer)
	if s.connID != "" {
		req.Header.Set("X-Switch-Connection-Id", s.connID)
	}
	if s.seq > 0 {
		req.Header.Set("Last-Event-ID", strconv.FormatInt(s.seq, 10))
		s.resumed = true
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: switch stream: connect: %w", ports.ErrUnavailable, err)
	}
	if err := statusError(path, resp.StatusCode); err != nil {
		closeBody(resp)
		return nil, err
	}
	return resp, nil
}

// sseFrame is one event being read. Each frame carries its own id, so a control frame has none.
type sseFrame struct {
	id    string
	event string
	data  []string
}

// read parses the SSE body frame by frame until it ends. A body that ends without an eviction or a gap is a dropped
// connection, which is unavailable and reconnects.
func (s *streamRun) read(ctx context.Context, body io.Reader) error {
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64<<10), maxFrame)
	var f sseFrame
	for sc.Scan() {
		line := sc.Text()
		if line != "" {
			name, value, _ := strings.Cut(line, ":")
			value = strings.TrimPrefix(value, " ")
			switch name {
			case "id":
				f.id = value
			case "event":
				f.event = value
			case "data":
				f.data = append(f.data, value)
			}
			continue
		}
		if f.event != "" || len(f.data) > 0 {
			if err := s.dispatch(ctx, f); err != nil {
				return err
			}
		}
		f = sseFrame{}
	}
	if err := sc.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return fmt.Errorf("switch stream: frame larger than %d bytes", maxFrame)
		}
		return fmt.Errorf("%w: switch stream: read: %w", ports.ErrUnavailable, err)
	}
	return fmt.Errorf("%w: switch stream: connection closed", ports.ErrUnavailable)
}

// dispatch acts on one frame. Unknown events are control frames the stream does not act on.
func (s *streamRun) dispatch(ctx context.Context, f sseFrame) error {
	switch f.event {
	case "connection_state":
		return s.connectionState(ctx, strings.Join(f.data, "\n"))
	case "message":
		return s.message(ctx, f)
	case "gap":
		return fmt.Errorf("%w: after sequence %d", ErrGap, s.seq)
	case "evicted":
		return ErrEvicted
	}
	return nil
}

// connectionState records the connection and its generation, starts the beats at the interval it names, and reads
// the resume cursors back once.
func (s *streamRun) connectionState(ctx context.Context, raw string) error {
	var cs struct {
		ConnectionID     string `json:"connection_id"`
		Generation       int64  `json:"generation"`
		HeartbeatSeconds int64  `json:"heartbeat_interval_seconds"`
	}
	if err := json.Unmarshal([]byte(raw), &cs); err != nil {
		return fmt.Errorf("switch stream: decode connection_state: %w", err)
	}
	if cs.ConnectionID == "" || cs.HeartbeatSeconds <= 0 {
		return errors.New("switch stream: connection_state has no connection id or heartbeat interval")
	}
	s.connID = cs.ConnectionID
	s.failures = 0
	s.startBeats(ctx, cs.ConnectionID, cs.Generation, time.Duration(cs.HeartbeatSeconds)*time.Second)
	if s.reconciled {
		return nil
	}
	if err := s.reconcile(ctx); err != nil {
		return err
	}
	s.reconciled = true
	return nil
}

// startBeats posts a beat every interval until the connection ends. A failed beat cancels the connection with its
// error. The goroutine has one owner, the streamRun, and stops when its context does.
func (s *streamRun) startBeats(ctx context.Context, connID string, gen int64, every time.Duration) {
	s.stopBeats()
	beatCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	s.stopBeats = func() {
		cancel()
		<-done
	}
	c := s.c
	end := s.cancelConn
	go func() {
		defer close(done)
		for {
			if err := c.wait(beatCtx, every); err != nil {
				return
			}
			if err := c.beat(beatCtx, connID, gen); err != nil {
				if beatCtx.Err() == nil {
					end(err)
				}
				return
			}
		}
	}()
}

// beat reports liveness for one connection generation. A refused beat is unavailable, so the stream reconnects.
func (c *Client) beat(ctx context.Context, connID string, gen int64) error {
	ctx, cancel := context.WithTimeout(ctx, requestDeadline)
	defer cancel()
	bearer, err := c.key()
	if err != nil {
		return err
	}
	raw, err := c.send(ctx, request{
		method: http.MethodPost,
		path:   "/agents/" + c.agentID + "/connection/beat",
		bearer: bearer,
		in:     map[string]any{"connection_id": connID, "generation": gen},
		header: map[string]string{"X-Switch-Connection-Id": connID},
	})
	if err != nil {
		return err
	}
	var reply struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return fmt.Errorf("switch beat: decode reply: %w", err)
	}
	if !reply.OK {
		return fmt.Errorf("%w: switch beat refused", ports.ErrUnavailable)
	}
	return nil
}

// message turns one message frame into a delivery. A frame that fails validation ends the stream: retrying would
// deliver the same frame again.
func (s *streamRun) message(ctx context.Context, f sseFrame) error {
	var env struct {
		RoomID  string `json:"room_id"`
		Payload struct {
			MessageID string `json:"message_id"`
			Sender    string `json:"sender"`
			Body      string `json:"body"`
			Timestamp *int64 `json:"timestamp"`
		} `json:"payload"`
	}
	if err := json.Unmarshal([]byte(strings.Join(f.data, "\n")), &env); err != nil {
		return fmt.Errorf("switch stream: decode message: %w", err)
	}
	if env.Payload.Timestamp == nil {
		return errors.New("switch stream: message has no timestamp")
	}
	msg := ports.Message{
		ID:     change.MessageID(env.Payload.MessageID),
		Room:   change.RoomID(env.RoomID),
		Sender: change.SenderID(env.Payload.Sender),
		Text:   env.Payload.Body,
		At:     time.UnixMilli(*env.Payload.Timestamp).UTC(),
	}
	if err := msg.Validate(); err != nil {
		return fmt.Errorf("switch stream: message refused: %w", err)
	}
	var seq int64
	if f.id != "" {
		n, err := strconv.ParseInt(f.id, 10, 64)
		if err != nil {
			return fmt.Errorf("switch stream: sequence %q: %w", f.id, err)
		}
		seq = n
	}
	at := s.latest(msg.Room, msg.At)
	d := Delivery{Message: msg, Sequence: seq, Replayed: s.resumed, Cursor: cursorOf(at)}
	if err := s.deliver(ctx, d); err != nil {
		return &stopError{err: err}
	}
	s.newest[msg.Room] = at
	if seq > s.seq {
		s.seq = seq
	}
	return nil
}

// reconcile reads each resumed room's history from its cursor with read_context. The message at the cursor comes back
// too, so the caller dedupes by MessageID.
func (s *streamRun) reconcile(ctx context.Context) error {
	for _, room := range s.rooms {
		cur := s.resume[room]
		if cur == "" {
			continue
		}
		msgs, _, err := s.c.Read(ctx, room, cur)
		if err != nil {
			return err
		}
		for _, m := range msgs {
			at := s.latest(room, m.At)
			d := Delivery{Message: m, Replayed: true, Cursor: cursorOf(at)}
			if err := s.deliver(ctx, d); err != nil {
				return &stopError{err: err}
			}
			s.newest[room] = at
		}
	}
	return nil
}

// latest returns the newer of the room's newest message time and t.
func (s *streamRun) latest(room change.RoomID, t time.Time) time.Time {
	if prev, ok := s.newest[room]; ok && prev.After(t) {
		return prev
	}
	return t
}
