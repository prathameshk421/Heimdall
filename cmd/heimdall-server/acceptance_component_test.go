//go:build component

package main

import (
	"context"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/postgres"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	testpg "github.com/prathameshk421/flip_the_switch_hackathon/internal/testkit/postgres"
)

const (
	acceptanceChange   = id.ChangeID("KAN-1")
	acceptanceCursor   = "jira:KAN"
	acceptanceTitle    = "Add refunds"
	acceptanceWaitFor  = 20 * time.Second
	acceptancePollTick = 10 * time.Millisecond
)

// scenario is the running fakes and the store the assertions read.
type scenario struct {
	t        *testing.T
	dsn      string
	cfgPath  string
	endpoint endpoints
	jira     *fakeJira
	sw       *fakeSwitch
	gh       *fakeGitHub
	store    *postgres.Store
}

// startSlice builds the slice on the scenario's database and starts its loops. The returned function stops the loops
// and then closes the pool, in that order.
func (s *scenario) startSlice() func() {
	s.t.Helper()
	ctx := context.Background()
	cfg, err := config.Load(s.cfgPath)
	require.NoError(s.t, err)
	d := deps{
		databaseURL: s.dsn,
		endpoints:   s.endpoint,
		httpClient:  &http.Client{},
		logger:      slog.New(slog.DiscardHandler),
		pollEvery:   20 * time.Millisecond,
		syncEvery:   20 * time.Millisecond,
		drainTick:   20 * time.Millisecond,
		openPool:    pgxpool.New,
		migrate:     postgres.Migrate,
	}
	a, err := build(ctx, cfg, d)
	require.NoError(s.t, err)
	stop := startLoops(ctx, a)
	return func() {
		stop()
		a.close()
	}
}

// eventCount counts the stored events of one type of the change.
func (s *scenario) eventCount(cid id.ChangeID, typ change.EventType) int {
	evs, err := s.store.Load(context.Background(), cid)
	if err != nil {
		return -1
	}
	n := 0
	for _, e := range evs {
		if e.Type == typ {
			n++
		}
	}
	return n
}

// stateOf replays the stored events of the change and returns its state.
func (s *scenario) stateOf(cid id.ChangeID) change.State {
	evs, err := s.store.Load(context.Background(), cid)
	require.NoError(s.t, err)
	st, err := change.Replay(evs)
	require.NoError(s.t, err)
	return st
}

func (s *scenario) cursor() string {
	v, err := s.store.GetCursor(context.Background(), acceptanceCursor)
	if err != nil {
		return ""
	}
	return v
}

// Slice 2 end to end: a KAN ticket moves to In Progress with a human assignee, and the server opens one room and
// makes one branch. The same database is then started again, and nothing is repeated. The app accounts and the
// unassigned ticket in the same search produce nothing.
func TestSliceOpensOneRoomAndOneBranchAcrossARestart(t *testing.T) {
	dsn := testpg.Start(t)
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	// The Jira search returns the move with a time inside the overlap window of the first poll.
	jira := newFakeJira(t, time.Now().Add(-time.Minute))
	sw := newFakeSwitch(t)
	gh := newFakeGitHub(t)
	s := &scenario{
		t:       t,
		dsn:     dsn,
		cfgPath: writeAcceptanceConfig(t, writeAcceptanceSecrets(t)),
		endpoint: endpoints{
			jiraBaseURL:     jira.srv.URL,
			githubAPI:       gh.srv.URL,
			switchBaseURL:   sw.srv.URL,
			switchAgentID:   acceptanceAgentID,
			switchAgentName: acceptanceAgentName,
			switchBridgeID:  acceptanceBridgeID,
		},
		jira:  jira,
		sw:    sw,
		gh:    gh,
		store: postgres.NewStore(pool),
	}

	stop := s.startSlice()
	require.Eventually(t, func() bool {
		return s.eventCount(acceptanceChange, change.EventRoomOpened) == 1 &&
			s.eventCount(acceptanceChange, change.EventBranchEnsured) == 1
	}, acceptanceWaitFor, acceptancePollTick, "the room and the branch must be recorded")

	require.Equal(t, 1, s.eventCount(acceptanceChange, change.EventChangeTriggered), "one change.triggered")
	require.Equal(t, change.StateRoomOpen, s.stateOf(acceptanceChange))
	require.Equal(t, []string{acceptanceTitle}, sw.createCallTitles(), "Switch saw one open-room call for KAN-1")
	require.Zero(t, sw.badAuth, "every Switch call carried the API key")
	refs := gh.refCalls()
	require.Len(t, refs, 1, "GitHub saw one create-ref call")
	require.Equal(t, "refs/heads/KAN-1-add-refunds", refs[0].Ref, "the branch is named with the ticket key")
	require.Equal(t, acceptanceBaseSHA, refs[0].SHA, "the branch starts at the default head")

	require.Zero(t, s.eventCount("KAN-2", change.EventChangeTriggered), "an app-assigned ticket starts nothing")
	require.Zero(t, s.eventCount("KAN-3", change.EventChangeTriggered), "an unassigned ticket starts nothing")
	require.Len(t, sw.createCallTitles(), 1, "no room for the app or unassigned ticket")
	require.Len(t, gh.refCalls(), 1, "no branch for the app or unassigned ticket")

	stop()
	savedBefore := s.cursor()
	require.NotEmpty(t, savedBefore, "the poll saved its cursor before the stop")
	searchesBefore := jira.searchCount()

	// Restart on the same database. The cursor moves only after the restarted poller handled its changes, so a moved
	// cursor means the repeat poll has finished.
	stop = s.startSlice()
	require.Eventually(t, func() bool {
		return s.cursor() != savedBefore && jira.searchCount() > searchesBefore
	}, acceptanceWaitFor, acceptancePollTick, "the restarted poller must poll and save its cursor")
	stop()

	require.Equal(t, 1, s.eventCount(acceptanceChange, change.EventChangeTriggered), "a restart does not trigger again")
	require.Equal(t, 1, s.eventCount(acceptanceChange, change.EventRoomOpened), "a restart does not open another room")
	require.Equal(t, 1, s.eventCount(acceptanceChange, change.EventBranchEnsured), "a restart does not make another branch")
	require.Equal(t, change.StateRoomOpen, s.stateOf(acceptanceChange))
	require.Equal(t, []string{acceptanceTitle}, sw.createCallTitles(), "still one open-room call after the restart")
	require.Len(t, gh.refCalls(), 1, "still one create-ref call after the restart")
}

func TestFileTrackerProductionWiringReplaysThroughDurableCursor(t *testing.T) {
	dsn := testpg.Start(t)
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	ticketsDir, criterion := writeFileAcceptanceTicket(t)
	sw := newFakeSwitch(t)
	gh := newFakeGitHub(t)
	s := &scenario{
		t: t, dsn: dsn, cfgPath: writeFileAcceptanceConfig(t, writeAcceptanceSecrets(t), ticketsDir),
		endpoint: endpoints{githubAPI: gh.srv.URL, switchBaseURL: sw.srv.URL, switchAgentID: acceptanceAgentID,
			switchAgentName: acceptanceAgentName, switchBridgeID: acceptanceBridgeID},
		sw: sw, gh: gh, store: postgres.NewStore(pool),
	}

	stop := s.startSlice()
	require.Eventually(t, func() bool {
		return s.eventCount(acceptanceChange, change.EventRoomOpened) == 1 &&
			s.eventCount(acceptanceChange, change.EventBranchEnsured) == 1
	}, acceptanceWaitFor, acceptancePollTick, "the registered file tracker must start the change")
	require.Equal(t, 1, s.eventCount(acceptanceChange, change.EventChangeTriggered))
	events, err := s.store.Load(context.Background(), acceptanceChange)
	require.NoError(t, err)
	var triggered change.Triggered
	for _, event := range events {
		if event.Type == change.EventChangeTriggered {
			decoded, decodeErr := change.Decode(event)
			require.NoError(t, decodeErr)
			triggered = decoded.(change.Triggered)
			require.Len(t, triggered.Criteria, 1)
			require.Equal(t, change.Actor{Kind: change.ActorSystem, ID: "file"}, event.Actor)
			require.Equal(t, []workflow.BlobRef{triggered.Criteria[0].Ref}, event.BlobRefs)
		}
	}
	require.Equal(t, change.TriggerFile, triggered.Source)
	require.Equal(t, criterion, triggered.Criteria[0].Text)
	require.Equal(t, "blob:sha256:"+triggered.Criteria[0].Hash, string(triggered.Criteria[0].Ref))
	cursor, err := s.store.GetCursor(context.Background(), "file:KAN")
	require.NoError(t, err)
	require.NotEmpty(t, cursor)
	stop()

	stop = s.startSlice()
	require.Eventually(t, func() bool {
		current, cursorErr := s.store.GetCursor(context.Background(), "file:KAN")
		return cursorErr == nil && current != cursor
	}, acceptanceWaitFor, acceptancePollTick, "the restarted file poller must read the overlap and advance its durable cursor")
	stop()
	require.Equal(t, 1, s.eventCount(acceptanceChange, change.EventChangeTriggered), "replay must not append another trigger")
	require.Equal(t, 1, s.eventCount(acceptanceChange, change.EventRoomOpened), "replay must not open another room")
	require.Equal(t, 1, s.eventCount(acceptanceChange, change.EventBranchEnsured), "replay must not create another branch")
	require.Len(t, sw.createCallTitles(), 1)
	require.Len(t, gh.refCalls(), 1)
}
