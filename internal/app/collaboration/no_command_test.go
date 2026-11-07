package collaboration

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/commands"
)

// unavailableIdentity fails every lookup, as the policy reader does while GitHub is rate limited.
type unavailableIdentity struct{ calls int }

func (f *unavailableIdentity) People(context.Context, Target) (commands.People, error) {
	f.calls++
	return nil, errors.New("github circuit open")
}
func (f *unavailableIdentity) IsAgent(context.Context, Target, commands.Source, string) (bool, error) {
	f.calls++
	return false, errors.New("github circuit open")
}
func (f *unavailableIdentity) Authorize(context.Context, Target, commands.Source, string, commands.Command) (string, error) {
	f.calls++
	return "", errors.New("github circuit open")
}

// Room chatter that is not a command never reads the policy. Agents post many such messages, and one GitHub
// call per message used up the GitHub App rate limit on the live VM; a failed lookup then stopped the room stream.
func TestPlainChatNeverLooksUpIdentity(t *testing.T) {
	identity := &unavailableIdentity{}
	executor, replies := &executorFake{}, &replyFake{}
	proc := processor(t, identity, executor, replies)
	for _, text := range []string{"I am starting on KAN-9 now.", "heimdallish is not the prefix", "", "/heimdall uphold F1"} {
		err := proc.Handle(context.Background(), Input{
			Command:   commands.Input{Source: commands.SourceChat, Sender: "sender-1", Text: text},
			SourceRef: "chat:msg-1",
		})
		require.NoError(t, err, "text %q", text)
	}
	require.Zero(t, identity.calls, "no identity lookup for text that is not a command")
	require.Zero(t, executor.calls)
	require.Zero(t, replies.calls)
}

// A real command still needs the identity, so a failed lookup is still returned for the stream to retry.
func TestACommandStillFailsWhenIdentityIsUnavailable(t *testing.T) {
	identity := &unavailableIdentity{}
	proc := processor(t, identity, &executorFake{}, &replyFake{})
	err := proc.Handle(context.Background(), Input{
		Command:   commands.Input{Source: commands.SourceChat, Sender: "sender-1", Text: "heimdall uphold F1"},
		SourceRef: "chat:msg-2",
	})
	require.ErrorContains(t, err, "resolve command sender kind")
	require.Equal(t, 1, identity.calls)
}
