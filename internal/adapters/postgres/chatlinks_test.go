//go:build component

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// A link code is issued once per sender while open, redeemed once, and the link it proves is read back (D194).
func TestChatLinkCodeIsIssuedRedeemedOnceAndLinks(t *testing.T) {
	ctx := context.Background()
	srv := startServer(t)
	s := NewStore(srv.database(ctx, t).pool)
	t0 := time.Date(2026, 11, 22, 14, 0, 0, 0, time.UTC)
	sender := "@switch-telegram-x-prathamesh421:localhost"

	code, err := s.OpenLinkCode(ctx, changeSender(sender), "room-1", t0, t0.Add(30*time.Minute))
	require.NoError(t, err)
	require.Len(t, code.Code, linkCodeLength)
	again, err := s.OpenLinkCode(ctx, changeSender(sender), "room-2", t0.Add(time.Minute), t0.Add(31*time.Minute))
	require.NoError(t, err)
	require.Equal(t, code, again, "an open code is given again, not a new one")

	_, err = s.RedeemLinkCode(ctx, "ZZZZZZ", "prathameshk421", t0.Add(2*time.Minute))
	require.ErrorIs(t, err, ports.ErrNoLinkCode)

	used, err := s.RedeemLinkCode(ctx, " "+lower(code.Code)+" ", "prathameshk421", t0.Add(2*time.Minute))
	require.NoError(t, err)
	require.Equal(t, code.Sender, used.Sender)
	require.Equal(t, code.Room, used.Room)
	_, err = s.RedeemLinkCode(ctx, code.Code, "someone-else", t0.Add(3*time.Minute))
	require.ErrorIs(t, err, ports.ErrNoLinkCode, "a code is used once")

	links, err := s.ChatLinks(ctx)
	require.NoError(t, err)
	require.Equal(t, []ports.ChatLink{{Sender: changeSender(sender), GitHub: "prathameshk421", At: t0.Add(2 * time.Minute)}}, links)

	fresh, err := s.OpenLinkCode(ctx, changeSender(sender), "room-1", t0.Add(4*time.Minute), t0.Add(34*time.Minute))
	require.NoError(t, err)
	require.NotEqual(t, code.Code, fresh.Code, "a used code is never given again")
	_, err = s.RedeemLinkCode(ctx, fresh.Code, "prathameshk421", t0.Add(35*time.Minute))
	require.ErrorIs(t, err, ports.ErrNoLinkCode, "an expired code is refused")
}

func changeSender(s string) change.SenderID { return change.SenderID(s) }

func lower(s string) string { return strings.ToLower(s) }
