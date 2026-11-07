//go:build component

package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/postgres"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	testpg "github.com/prathameshk421/flip_the_switch_hackathon/internal/testkit/postgres"
)

// closedEvents returns the pr.closed events of cid. It does not use require, so it can run inside require.Eventually.
func closedEvents(store *postgres.Store, cid id.ChangeID) ([]change.Event, error) {
	evs, err := store.Load(context.Background(), cid)
	if err != nil {
		return nil, err
	}
	var out []change.Event
	for _, e := range evs {
		if e.Type == change.EventPRClosed {
			out = append(out, e)
		}
	}
	return out, nil
}

// TestBootReconcileRecordsPRMergedWhileDown runs the real server three times on one database. The first run opens PR 1
// and posts the gate. GitHub merges PR 1 while the server is off. The second run records pr.closed once, at boot. The
// third run adds no second pr.closed (TECH_SPEC 2.2, D56).
func TestBootReconcileRecordsPRMergedWhileDown(t *testing.T) {
	ctx := context.Background()
	dsn := testpg.Start(t)
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	require.NoError(t, postgres.Migrate(ctx, pool))

	store := postgres.NewStore(pool)
	seedPinnedChange(t, store)

	jira := newFakeJira(t, time.Now().Add(-time.Minute))
	sw := newFakeSwitch(t)
	gh := newFakeGateGitHub(t, []byte(gateTestPolicyYAML))
	cfgPath := writeGateAcceptanceConfig(t, writeAcceptanceSecrets(t))

	boot := func() func() {
		cfg, err := config.Load(cfgPath)
		require.NoError(t, err)
		d := deps{
			databaseURL: dsn,
			endpoints: endpoints{
				jiraBaseURL:     jira.srv.URL,
				githubAPI:       gh.srv.URL,
				switchBaseURL:   sw.srv.URL,
				switchAgentID:   acceptanceAgentID,
				switchAgentName: acceptanceAgentName,
				switchBridgeID:  acceptanceBridgeID,
			},
			httpClient: &http.Client{},
			logger:     slog.New(slog.NewTextHandler(os.Stderr, nil)),
			pollEvery:  gateTestPollTick,
			syncEvery:  gateTestPollTick,
			drainTick:  gateTestPollTick,
			openPool:   pgxpool.New,
			migrate:    postgres.Migrate,
		}
		a, err := build(ctx, cfg, d)
		require.NoError(t, err)
		stop := startLoops(ctx, a)
		return func() {
			stop()
			a.close()
		}
	}

	// Run 1: the server opens PR 1 and posts the gate check on the head.
	stop := boot()
	require.Eventually(t, func() bool {
		_, ok := gh.lastGateCheck()
		return ok
	}, gateTestWaitFor, gateTestPollTick, "run 1 must post the gate check")
	stop()

	// Down: GitHub merges PR 1 while the server is off.
	gh.setClosed(true)

	// Run 2: the boot pass records pr.closed for the merged PR.
	stop = boot()
	require.Eventually(t, func() bool {
		evs, err := closedEvents(store, gateTestChange)
		return err == nil && len(evs) == 1
	}, gateTestWaitFor, gateTestPollTick, "run 2 must record pr.closed once")
	stop()

	evs, err := closedEvents(store, gateTestChange)
	require.NoError(t, err)
	require.Len(t, evs, 1)
	var pl change.PRClosed
	require.NoError(t, json.Unmarshal(evs[0].Payload, &pl))
	require.True(t, pl.Merged)
	require.Equal(t, 1, pl.Number)

	// Run 3: a restart after the reconcile. It must not add a second pr.closed.
	pullsBefore := gh.pullsCount()
	stop = boot()
	require.Eventually(t, func() bool {
		return gh.pullsCount() > pullsBefore
	}, gateTestWaitFor, gateTestPollTick, "run 3 must sync")
	stop()

	evs, err = closedEvents(store, gateTestChange)
	require.NoError(t, err)
	require.Len(t, evs, 1, "a restart must not duplicate pr.closed")
}
