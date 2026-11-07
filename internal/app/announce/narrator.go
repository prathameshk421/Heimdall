package announce

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// cursorSource names the narrator's place in the global event order.
const cursorSource = "announce:events"

// batchSize bounds how many events one pass reads.
const batchSize = 100

// Feed is a room that hears the stories of every change, besides the room of the change itself: the Telegram group,
// a Mattermost channel. Audience picks which stories it gets.
type Feed struct {
	Room     change.RoomID
	Audience Audience
}

// Config is what New needs.
type Config struct {
	// History reads every change's events in one global order.
	History ports.WorkflowHistory
	// Events loads one change's events, to give a line its context.
	Events  ports.EventStore
	Cursors ports.CursorTable
	Chat    ports.Chat
	Feeds   []Feed
	// ReviewRoom, when set, is the standing channel for the review story. Without it the review lines go to the room
	// of the change, like the test lines.
	ReviewRoom change.RoomID
	// TestsRoom is the standing channel for the test story, set together with ReviewRoom.
	TestsRoom change.RoomID
	// Tickets, when set, gives the first line about a ticket its title: "KAN-16 (Add refund reasons to the Pay API)".
	Tickets ports.TicketReader
	// Comments, when set, keeps one live comment on the change's pull request: the verdict, the checks and each finding.
	Comments ports.PRComments
	// Inline, when set, also puts each finding on its line in the pull request's Files tab.
	Inline InlineComments
	// Warn receives a failed comment write. The story in chat goes on, and the next event writes the comment again.
	Warn func(context.Context, error)
}

// InlineComments writes a comment on one line of a pull request's diff.
type InlineComments interface {
	InlineComment(ctx context.Context, repo change.Repo, n int, sha id.SHA, path string, line int, key change.EffectKey, body string) error
}

// Narrator posts one plain line per milestone of every change to the change's room and to the feeds.
type Narrator struct {
	cfg    Config
	titles map[id.ChangeID]string
}

// New builds the narrator, or refuses when a dependency is missing.
func New(cfg Config) (*Narrator, error) {
	var missing []string
	if cfg.History == nil {
		missing = append(missing, "History")
	}
	if cfg.Events == nil {
		missing = append(missing, "Events")
	}
	if cfg.Cursors == nil {
		missing = append(missing, "Cursors")
	}
	if cfg.Chat == nil {
		missing = append(missing, "Chat")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("announce: missing %v", missing)
	}
	for _, f := range cfg.Feeds {
		if f.Room == "" {
			return nil, errors.New("announce: a feed has no room")
		}
		switch f.Audience {
		case AudienceTests, AudienceReview, AudienceBoth:
		default:
			return nil, fmt.Errorf("announce: feed %s has audience %q, want tests, review or both", f.Room, f.Audience)
		}
	}
	return &Narrator{cfg: cfg, titles: map[id.ChangeID]string{}}, nil
}

// RunOnce reads the events after the cursor and posts a line for each one that has one. It returns how many events
// it announced. The first run only sets the cursor at the end of the log, so a deployment does not replay old changes
// into chat. A post that fails stops the run at that event, keeps the cursor before it, and the next run retries; a
// post that already went out is not repeated because its key is the same.
func (n *Narrator) RunOnce(ctx context.Context) (int, error) {
	after, found, err := n.cursor(ctx)
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, n.baseline(ctx)
	}
	events, last, err := n.cfg.History.EventsAfter(ctx, after, batchSize)
	if err != nil {
		return 0, fmt.Errorf("announce: read events after %d: %w", after, err)
	}
	announced := 0
	for _, ev := range events {
		said, err := n.announce(ctx, ev)
		if err != nil {
			return announced, err
		}
		if said {
			announced++
		}
	}
	if len(events) == 0 {
		return 0, nil
	}
	// The cursor moves once the whole batch is delivered. A failure part way leaves it where it was, and the retry
	// repeats the posts that already went out under the same keys, which the room drops.
	if err := n.cfg.Cursors.SetCursor(ctx, cursorSource, strconv.FormatInt(last, 10)); err != nil {
		return announced, fmt.Errorf("announce: save cursor: %w", err)
	}
	return announced, nil
}

func (n *Narrator) cursor(ctx context.Context) (int64, bool, error) {
	raw, err := n.cfg.Cursors.GetCursor(ctx, cursorSource)
	if errors.Is(err, ports.ErrNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("announce: read cursor: %w", err)
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("announce: cursor %q is not a position: %w", raw, err)
	}
	return v, true, nil
}

// baseline moves the cursor to the end of the log without posting.
func (n *Narrator) baseline(ctx context.Context) error {
	var pos int64
	for {
		events, last, err := n.cfg.History.EventsAfter(ctx, pos, batchSize)
		if err != nil {
			return fmt.Errorf("announce: read events after %d: %w", pos, err)
		}
		if len(events) == 0 {
			break
		}
		pos = last
	}
	if err := n.cfg.Cursors.SetCursor(ctx, cursorSource, strconv.FormatInt(pos, 10)); err != nil {
		return fmt.Errorf("announce: save baseline: %w", err)
	}
	return nil
}

func (n *Narrator) announce(ctx context.Context, ev change.Event) (bool, error) {
	all, err := n.cfg.Events.Load(ctx, ev.ChangeID)
	if err != nil {
		return false, fmt.Errorf("announce: load %s: %w", ev.ChangeID, err)
	}
	var before []change.Event
	for _, h := range all {
		if h.Version < ev.Version {
			before = append(before, h)
		}
	}
	n.updateBoard(ctx, ev, before)
	line, ok := Narrate(before, ev)
	if !ok {
		return false, nil
	}
	line.Text = n.withTitle(ctx, ev, line.Text)
	for _, room := range n.rooms(before, line.Audience) {
		key, err := change.IdempotencyKey(ev.ChangeID, 1, change.EffectRoomPost, "announce:"+string(ev.ID)+":"+string(room))
		if err != nil {
			return false, fmt.Errorf("announce: key for %s: %w", ev.ID, err)
		}
		if err := n.cfg.Chat.Post(ctx, room, key, line.Text); err != nil {
			return false, fmt.Errorf("announce: post %s to %s: %w", ev.Type, room, err)
		}
	}
	return true, nil
}

// updateBoard rewrites the pull request comment when ev changes what it shows, and puts a new finding on its line.
func (n *Narrator) updateBoard(ctx context.Context, ev change.Event, before []change.Event) {
	if n.cfg.Comments == nil || !boardEvent(ev.Type) {
		return
	}
	upTo := slices.Concat(before, []change.Event{ev})
	n.commentOnLine(ctx, ev, upTo)
	body, ok := RenderBoard(string(ev.ChangeID), upTo)
	if !ok {
		return
	}
	b := foldBoard(upTo)
	key, err := change.IdempotencyKey(ev.ChangeID, 1, change.EffectGitHubComment, "review-board")
	if err == nil {
		err = n.cfg.Comments.Comment(ctx, change.Repo(b.repo), b.number, key, body)
	}
	n.warn(ctx, err, "update the pull request comment", ev)
}

func (n *Narrator) commentOnLine(ctx context.Context, ev change.Event, upTo []change.Event) {
	if n.cfg.Inline == nil || ev.Type != change.EventFindingFiled {
		return
	}
	var p struct {
		Ref string `json:"ref"`
	}
	if !decode(ev, &p) {
		return
	}
	in, ok := RenderInline(upTo, p.Ref)
	if !ok {
		return
	}
	key, err := change.IdempotencyKey(ev.ChangeID, 1, change.EffectGitHubComment, "finding-line:"+p.Ref)
	if err == nil {
		err = n.cfg.Inline.InlineComment(ctx, change.Repo(in.Repo), in.Number, id.SHA(in.Head), in.Path, in.Line, key, in.Body)
	}
	n.warn(ctx, err, "comment on the finding's line", ev)
}

func (n *Narrator) warn(ctx context.Context, err error, what string, ev change.Event) {
	if err != nil && n.cfg.Warn != nil {
		n.cfg.Warn(ctx, fmt.Errorf("%s of %s: %w", what, ev.ChangeID, err))
	}
}

// rooms lists where a line goes: the room of the change when it exists, then each feed that wants the audience.
func (n *Narrator) rooms(history []change.Event, a Audience) []change.RoomID {
	var out []change.RoomID
	if n.cfg.TestsRoom != "" && n.cfg.ReviewRoom != "" {
		// Standing channels: the story is told there, not in the ticket's working room.
		if a == AudienceTests || a == AudienceBoth {
			out = append(out, n.cfg.TestsRoom)
		}
		if a == AudienceReview || a == AudienceBoth {
			out = append(out, n.cfg.ReviewRoom)
		}
	} else if room := roomOf(history); room != "" {
		out = append(out, room)
	}
	for _, f := range n.cfg.Feeds {
		if f.Audience == AudienceBoth || a == AudienceBoth || f.Audience == a {
			out = append(out, f.Room)
		}
	}
	return out
}

func roomOf(history []change.Event) change.RoomID {
	for i := len(history) - 1; i >= 0; i-- {
		var p struct {
			RoomID change.RoomID `json:"room_id"`
		}
		if history[i].Type == change.EventRoomOpened && decode(history[i], &p) {
			return p.RoomID
		}
	}
	return ""
}

// firstMention marks the events whose line is the first thing people read about a ticket or its pull request.
func firstMention(t change.EventType) bool {
	return t == change.EventTaskAssigned || t == change.EventPROpened || t == change.EventChangeMerged
}

// withTitle puts the ticket title after the first mention of its key. A title that cannot be read leaves the line as it
// is: the key alone is already clear.
func (n *Narrator) withTitle(ctx context.Context, ev change.Event, text string) string {
	if n.cfg.Tickets == nil || !firstMention(ev.Type) {
		return text
	}
	title, ok := n.titles[ev.ChangeID]
	if !ok {
		t, err := n.cfg.Tickets.Ticket(ctx, ev.ChangeID)
		if err != nil {
			return text
		}
		title = t.Title
		n.titles[ev.ChangeID] = title
	}
	if title == "" {
		return text
	}
	key := string(ev.ChangeID)
	return strings.Replace(text, key, key+" ("+title+")", 1)
}
