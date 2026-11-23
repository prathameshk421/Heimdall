package people

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A chat account that proved its GitHub login (D194) speaks for the person with that login, with that person's roles.
func TestLinkedSenderResolvesToThePersonWithThatLogin(t *testing.T) {
	t.Parallel()
	d := newDir(t)
	d.Link([]ChatLink{{Sender: "@switch-telegram-x-alice:localhost", GitHub: "ALICE"}})

	ident, err := d.Resolve(ChannelChat, "@switch-telegram-x-alice:localhost")
	require.NoError(t, err)
	require.Equal(t, "alice", ident.Name)
	require.Contains(t, ident.Roles, RoleApprover, "a linked account carries the person's policy roles")
	name, ok := d.HumanName(ChannelChat, "@switch-telegram-x-alice:localhost")
	require.True(t, ok)
	require.Equal(t, "alice", name)

	still, err := d.Resolve(ChannelChat, "alice")
	require.NoError(t, err)
	require.Equal(t, "alice", still.Name, "the policy's own chat id keeps working: one person, several accounts")
}

func TestLinkNeverMovesASenderThePolicyMaps(t *testing.T) {
	t.Parallel()
	d := newDir(t)
	d.Link([]ChatLink{{Sender: "dana", GitHub: "alice"}})
	ident, err := d.Resolve(ChannelChat, "dana")
	require.NoError(t, err)
	require.Equal(t, "dana", ident.Name, "the policy file wins over a link")
}

func TestLinkToALoginNoPersonHoldsMapsNobody(t *testing.T) {
	t.Parallel()
	d := newDir(t)
	d.Link([]ChatLink{{Sender: "@switch-telegram-x-mallory:localhost", GitHub: "mallory"}})
	_, err := d.Resolve(ChannelChat, "@switch-telegram-x-mallory:localhost")
	require.ErrorIs(t, err, ErrUnknownSender)
}

func TestLinkNeverTurnsAnAgentIntoAPerson(t *testing.T) {
	t.Parallel()
	d := newDir(t)
	d.Link([]ChatLink{{Sender: "bob-reviewer", GitHub: "alice"}})
	ident, err := d.Resolve(ChannelChat, "bob-reviewer")
	require.NoError(t, err)
	require.Equal(t, KindAgent, ident.Kind, "an agent name stays an agent")
}

func TestLinkSkipsAMalformedSender(t *testing.T) {
	t.Parallel()
	d := newDir(t)
	d.Link([]ChatLink{{Sender: "", GitHub: "alice"}, {Sender: "bad\nid", GitHub: "alice"}})
	_, err := d.Resolve(ChannelChat, "bad\nid")
	require.ErrorIs(t, err, ErrUnknownSender)
}
