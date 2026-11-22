package collaboration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/commands"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

var linkNow = time.Date(2026, 11, 22, 14, 30, 0, 0, time.UTC)

// linksFake keeps codes in memory the way the store does: one open code per sender, used once.
type linksFake struct {
	codes  map[string]ports.LinkCode
	used   map[string]bool
	linked map[change.SenderID]string
	issued int
}

func newLinksFake() *linksFake {
	return &linksFake{codes: map[string]ports.LinkCode{}, used: map[string]bool{}, linked: map[change.SenderID]string{}}
}

func (f *linksFake) OpenLinkCode(_ context.Context, sender change.SenderID, room change.RoomID, now, expires time.Time) (ports.LinkCode, error) {
	for code, c := range f.codes {
		if c.Sender == sender && !f.used[code] && c.Expires.After(now) {
			return c, nil
		}
	}
	f.issued++
	c := ports.LinkCode{Code: "K7Q2X" + string(rune('A'+f.issued)), Sender: sender, Room: room, Expires: expires}
	f.codes[c.Code] = c
	return c, nil
}

func (f *linksFake) RedeemLinkCode(_ context.Context, code, login string, now time.Time) (ports.LinkCode, error) {
	c, ok := f.codes[code]
	if !ok || f.used[code] || !c.Expires.After(now) {
		return ports.LinkCode{}, ports.ErrNoLinkCode
	}
	f.used[code] = true
	f.linked[c.Sender] = login
	return c, nil
}

func (f *linksFake) ChatLinks(context.Context) ([]ports.ChatLink, error) { return nil, nil }

// replyLog keeps every reply, so a test can read the GitHub answer and the chat confirmation apart.
type replyLog struct{ replies []loggedReply }

type loggedReply struct {
	target Target
	source commands.Source
	body   string
}

func (r *replyLog) Reply(_ context.Context, target Target, source commands.Source, _, body string) error {
	r.replies = append(r.replies, loggedReply{target, source, body})
	return nil
}

func linkProcessor(t *testing.T, people mappedPeople, links *linksFake, replies Replies) *Processor {
	t.Helper()
	p, err := New(Config{
		Identity: identityFake{people: people}, Executor: &executorFake{}, Replies: replies,
		Questioner: &stubQuestioner{answer: "ok"}, Clock: fixedClock{linkNow}, Links: links,
	})
	require.NoError(t, err)
	return p
}

// Live, Nov 22: a person typed `heimdall help` in Telegram from an account the policy does not list and got silence.
// Every way a person addresses Heimdall now answers an unknown sender with a code and where to use it (D194).
func TestAnUnknownChatSenderIsToldHowToLinkInsteadOfSilence(t *testing.T) {
	for _, text := range []string{"heimdall help", "heimdall status", "@heimdall-coord-main what is blocking?", "heimdall uphold F1"} {
		t.Run(text, func(t *testing.T) {
			links := newLinksFake()
			replies := &replyLog{}
			p := linkProcessor(t, mappedPeople{}, links, replies)
			require.NoError(t, p.Handle(context.Background(), Input{
				Command:   commands.Input{Source: commands.SourceChat, Sender: "@switch-telegram-x-prathamesh421:localhost", Text: text},
				Target:    Target{ChangeID: "KAN-23", Room: ports.RoomBinding{ChangeID: "KAN-23", RoomID: "room-tg"}},
				SourceRef: "msg-1",
			}))
			require.Len(t, replies.replies, 1)
			body := replies.replies[0].body
			require.Contains(t, body, "/heimdall link K7Q2XB", "the reply carries the code to comment on GitHub")
			require.Contains(t, body, "30 minutes")
			require.NotContains(t, body, "**")
			require.NotContains(t, body, "`")
			require.Equal(t, 1, links.issued)
		})
	}
}

func TestAnUnknownSenderAskingTwiceGetsTheSameCode(t *testing.T) {
	links := newLinksFake()
	replies := &replyLog{}
	p := linkProcessor(t, mappedPeople{}, links, replies)
	for _, ref := range []string{"msg-1", "msg-2"} {
		require.NoError(t, p.Handle(context.Background(), Input{
			Command:   commands.Input{Source: commands.SourceChat, Sender: "u-new", Text: "heimdall help"},
			Target:    Target{ChangeID: "KAN-23", Room: ports.RoomBinding{ChangeID: "KAN-23", RoomID: "room-tg"}},
			SourceRef: ref,
		}))
	}
	require.Equal(t, 1, links.issued)
	require.Equal(t, replies.replies[0].body, replies.replies[1].body)
}

func TestAStandingRoomAsksAnUnknownSenderToLink(t *testing.T) {
	links := newLinksFake()
	replies := &replyLog{}
	p := linkProcessor(t, mappedPeople{}, links, replies)
	candidates := []ports.RoomBinding{{ChangeID: "KAN-23", RoomID: "room-tg"}, {ChangeID: "KAN-24", RoomID: "room-tg"}}
	require.NoError(t, p.HandleSharedRoomMessage(context.Background(), "room-tg", candidates, ports.Message{
		ID: "msg-1", Room: "room-tg", Sender: "u-new", Text: "heimdall status KAN-24", At: linkNow,
	}))
	require.Len(t, replies.replies, 1)
	require.Contains(t, replies.replies[0].body, "/heimdall link ")
	require.Equal(t, change.RoomID("room-tg"), replies.replies[0].target.Room.RoomID)
}

func TestAnAgentIsNeverAskedToLink(t *testing.T) {
	links := newLinksFake()
	replies := &replyLog{}
	p, err := New(Config{
		Identity: identityFake{people: mappedPeople{}, agent: true}, Executor: &executorFake{}, Replies: replies,
		Questioner: &stubQuestioner{}, Clock: fixedClock{linkNow}, Links: links,
	})
	require.NoError(t, err)
	require.NoError(t, p.Handle(context.Background(), Input{
		Command:   commands.Input{Source: commands.SourceChat, Sender: "reviewer-1", Text: "heimdall help"},
		Target:    Target{ChangeID: "KAN-23", Room: ports.RoomBinding{RoomID: "room-tg"}},
		SourceRef: "msg-1",
	}))
	require.Empty(t, replies.replies)
	require.Zero(t, links.issued)
}

// The GitHub login that comments the code is who the chat account is: GitHub already proved the login.
func TestLinkCommentOnGitHubLinksTheChatAccountAndSaysSoInBothPlaces(t *testing.T) {
	links := newLinksFake()
	replies := &replyLog{}
	p := linkProcessor(t, mappedPeople{"github:prathameshk421": "approver"}, links, replies)
	code, err := links.OpenLinkCode(context.Background(), "u-tg", "room-tg", linkNow, linkNow.Add(30*time.Minute))
	require.NoError(t, err)

	require.NoError(t, p.Handle(context.Background(), Input{
		Command:   commands.Input{Source: commands.SourceGitHub, Sender: "prathameshk421", Text: "/heimdall link " + strings.ToLower(code.Code)},
		Target:    Target{ChangeID: "KAN-23", Repo: "o/r", PR: 18},
		SourceRef: "c-1",
	}))
	require.Equal(t, "prathameshk421", links.linked["u-tg"])
	require.Len(t, replies.replies, 2)
	gh, chat := replies.replies[0], replies.replies[1]
	require.Equal(t, commands.SourceGitHub, gh.source)
	require.Contains(t, gh.body, "approver")
	require.Equal(t, commands.SourceChat, chat.source)
	require.Equal(t, change.RoomID("room-tg"), chat.target.Room.RoomID, "the confirmation goes to the room the code was asked in")
	require.Contains(t, chat.body, "approver")
	require.Contains(t, chat.body, "prathameshk421")
}

func TestLinkByALoginOutsideThePolicySaysWhatIsStillMissing(t *testing.T) {
	links := newLinksFake()
	replies := &replyLog{}
	p := linkProcessor(t, mappedPeople{}, links, replies)
	code, err := links.OpenLinkCode(context.Background(), "u-tg", "room-tg", linkNow, linkNow.Add(30*time.Minute))
	require.NoError(t, err)
	require.NoError(t, p.Handle(context.Background(), Input{
		Command:   commands.Input{Source: commands.SourceGitHub, Sender: "stranger", Text: "/heimdall link " + code.Code},
		Target:    Target{ChangeID: "KAN-23", Repo: "o/r", PR: 18},
		SourceRef: "c-1",
	}))
	require.Len(t, replies.replies, 2)
	require.Contains(t, replies.replies[0].body, "not in this repository's people list")
	require.Contains(t, replies.replies[1].body, "not in this repository's people list")
}

func TestAnUnknownOrUsedLinkCodeIsRefusedOnGitHub(t *testing.T) {
	links := newLinksFake()
	replies := &replyLog{}
	p := linkProcessor(t, mappedPeople{}, links, replies)
	require.NoError(t, p.Handle(context.Background(), Input{
		Command:   commands.Input{Source: commands.SourceGitHub, Sender: "prathameshk421", Text: "/heimdall link ABCDEF"},
		Target:    Target{ChangeID: "KAN-23", Repo: "o/r", PR: 18},
		SourceRef: "c-1",
	}))
	require.Len(t, replies.replies, 1)
	require.Contains(t, replies.replies[0].body, "unknown, already used or expired")
	require.Empty(t, links.linked)
}

// A link proven in chat would prove nothing: the chat account is the thing being proven.
func TestLinkInChatPointsToGitHub(t *testing.T) {
	links := newLinksFake()
	replies := &replyLog{}
	p := linkProcessor(t, mappedPeople{}, links, replies)
	require.NoError(t, p.Handle(context.Background(), Input{
		Command:   commands.Input{Source: commands.SourceChat, Sender: "u-tg", Text: "heimdall link K7Q2XB"},
		Target:    Target{ChangeID: "KAN-23", Room: ports.RoomBinding{ChangeID: "KAN-23", RoomID: "room-tg"}},
		SourceRef: "msg-1",
	}))
	require.Len(t, replies.replies, 1)
	require.Contains(t, replies.replies[0].body, "GitHub")
	require.Empty(t, links.linked)
}
