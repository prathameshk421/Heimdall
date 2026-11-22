package commands_test

import (
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/commands"
	"github.com/stretchr/testify/require"
)

type unlinkPeople struct{}

func (unlinkPeople) Lookup(commands.Source, string) (string, bool) { return "alice", true }

func TestParseUnlinkAcceptsGitHubLoginAndSwitchSenderID(t *testing.T) {
	t.Parallel()
	res := commands.Parse(commands.Input{Source: commands.SourceChat, Sender: "s1", Text: "heimdall unlink @prathameshk421"}, unlinkPeople{})
	require.Equal(t, commands.KindCommand, res.Kind)
	require.Equal(t, commands.Unlink{Target: "prathameshk421"}, res.Command)

	rawSender := "@switch-telegram-ff4ccbe3-prathamesh421:localhost"
	res2 := commands.Parse(commands.Input{Source: commands.SourceGitHub, Sender: "alice", Text: "/heimdall unlink " + rawSender}, unlinkPeople{})
	require.Equal(t, commands.KindCommand, res2.Kind)
	require.Equal(t, commands.Unlink{Target: rawSender}, res2.Command)

	bad := commands.Parse(commands.Input{Source: commands.SourceChat, Sender: "s1", Text: "heimdall unlink"}, unlinkPeople{})
	require.Equal(t, commands.KindBadArgs, bad.Kind)
	require.True(t, commands.RepositoryScoped("unlink"))
}
