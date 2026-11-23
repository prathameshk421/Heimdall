package collaboration

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/commands"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// questionerFunc lets one test read the target a question was about.
type questionerFunc func(context.Context, Target, string, string) (string, error)

func (f questionerFunc) AnswerQuestion(ctx context.Context, target Target, person, question string) (string, error) {
	return f(ctx, target, person, question)
}

// A mention of the coordinator reaches the Questioner whatever the platform calls it. Live Telegram and Slack sent
// "@heimdall-coord-main", "@Heimdall" and "<@U0CFX056ZRA>", and none of them matched the old "@heimdall" literal.
func TestAnyCoordinatorMentionReachesTheQuestioner(t *testing.T) {
	t.Parallel()
	for _, text := range []string{
		"@heimdall status",
		"@heimdall-coord-main status",
		"@Heimdall what is blocking KAN-5?",
		"<@U0CFX056ZRA> status",
		"<@U0CFX056ZRA|heimdall> is the PR green?",
		"@heimdall-coord-main @U0CFX056ZRA status",
	} {
		t.Run(text, func(t *testing.T) {
			q := &stubQuestioner{answer: "KAN-5 has open PR #12."}
			replies := &replyFake{}
			p, err := New(Config{
				Identity:   identityFake{people: mappedPeople{"chat:u-alice": "alice"}},
				Executor:   &executorFake{},
				Replies:    replies,
				Questioner: q,
			})
			require.NoError(t, err)
			err = p.Handle(context.Background(), Input{
				Command:   commands.Input{Source: commands.SourceChat, Sender: "u-alice", Text: text},
				Target:    Target{ChangeID: "KAN-5", Room: ports.RoomBinding{RoomID: "room-5"}},
				SourceRef: "msg-1",
			})
			require.NoError(t, err)
			require.Equal(t, 1, q.calls, "the question reached the Questioner")
			require.Equal(t, 1, replies.calls)
		})
	}
}

// A bare mention says nothing, so it is not a question and gets no reply.
func TestABareMentionIsNotAQuestion(t *testing.T) {
	t.Parallel()
	q := &stubQuestioner{}
	replies := &replyFake{}
	p, err := New(Config{
		Identity:   identityFake{people: mappedPeople{"chat:u-alice": "alice"}},
		Executor:   &executorFake{},
		Replies:    replies,
		Questioner: q,
	})
	require.NoError(t, err)
	for _, text := range []string{"@heimdall", "@heimdall-coord-main", "@Heimdall "} {
		require.NoError(t, p.Handle(context.Background(), Input{
			Command:   commands.Input{Source: commands.SourceChat, Sender: "u-alice", Text: text},
			Target:    Target{ChangeID: "KAN-5"},
			SourceRef: "msg-1",
		}))
	}
	require.Zero(t, q.calls)
	require.Zero(t, replies.calls)
}

// `heimdall status` is read-only: it answers through the Questioner and never reaches the executor, so no action runs.
func TestHeimdallStatusAnswersWithoutExecutingACommand(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"heimdall status", "heimdall status KAN-5", "@heimdall-coord-main heimdall status?"} {
		t.Run(text, func(t *testing.T) {
			q := &stubQuestioner{answer: "KAN-5 has open PR #12."}
			executor, replies := &executorFake{}, &replyFake{}
			p, err := New(Config{
				Identity:   identityFake{people: mappedPeople{"chat:u-alice": "alice"}},
				Executor:   executor,
				Replies:    replies,
				Questioner: q,
			})
			require.NoError(t, err)
			err = p.Handle(context.Background(), Input{
				Command:   commands.Input{Source: commands.SourceChat, Sender: "u-alice", Text: text},
				Target:    Target{ChangeID: "KAN-5"},
				SourceRef: "msg-1",
			})
			require.NoError(t, err)
			require.Equal(t, 1, q.calls)
			require.Zero(t, executor.calls, "status runs no action")
			require.Equal(t, "KAN-5 has open PR #12.", replies.body)
		})
	}
}

// `heimdall help` returns the command grammar, and still shows nothing to an unmapped or agent sender.
func TestHeimdallHelpReturnsTheGrammar(t *testing.T) {
	t.Parallel()
	q := &stubQuestioner{}
	executor, replies := &executorFake{}, &replyFake{}
	p, err := New(Config{
		Identity:   identityFake{people: mappedPeople{"chat:u-alice": "alice"}},
		Executor:   executor,
		Replies:    replies,
		Questioner: q,
	})
	require.NoError(t, err)
	err = p.Handle(context.Background(), Input{
		Command:   commands.Input{Source: commands.SourceChat, Sender: "u-alice", Text: "heimdall help"},
		Target:    Target{ChangeID: "KAN-5"},
		SourceRef: "msg-1",
	})
	require.NoError(t, err)
	require.Contains(t, replies.body, "heimdall uphold")
	require.Zero(t, executor.calls)
	require.Zero(t, q.calls)

	agentReplies := &replyFake{}
	agentProc, err := New(Config{
		Identity:   identityFake{people: mappedPeople{"chat:u-bot": "bob"}, agent: true},
		Executor:   &executorFake{},
		Replies:    agentReplies,
		Questioner: q,
	})
	require.NoError(t, err)
	require.NoError(t, agentProc.Handle(context.Background(), Input{
		Command:   commands.Input{Source: commands.SourceChat, Sender: "u-bot", Text: "heimdall help"},
		Target:    Target{ChangeID: "KAN-5"},
		SourceRef: "msg-2",
	}))
	require.Zero(t, agentReplies.calls, "an agent never gets the grammar")
}

// State-changing verbs still go through Parse and Authorize, so status and help cannot widen who may act.
func TestStatusDoesNotWidenCommandAuth(t *testing.T) {
	t.Parallel()
	executor, replies := &executorFake{}, &replyFake{}
	p, err := New(Config{
		Identity:   identityFake{people: mappedPeople{"chat:u-alice": "alice"}, deny: errors.New("wrong role")},
		Executor:   executor,
		Replies:    replies,
		Questioner: &stubQuestioner{},
	})
	require.NoError(t, err)
	require.NoError(t, p.Handle(context.Background(), Input{
		Command:   commands.Input{Source: commands.SourceChat, Sender: "u-alice", Text: "heimdall uphold F1"},
		Target:    Target{ChangeID: "KAN-5"},
		SourceRef: "msg-1",
	}))
	require.Zero(t, executor.calls)
	require.Contains(t, replies.body, "not allowed")
}

// A standing room answers `heimdall status` for its one open change, and asks which one when several are open.
func TestSharedRoomStatusAnswersOneChangeAndAsksAboutSeveral(t *testing.T) {
	t.Parallel()
	q := &stubQuestioner{answer: "KAN-5 has open PR #12."}
	replies := &replyFake{}
	p, err := New(Config{
		Identity:   identityFake{people: mappedPeople{"chat:u-alice": "alice"}},
		Executor:   &executorFake{},
		Replies:    replies,
		Questioner: q,
	})
	require.NoError(t, err)

	one := []ports.RoomBinding{{ChangeID: id.ChangeID("KAN-5"), RoomID: "switch:dev-gc"}}
	require.NoError(t, p.HandleSharedRoomMessage(context.Background(), "switch:dev-gc", one, ports.Message{
		ID: "msg-1", Sender: "u-alice", Text: "heimdall status",
	}))
	require.Equal(t, 1, q.calls)
	require.Equal(t, "KAN-5 has open PR #12.", replies.body)

	several := []ports.RoomBinding{
		{ChangeID: id.ChangeID("KAN-5"), RoomID: "switch:dev-gc"},
		{ChangeID: id.ChangeID("pr-aarush323-heimdall-fixture-pay-15"), RoomID: "switch:dev-gc"},
	}
	require.NoError(t, p.HandleSharedRoomMessage(context.Background(), "switch:dev-gc", several, ports.Message{
		ID: "msg-2", Sender: "u-alice", Text: "@heimdall-coord-main status",
	}))
	require.Equal(t, 1, q.calls, "several open changes get a question, not a guess")
	require.Contains(t, replies.body, "Which one do you mean?")
	require.Contains(t, replies.body, "KAN-5")
	require.Contains(t, replies.body, "aarush323/heimdall-fixture-pay #15")
	require.NotContains(t, replies.body, "pr-aarush323")
}

// A standing room with no open change says so, in plain words.
func TestSharedRoomStatusSaysWhenNothingIsOpen(t *testing.T) {
	t.Parallel()
	replies := &replyFake{}
	p, err := New(Config{
		Identity:   identityFake{people: mappedPeople{"chat:u-alice": "alice"}},
		Executor:   &executorFake{},
		Replies:    replies,
		Questioner: &stubQuestioner{},
	})
	require.NoError(t, err)
	require.NoError(t, p.HandleSharedRoomMessage(context.Background(), "switch:dev-gc", nil, ports.Message{
		ID: "msg-1", Sender: "u-alice", Text: "heimdall status",
	}))
	require.Contains(t, replies.body, "no open pull requests or tickets")
}

// `heimdall help` in a standing room answers the grammar, and never asks which ticket the sender means.
func TestSharedRoomHelpReturnsTheGrammar(t *testing.T) {
	t.Parallel()
	candidates := []ports.RoomBinding{
		{ChangeID: id.ChangeID("KAN-5"), RoomID: "switch:dev-gc"},
		{ChangeID: id.ChangeID("PAY-2"), RoomID: "switch:dev-gc"},
	}
	replies := &replyFake{}
	p, err := New(Config{
		Identity:   identityFake{people: mappedPeople{"chat:u-alice": "alice"}},
		Executor:   &executorFake{},
		Replies:    replies,
		Questioner: &stubQuestioner{},
	})
	require.NoError(t, err)
	require.NoError(t, p.HandleSharedRoomMessage(context.Background(), "switch:dev-gc", candidates, ports.Message{
		ID: "msg-1", Sender: "u-alice", Text: "heimdall help",
	}))
	require.Contains(t, replies.body, "heimdall uphold")
	require.NotContains(t, replies.body, "Which one do you mean?")
}

// A real state-changing command in a standing room still needs its ticket key.
func TestSharedRoomStillAsksForATicketOnAStateChange(t *testing.T) {
	t.Parallel()
	candidates := []ports.RoomBinding{
		{ChangeID: id.ChangeID("KAN-5"), RoomID: "switch:dev-gc"},
		{ChangeID: id.ChangeID("PAY-2"), RoomID: "switch:dev-gc"},
	}
	executor, replies := &executorFake{}, &replyFake{}
	p, err := New(Config{
		Identity:   identityFake{people: mappedPeople{"chat:u-alice": "alice"}, person: "alice"},
		Executor:   executor,
		Replies:    replies,
		Questioner: &stubQuestioner{},
	})
	require.NoError(t, err)
	require.NoError(t, p.HandleSharedRoomMessage(context.Background(), "switch:dev-gc", candidates, ports.Message{
		ID: "msg-1", Sender: "u-alice", Text: "heimdall uphold F1",
	}))
	require.Zero(t, executor.calls)
	require.Contains(t, replies.body, "Which ticket is that about?")
}

// A status that names its ticket is answered for that ticket, even when the standing room serves several.
func TestSharedRoomStatusNamingATicketAnswersThatTicket(t *testing.T) {
	t.Parallel()
	var answered id.ChangeID
	q := questionerFunc(func(_ context.Context, target Target, _, _ string) (string, error) {
		answered = target.ChangeID
		return "PAY-2 is merged.", nil
	})
	replies := &replyFake{}
	p, err := New(Config{
		Identity:   identityFake{people: mappedPeople{"chat:u-alice": "alice"}},
		Executor:   &executorFake{},
		Replies:    replies,
		Questioner: q,
	})
	require.NoError(t, err)
	candidates := []ports.RoomBinding{
		{ChangeID: id.ChangeID("KAN-5"), RoomID: "switch:dev-gc"},
		{ChangeID: id.ChangeID("PAY-2"), RoomID: "switch:dev-gc"},
	}
	require.NoError(t, p.HandleSharedRoomMessage(context.Background(), "switch:dev-gc", candidates, ports.Message{
		ID: "msg-1", Sender: "u-alice", Text: "heimdall status PAY-2",
	}))
	require.Equal(t, id.ChangeID("PAY-2"), answered)
	require.Equal(t, "PAY-2 is merged.", replies.body)
}
