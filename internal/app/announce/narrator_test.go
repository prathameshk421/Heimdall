package announce_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/announce"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

type tail struct {
	events []change.Event // global order; seq is the index + 1
}

func (f *tail) EventsAfter(_ context.Context, after int64, limit int) ([]change.Event, int64, error) {
	var out []change.Event
	last := after
	for i := int(after); i < len(f.events) && len(out) < limit; i++ {
		out = append(out, f.events[i])
		last = int64(i + 1)
	}
	return out, last, nil
}

func (f *tail) Load(_ context.Context, cid id.ChangeID) ([]change.Event, error) {
	var out []change.Event
	for _, e := range f.events {
		if e.ChangeID == cid {
			out = append(out, e)
		}
	}
	return out, nil
}

func (*tail) Append(context.Context, id.ChangeID, int64, []change.Event, []change.Effect) (int64, error) {
	return 0, errors.New("not used")
}

type cursors struct{ m map[string]string }

func (c *cursors) GetCursor(_ context.Context, s string) (string, error) {
	v, ok := c.m[s]
	if !ok {
		return "", ports.ErrNotFound
	}
	return v, nil
}
func (c *cursors) SetCursor(_ context.Context, s, v string) error { c.m[s] = v; return nil }

type post struct {
	room change.RoomID
	text string
	key  change.EffectKey
}

// chat follows the Chat contract: a repeat of a key in a room is not posted twice.
type chat struct {
	posts []post
	fail  change.RoomID
	seen  map[string]bool
}

func (c *chat) OpenRoom(context.Context, ports.RoomRequest) (change.RoomID, error) { return "", nil }
func (c *chat) Post(_ context.Context, room change.RoomID, key change.EffectKey, text string) error {
	if room == c.fail {
		return errors.New("room is down")
	}
	id := string(room) + "|" + string(key)
	if c.seen[id] {
		return nil
	}
	if c.seen == nil {
		c.seen = map[string]bool{}
	}
	c.seen[id] = true
	c.posts = append(c.posts, post{room, text, key})
	return nil
}
func (c *chat) Read(context.Context, change.RoomID, ports.Cursor) ([]ports.Message, ports.Cursor, error) {
	return nil, "", nil
}

func newNarrator(t *testing.T, log *tail, cur *cursors, c *chat) *announce.Narrator {
	t.Helper()
	n, err := announce.New(announce.Config{
		History: log, Events: log, Cursors: cur, Chat: c,
		Feeds: []announce.Feed{
			{Room: "tg-dev", Audience: announce.AudienceBoth},
			{Room: "mm-tests", Audience: announce.AudienceTests},
			{Room: "mm-review", Audience: announce.AudienceReview},
		},
	})
	require.NoError(t, err)
	return n
}

func roomOpened(t *testing.T) change.Event {
	return ev(t, change.EventRoomOpened, change.RoomOpened{RoomID: "dc-kan14"})
}

// A line goes to the ticket room and to the feeds of its audience. The Telegram group takes both stories.
func TestNarratorPostsToTheTicketRoomAndTheMatchingFeeds(t *testing.T) {
	log := &tail{events: []change.Event{roomOpened(t)}}
	cur := &cursors{m: map[string]string{}}
	c := &chat{}
	n := newNarrator(t, log, cur, c)

	_, err := n.RunOnce(context.Background()) // first run only sets the baseline: history is not replayed into chat
	require.NoError(t, err)
	require.Empty(t, c.posts)

	log.events = append(log.events,
		ev(t, change.EventTestsVerified, struct{}{}),
		ev(t, change.EventGateEvaluated, change.GateEvaluated{OK: true}),
	)
	for i := range log.events {
		log.events[i].Version = int64(i + 1)
	}
	posted, err := n.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, posted)

	rooms := map[change.RoomID][]string{}
	for _, p := range c.posts {
		rooms[p.room] = append(rooms[p.room], p.text)
	}
	require.Len(t, rooms["dc-kan14"], 2, "the ticket room hears both stories")
	require.Len(t, rooms["tg-dev"], 2, "the Telegram group hears both stories")
	require.Len(t, rooms["mm-tests"], 1)
	require.Contains(t, rooms["mm-tests"][0], "tests hold up")
	require.Len(t, rooms["mm-review"], 1)
	require.Contains(t, rooms["mm-review"][0], "Ready to merge")

	// A second run has nothing new and posts nothing again.
	posted, err = n.RunOnce(context.Background())
	require.NoError(t, err)
	require.Zero(t, posted)
}

// A room that is down stops the run at that event, keeps the cursor, and the next run delivers it; a post that
// already went out is not sent twice because the key is the same.
func TestNarratorRetriesAnEventWhoseRoomWasDown(t *testing.T) {
	log := &tail{events: []change.Event{roomOpened(t)}}
	cur := &cursors{m: map[string]string{}}
	c := &chat{}
	n := newNarrator(t, log, cur, c)
	_, err := n.RunOnce(context.Background())
	require.NoError(t, err)

	log.events = append(log.events, ev(t, change.EventTestsVerified, struct{}{}))
	for i := range log.events {
		log.events[i].Version = int64(i + 1)
	}
	c.fail = "mm-tests"
	_, err = n.RunOnce(context.Background())
	require.Error(t, err)

	c.fail = ""
	posted, err := n.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, posted)
	keys := map[change.EffectKey]int{}
	for _, p := range c.posts {
		keys[p.key]++
	}
	for k, n := range keys {
		require.Equal(t, 1, n, "post key %s must be used once per room delivery", k)
	}
}

// Before the room exists the ticket room is skipped, and the feeds still hear the line.
func TestNarratorSkipsTheTicketRoomUntilItExists(t *testing.T) {
	log := &tail{}
	cur := &cursors{m: map[string]string{}}
	c := &chat{}
	n := newNarrator(t, log, cur, c)
	_, err := n.RunOnce(context.Background())
	require.NoError(t, err)

	log.events = append(log.events, ev(t, change.EventTestsVerified, struct{}{}))
	log.events[0].Version = 1
	_, err = n.RunOnce(context.Background())
	require.NoError(t, err)
	for _, p := range c.posts {
		require.NotEqual(t, change.RoomID("dc-kan14"), p.room)
	}
	require.NotEmpty(t, c.posts)
}
