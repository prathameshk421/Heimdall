//go:build component

package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/postgres"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/ids"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	pgtest "github.com/prathameshk421/flip_the_switch_hackathon/internal/testkit/postgres"
)

func TestMappedRoomCommandPersistsAndRepliesThroughPostgres(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, pgtest.Start(t))
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	require.NoError(t, postgres.Migrate(ctx, pool))
	store := postgres.NewStore(pool)
	cid := id.ChangeID("KAN-991")
	eng := newSliceEngine(t, store)
	first := triggeredEvent(t, string(cid))
	first.ChangeID = cid
	first.Payload, err = json.Marshal(change.Triggered{Ticket: cid, Repo: "aarush323/PBL_TOS_RISK_ANALYZER", Assignee: "jira-assignee", Source: change.TriggerJira})
	require.NoError(t, err)
	require.NoError(t, first.Validate())
	_, err = eng.Start(ctx, first)
	require.NoError(t, err)

	policyBytes, err := os.ReadFile("../../internal/domain/policy/testdata/policy.golden.yaml")
	require.NoError(t, err)
	chat := &commandTestChat{}
	comments := &commandTestComments{}
	ad := adapters{tickets: commandTestTickets{}, vcs: commandTestVCS{policy: policyBytes}, chat: chat,
		projects: projectRepos{"KAN": "aarush323/PBL_TOS_RISK_ANALYZER"}}
	runtime, err := newCommandRuntime(store, ad, eng, ids.New(), clock.System{}, nil, nil)
	require.NoError(t, err)
	runtime.comments = comments
	room := ports.RoomBinding{ChangeID: cid, RoomID: "room-component"}
	message := ports.Message{ID: "component-message-1", Room: room.RoomID, Sender: "bob", Text: "heimdall remember refunds are capped at 500", At: time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)}
	require.NoError(t, runtime.processor.HandleRoomMessage(ctx, room, message))
	events, err := store.Load(ctx, cid)
	require.NoError(t, err)
	decision := findDecision(t, events, string(message.ID))
	require.Equal(t, change.DecisionRemember, decision.Kind)
	require.Equal(t, "refunds are capped at 500", decision.Reason)
	require.Equal(t, []string{"I recorded that for this change."}, chat.posts)
}
