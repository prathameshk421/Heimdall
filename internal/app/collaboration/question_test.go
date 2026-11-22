package collaboration

import (
	"context"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/commands"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/stretchr/testify/require"
)

type stubQuestioner struct {
	calls    int
	person   string
	question string
	answer   string
}

func (s *stubQuestioner) AnswerQuestion(_ context.Context, _ Target, person, question string) (string, error) {
	s.calls++
	s.person = person
	s.question = question
	return s.answer, nil
}

func TestMentionQuestionAnswersMappedPersonAndIgnoresUnmappedAndAgents(t *testing.T) {
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

	target := Target{
		ChangeID: "KAN-5",
		Room:     ports.RoomBinding{RoomID: "room-5"},
	}

	// Mapped human gets an answer.
	err = p.Handle(context.Background(), Input{
		Command:   commands.Input{Source: commands.SourceChat, Sender: "u-alice", Text: "@heimdall why is KAN-5 blocked?"},
		Target:    target,
		SourceRef: "msg-1",
	})
	require.NoError(t, err)
	require.Equal(t, 1, q.calls)
	require.Equal(t, "alice", q.person)
	require.Equal(t, "why is KAN-5 blocked?", q.question)
	require.Equal(t, 1, replies.calls)
	require.Equal(t, "KAN-5 has open PR #12.", replies.body)

	// Unmapped human is ignored.
	err = p.Handle(context.Background(), Input{
		Command:   commands.Input{Source: commands.SourceChat, Sender: "u-stranger", Text: "@heimdall why is KAN-5 blocked?"},
		Target:    target,
		SourceRef: "msg-2",
	})
	require.NoError(t, err)
	require.Equal(t, 1, q.calls)
	require.Equal(t, 1, replies.calls)

	// Agent is ignored.
	agentProc, err := New(Config{
		Identity:   identityFake{people: mappedPeople{"chat:u-alice": "alice"}, agent: true},
		Executor:   &executorFake{},
		Replies:    replies,
		Questioner: q,
	})
	require.NoError(t, err)
	err = agentProc.Handle(context.Background(), Input{
		Command:   commands.Input{Source: commands.SourceChat, Sender: "u-bot", Text: "@heimdall why is KAN-5 blocked?"},
		Target:    target,
		SourceRef: "msg-3",
	})
	require.NoError(t, err)
	require.Equal(t, 1, q.calls)
	require.Equal(t, 1, replies.calls)
}
