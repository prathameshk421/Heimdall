package main

import (
	"context"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/commands"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/stretchr/testify/require"
)

type stubChatLinksRemover struct {
	removed map[string]int
	err     error
}

func (s *stubChatLinksRemover) OpenLinkCode(context.Context, change.SenderID, change.RoomID, time.Time, time.Time) (ports.LinkCode, error) {
	return ports.LinkCode{}, nil
}

func (s *stubChatLinksRemover) RedeemLinkCode(context.Context, string, string, time.Time) (ports.LinkCode, error) {
	return ports.LinkCode{}, nil
}

func (s *stubChatLinksRemover) ChatLinks(context.Context) ([]ports.ChatLink, error) {
	return nil, nil
}

func (s *stubChatLinksRemover) DeleteChatLinks(_ context.Context, target string) (int, error) {
	return s.removed[target], s.err
}

func TestExecuteDirectUnlinkRemovesLinksAndReportsCount(t *testing.T) {
	t.Parallel()
	r := &commandRuntime{links: &stubChatLinksRemover{removed: map[string]int{"prathameshk421": 2}}}

	reply, handled, err := r.executeDirect(context.Background(), "owner", commands.Unlink{Target: "prathameshk421"})
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, "Unlinked 2 chat account(s) for prathameshk421.", reply)

	reply2, handled2, err2 := r.executeDirect(context.Background(), "owner", commands.Unlink{Target: "nobody"})
	require.NoError(t, err2)
	require.True(t, handled2)
	require.Equal(t, "No linked chat accounts found for nobody.", reply2)
}
