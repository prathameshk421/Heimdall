package effects_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/effects"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

type recordingChat struct {
	posts []string
}

func (c *recordingChat) OpenRoom(context.Context, ports.RoomRequest) (change.RoomID, error) {
	return "switch:r1", nil
}

func (c *recordingChat) Post(_ context.Context, room change.RoomID, _ change.EffectKey, text string) error {
	c.posts = append(c.posts, string(room)+":"+text)
	return nil
}

func (c *recordingChat) Read(context.Context, change.RoomID, ports.Cursor) ([]ports.Message, ports.Cursor, error) {
	return nil, "", nil
}

type recordingIngest struct {
	events []change.Event
}

func (r *recordingIngest) Ingest(_ context.Context, _ id.ChangeID, evs []change.Event) error {
	r.events = append(r.events, evs...)
	return nil
}

type fakeRoomCloser struct {
	closed []change.RoomID
	err    error
}

func (f *fakeRoomCloser) CloseRoom(_ context.Context, room change.RoomID) error {
	if f.err != nil {
		return f.err
	}
	f.closed = append(f.closed, room)
	return nil
}

type fakeTicketWriter struct {
	comments    []string
	transitions []string
	commentErr  error
	transErr    error
}

func (f *fakeTicketWriter) Comment(_ context.Context, ticket id.ChangeID, _ change.EffectKey, text string) error {
	if f.commentErr != nil {
		return f.commentErr
	}
	f.comments = append(f.comments, string(ticket)+":"+text)
	return nil
}

func (f *fakeTicketWriter) CreateTicket(_ context.Context, _ ports.TicketDraft) (id.ChangeID, error) {
	return "KAN-99", nil
}

func (f *fakeTicketWriter) Transition(_ context.Context, ticket id.ChangeID, _ change.EffectKey, status string) error {
	if f.transErr != nil {
		return f.transErr
	}
	f.transitions = append(f.transitions, string(ticket)+":"+status)
	return nil
}

func TestRoomPostExecutorPostsMessage(t *testing.T) {
	t.Parallel()
	chat := &recordingChat{}
	ex, err := effects.NewRoomPost(effects.RoomPostDeps{Chat: chat})
	require.NoError(t, err)

	raw, err := json.Marshal(change.RoomPostPayload{RoomID: "switch:r1", Text: "hello @bob"})
	require.NoError(t, err)
	fx := change.Effect{Key: "KAN-5/ask/signoff/abc", Kind: change.EffectRoomPost, Target: "switch:r1", Payload: raw}
	require.NoError(t, ex.Execute(context.Background(), fx))
	require.Equal(t, []string{"switch:r1:hello @bob"}, chat.posts)
}

func TestRoomCloseExecutorArchivesAndIngestsRoomClosed(t *testing.T) {
	t.Parallel()
	closer := &fakeRoomCloser{}
	in := &recordingIngest{}
	ex, err := effects.NewRoomClose(effects.RoomCloseDeps{Closer: closer, Ingest: in, IDs: &fakeIDs{}, Clock: fixedClock{}})
	require.NoError(t, err)

	raw, err := json.Marshal(change.RoomClosePayload{Change: "KAN-5", RoomID: "switch:r1"})
	require.NoError(t, err)
	fx := change.Effect{Key: "KAN-5/close/archive/r0", Kind: change.EffectRoomClose, Target: "switch:r1", Payload: raw}
	require.NoError(t, ex.Execute(context.Background(), fx))
	require.Equal(t, []change.RoomID{"switch:r1"}, closer.closed)
	require.Len(t, in.events, 1)
	require.Equal(t, change.EventRoomClosed, in.events[0].Type)
}

func TestTrackerWriteExecutorCommentsTransitionsAndIngests(t *testing.T) {
	t.Parallel()
	tw := &fakeTicketWriter{}
	in := &recordingIngest{}
	ex, err := effects.NewTrackerWrite(effects.TrackerWriteDeps{
		Writer: tw, Transitioner: tw, Ingest: in, IDs: &fakeIDs{}, Clock: fixedClock{},
	})
	require.NoError(t, err)

	raw, err := json.Marshal(change.TrackerWritePayload{
		Ticket: "KAN-5", Stage: change.TrackerStageMerge, Comment: "merged!", Transition: "Done",
	})
	require.NoError(t, err)
	fx := change.Effect{Key: "KAN-5/tracker_merge/abc/r0", Kind: change.EffectTrackerWrite, Target: "KAN-5", Payload: raw}
	require.NoError(t, ex.Execute(context.Background(), fx))
	require.Equal(t, []string{"KAN-5:merged!"}, tw.comments)
	require.Equal(t, []string{"KAN-5:Done"}, tw.transitions)
	require.Len(t, in.events, 1)
	require.Equal(t, change.EventTrackerUpdated, in.events[0].Type)
}
