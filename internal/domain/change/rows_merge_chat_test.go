package change_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// The room-close summary names a ticketless pull request by its own address, never the pr-... stream id.
func TestRoomCloseSummaryNamesATicketlessPullRequestReadably(t *testing.T) {
	const prc = id.ChangeID("pr-aarush323-heimdall-fixture-pay-15")
	closed := change.PRClosed{
		Repo:    "aarush323/heimdall-fixture-pay",
		Number:  15,
		HeadSHA: id.SHA("abcdef1234567890abcdef1234567890abcdef12"),
		Merged:  true,
	}
	v := change.View{
		Room: "switch:dev-gc",
		PR:   &change.PRInfo{Repo: "aarush323/heimdall-fixture-pay", Number: 15},
		Closed: &change.ClosedInfo{
			HeadSHA: closed.HeadSHA, Merged: true,
		},
	}
	in := change.In{Change: prc, View: v, Now: time.Now()}
	next := change.Next(in)
	var text string
	for _, a := range next {
		if a.Job != nil && a.Job.Kind == change.EffectRoomPost {
			p, ok := a.Job.Payload.(change.RoomPostPayload)
			require.True(t, ok)
			text = p.Text
		}
	}
	require.Contains(t, text, "PR aarush323/heimdall-fixture-pay #15 merged at abcdef1")
	require.NotContains(t, text, "pr-aarush323")
}
