package collaboration

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/commands"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

type identityFake struct {
	people commands.People
	agent  bool
	person string
	deny   error
}

func (f identityFake) People(context.Context, Target) (commands.People, error) { return f.people, nil }
func (f identityFake) IsAgent(context.Context, Target, commands.Source, string) (bool, error) {
	return f.agent, nil
}
func (f identityFake) Authorize(_ context.Context, _ Target, _ commands.Source, _ string, _ commands.Command) (string, error) {
	return f.person, f.deny
}

type mappedPeople map[string]string

func (f mappedPeople) Lookup(src commands.Source, sender string) (string, bool) {
	name, ok := f[string(src)+":"+sender]
	return name, ok
}

type executorFake struct {
	person  string
	source  commands.Source
	command commands.Command
	calls   int
	err     error
}

func (f *executorFake) Execute(_ context.Context, _ Target, person string, source commands.Source, _ string, command commands.Command) (string, error) {
	f.person, f.source, f.command, f.calls = person, source, command, f.calls+1
	return "Done.", f.err
}

type replyFake struct {
	source    commands.Source
	sourceRef string
	body      string
	calls     int
}

func (f *replyFake) Reply(_ context.Context, _ Target, source commands.Source, sourceRef, body string) error {
	f.source, f.sourceRef, f.body, f.calls = source, sourceRef, body, f.calls+1
	return nil
}

func processor(t *testing.T, identity Identity, executor *executorFake, replies *replyFake) *Processor {
	t.Helper()
	p, err := New(Config{Identity: identity, Executor: executor, Replies: replies})
	require.NoError(t, err)
	return p
}

func TestHandleAuthorizesAndExecutesMappedRoomCommand(t *testing.T) {
	identity := identityFake{people: mappedPeople{"chat:sender-1": "ana"}, person: "ana"}
	executor, replies := &executorFake{}, &replyFake{}
	proc := processor(t, identity, executor, replies)
	in := Input{
		Command:   commands.Input{Text: "heimdall remember refunds are capped at 500", Source: commands.SourceChat, Sender: "sender-1"},
		Target:    Target{Room: ports.RoomBinding{ChangeID: id.ChangeID("PAY-1"), RoomID: change.RoomID("room-1")}},
		SourceRef: "message-1",
	}
	require.NoError(t, proc.Handle(context.Background(), in))
	require.Equal(t, 1, executor.calls)
	require.Equal(t, "ana", executor.person)
	require.Equal(t, commands.Remember{Text: "refunds are capped at 500"}, executor.command)
	require.Equal(t, "Done.", replies.body)
	require.Equal(t, "message-1", replies.sourceRef)
}

func TestHandleRefusesUnauthorizedCommandWithoutExecuting(t *testing.T) {
	executor, replies := &executorFake{}, &replyFake{}
	proc := processor(t, identityFake{
		people: mappedPeople{"chat:sender-1": "ana"}, deny: errors.New("wrong role"),
	}, executor, replies)
	err := proc.Handle(context.Background(), Input{
		Command:   commands.Input{Text: "heimdall uphold F3", Source: commands.SourceChat, Sender: "sender-1"},
		SourceRef: "message-2",
	})
	require.NoError(t, err)
	require.Zero(t, executor.calls)
	require.Contains(t, replies.body, "not authorized")
}

func TestHandleIgnoresAgentsBotsEditedAndUnaddressedPosts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		in    Input
		agent bool
	}{
		{name: "agent", in: Input{Command: commands.Input{Text: "heimdall uphold F1", Source: commands.SourceChat, Sender: "reviewer-agent"}, SourceRef: "m1"}, agent: true},
		{name: "bot comment", in: Input{Command: commands.Input{Text: "/heimdall uphold F1", Source: commands.SourceGitHub, Sender: "heimdall[bot]"}, SourceRef: "m2", Bot: true}},
		{name: "edited comment", in: Input{Command: commands.Input{Text: "/heimdall uphold F1", Source: commands.SourceGitHub, Sender: "ana"}, SourceRef: "m3", Edited: true}},
		{name: "ordinary text", in: Input{Command: commands.Input{Text: "please uphold F1", Source: commands.SourceChat, Sender: "sender-1"}, SourceRef: "m4"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			executor, replies := &executorFake{}, &replyFake{}
			proc := processor(t, identityFake{people: mappedPeople{"chat:sender-1": "ana", "github:ana": "ana"}, agent: tc.agent, person: "ana"}, executor, replies)
			require.NoError(t, proc.Handle(context.Background(), tc.in))
			require.Zero(t, executor.calls)
			require.Zero(t, replies.calls)
		})
	}
}

func TestHandleRepliesWithSourceGrammarForUnknownVerb(t *testing.T) {
	executor, replies := &executorFake{}, &replyFake{}
	proc := processor(t, identityFake{people: mappedPeople{"github:ana": "ana"}}, executor, replies)
	require.NoError(t, proc.Handle(context.Background(), Input{
		Command: commands.Input{Text: "/heimdall nope", Source: commands.SourceGitHub, Sender: "ana"}, SourceRef: "comment-4",
	}))
	require.Zero(t, executor.calls)
	require.Equal(t, commands.SourceGitHub, replies.source)
	require.Contains(t, replies.body, "/heimdall uphold <finding>")
}
