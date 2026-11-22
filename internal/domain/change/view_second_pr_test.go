package change_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/stretchr/testify/require"
)

func TestSecondPROpenedAfterUnmergedCloseResumesChange(t *testing.T) {
	t.Parallel()
	base := id.SHA("1111111111111111111111111111111111111111")
	head1 := id.SHA("2222222222222222222222222222222222222222")
	head2 := id.SHA("3333333333333333333333333333333333333333")
	mkEvent := func(ver int64, typ change.EventType, payload any) change.Event {
		raw, err := json.Marshal(payload)
		require.NoError(t, err)
		return change.Event{
			ID:         change.EventID("01J0000000000000000000000" + string(rune('0'+ver))),
			ChangeID:   "KAN-8",
			Version:    ver,
			Type:       typ,
			Actor:      change.Actor{Kind: change.ActorGitHub, ID: "alice"},
			OccurredAt: time.Date(2026, 11, 22, 12, 0, int(ver), 0, time.UTC),
			Payload:    raw,
		}
	}

	// PR #1 opened, closed unmerged, then PR #2 opened -> resumed.
	events := []change.Event{
		mkEvent(1, change.EventPROpened, change.PROpened{Repo: "org/repo", Number: 1, HeadSHA: head1, BaseSHA: base, Author: "alice", Ticket: "KAN-8"}),
		mkEvent(2, change.EventPRClosed, change.PRClosed{Repo: "org/repo", Number: 1, HeadSHA: head1, Merged: false}),
		mkEvent(3, change.EventPROpened, change.PROpened{Repo: "org/repo", Number: 2, HeadSHA: head2, BaseSHA: base, Author: "alice", Ticket: "KAN-8"}),
	}
	v, err := change.Fold(events)
	require.NoError(t, err)
	require.False(t, v.Over(), "second PR after unmerged close must resume change")
	require.NotNil(t, v.PR)
	require.Equal(t, 2, v.PR.Number)
	require.Equal(t, head2, v.PR.HeadSHA)

	// Merged PR #1 stays over even if another PR event arrives.
	mergedEvents := []change.Event{
		mkEvent(1, change.EventPROpened, change.PROpened{Repo: "org/repo", Number: 1, HeadSHA: head1, BaseSHA: base, Author: "alice", Ticket: "KAN-8"}),
		mkEvent(2, change.EventPRClosed, change.PRClosed{Repo: "org/repo", Number: 1, HeadSHA: head1, Merged: true}),
		mkEvent(3, change.EventPROpened, change.PROpened{Repo: "org/repo", Number: 2, HeadSHA: head2, BaseSHA: base, Author: "alice", Ticket: "KAN-8"}),
	}
	mv, err := change.Fold(mergedEvents)
	require.NoError(t, err)
	require.True(t, mv.Over(), "merged change must stay over")
}
