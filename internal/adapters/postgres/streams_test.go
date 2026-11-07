//go:build component

package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// prEvent is a valid pull request event of change cid at version. payload is the JSON of a change.PROpened or
// change.PRClosed value.
func prEvent(t *testing.T, cid id.ChangeID, version int64, typ change.EventType, payload any) change.Event {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	return change.Event{
		ID:            newEventID(),
		ChangeID:      cid,
		Version:       version,
		Type:          typ,
		Actor:         change.Actor{Kind: change.ActorGitHub, ID: "dev-alice"},
		OccurredAt:    base.Add(time.Duration(version) * time.Second),
		CorrelationID: "corr-pr",
		Payload:       raw,
	}
}

func TestOpenPRStreamsListsOnlyPullRequestsStillOpen(t *testing.T) {
	ctx := context.Background()
	srv := startServer(t)
	db := srv.database(ctx, t)
	store := NewStore(db.pool)

	const repo change.Repo = "acme/pay"
	opened := func(n int) change.PROpened {
		return change.PROpened{Repo: repo, Number: n, HeadSHA: "1111111111111111111111111111111111111111", Author: "dev-alice"}
	}
	// PAY-1: opened and still open. Listed.
	_, err := store.Append(ctx, "PAY-1", 0, []change.Event{prEvent(t, "PAY-1", 1, change.EventPROpened, opened(1))}, nil)
	require.NoError(t, err)
	// PAY-2: opened, then merged while the server was down. Not listed.
	_, err = store.Append(ctx, "PAY-2", 0, []change.Event{prEvent(t, "PAY-2", 1, change.EventPROpened, opened(2))}, nil)
	require.NoError(t, err)
	_, err = store.Append(ctx, "PAY-2", 1, []change.Event{prEvent(t, "PAY-2", 2, change.EventPRClosed,
		change.PRClosed{Repo: repo, Number: 2, HeadSHA: "1111111111111111111111111111111111111111", Merged: true})}, nil)
	require.NoError(t, err)
	// PAY-3: a room only, no pull request. Not listed.
	_, err = store.Append(ctx, "PAY-3", 0, roomEvents("PAY-3", 0, "room-3"), nil)
	require.NoError(t, err)

	got, err := store.OpenPRStreams(ctx)
	require.NoError(t, err)
	require.Equal(t, []ports.OpenPRStream{{Change: "PAY-1", Repo: repo, Number: 1}}, got)

	// A restart is a new Store on the same database. It reads the same streams.
	again, err := NewStore(db.pool).OpenPRStreams(ctx)
	require.NoError(t, err)
	require.Equal(t, got, again)
}
