//go:build component

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/postgres"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/agenttools"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/ids"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	testpg "github.com/prathameshk421/flip_the_switch_hackathon/internal/testkit/postgres"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport/mcp"
)

type fakeComponentTaskIndex struct {
	ref ports.ActiveTaskRef
}

func (f *fakeComponentTaskIndex) ActiveTask(_ context.Context, agent id.AgentName, role workflow.Role) (ports.ActiveTaskRef, error) {
	if agent == "test-author" && role == workflow.RoleTestAuthor {
		return f.ref, nil
	}
	return ports.ActiveTaskRef{}, ports.ErrNotFound
}

func TestMCPGetTaskWithRealAgenttoolsAndFakeTaskIndex(t *testing.T) {
	ctx := context.Background()
	dsn := testpg.Start(t)
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	require.NoError(t, postgres.Migrate(ctx, pool))
	store := postgres.NewStore(pool)

	cid := id.ChangeID("KAN-1")
	tid := id.TaskID("task-test-author")
	now := time.Now().UTC()
	gen := ids.New()
	clk := clock.System{}

	// Seed triggered and task.assigned in the real Postgres event store
	trigPayload, err := json.Marshal(change.Triggered{
		Ticket: cid,
		Repo:   change.Repo("aarush323/PBL_TOS_RISK_ANALYZER"),
	})
	require.NoError(t, err)

	trigEv := change.Event{
		ID:         gen.NewEvent(),
		ChangeID:   cid,
		Version:    1,
		Type:       change.EventChangeTriggered,
		Actor:      change.Actor{Kind: change.ActorTracker, ID: "jira"},
		OccurredAt: now.Add(-time.Minute),
		Payload:    trigPayload,
	}

	taskPayload, err := json.Marshal(change.TaskAssigned{
		TaskID:    tid,
		Agent:     id.AgentName("test-author"),
		Role:      workflow.RoleTestAuthor,
		Deadline:  now.Add(time.Hour),
		BundleRef: "blob:sha256:0000000000000000000000000000000000000000000000000000000000000000",
	})
	require.NoError(t, err)

	assignEv := change.Event{
		ID:         gen.NewEvent(),
		ChangeID:   cid,
		Version:    2,
		Type:       change.EventTaskAssigned,
		Actor:      change.Actor{Kind: change.ActorSystem, ID: "heimdall"},
		OccurredAt: now.Add(-30 * time.Second),
		Payload:    taskPayload,
	}

	_, err = store.Append(ctx, cid, 0, []change.Event{trigEv, assignEv}, nil)
	require.NoError(t, err)

	// Real agenttools Service with FAKE TaskIndex
	fakeIdx := &fakeComponentTaskIndex{ref: ports.ActiveTaskRef{ChangeID: cid, TaskID: tid}}
	blobs := newMemBlobStore()
	asker := &chatAsker{chat: dummyChat{}, store: store, ids: gen}

	svc, err := agenttools.New(agenttools.Config{
		Events: store,
		Index:  fakeIdx,
		Blobs:  blobs,
		Clock:  clk,
		IDs:    gen,
		Asker:  asker,
	})
	require.NoError(t, err)

	tracker := &assignmentAgentTracker{agents: make(map[id.TaskID]id.AgentName)}
	tasks := &tasksAdapter{svc: svc, tracker: tracker}
	reviews := &mcpReviewsAdapter{svc: svc, tracker: tracker}

	// Token store with test-author and reviewer-1
	authorToken := "token-author-secret"
	reviewerToken := "token-reviewer-secret"
	tokStore := &tokenStore{
		principals: map[string]mcp.Principal{
			mcp.HashToken(authorToken): {
				Agent: id.AgentName("test-author"),
				Role:  workflow.RoleTestAuthor,
			},
			mcp.HashToken(reviewerToken): {
				Agent: id.AgentName("reviewer-1"),
				Role:  workflow.RoleReviewer,
			},
		},
	}

	mcpSrv, err := mcp.NewServer(mcp.Deps{
		Logger:  discardLogger(),
		Clock:   clk,
		Tokens:  tokStore,
		Tasks:   tasks,
		Reviews: reviews,
		Code:    dummyVCS{},
		Blobs:   blobs,
	})
	require.NoError(t, err)

	rootMux := http.NewServeMux()
	rootMux.Handle("/mcp", mcpSrv.Handler())

	ts := httptest.NewServer(rootMux)
	defer ts.Close()

	getTaskCall := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"heimdall_get_task","arguments":{}}}`

	// 1. Calling get_task with authorToken -> returns active task for test-author
	reqAuthor, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/mcp", bytes.NewReader([]byte(getTaskCall)))
	require.NoError(t, err)
	reqAuthor.Header.Set("Authorization", "Bearer "+authorToken)
	reqAuthor.Header.Set("Content-Type", "application/json")

	respAuthor, err := ts.Client().Do(reqAuthor)
	require.NoError(t, err)
	defer func() { _ = respAuthor.Body.Close() }()

	require.Equal(t, http.StatusOK, respAuthor.StatusCode)
	bodyAuthor, err := io.ReadAll(respAuthor.Body)
	require.NoError(t, err)

	var rpcResp struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(bodyAuthor, &rpcResp))
	require.False(t, rpcResp.Result.IsError, "must not be error for author: %s", string(bodyAuthor))
	require.NotEmpty(t, rpcResp.Result.Content)
	require.Contains(t, rpcResp.Result.Content[0].Text, string(tid))
	require.Contains(t, rpcResp.Result.Content[0].Text, string(cid))

	// 2. Calling get_task with reviewerToken -> returns refusal because reviewer has no active task
	reqReviewer, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/mcp", bytes.NewReader([]byte(getTaskCall)))
	require.NoError(t, err)
	reqReviewer.Header.Set("Authorization", "Bearer "+reviewerToken)
	reqReviewer.Header.Set("Content-Type", "application/json")

	respReviewer, err := ts.Client().Do(reqReviewer)
	require.NoError(t, err)
	defer func() { _ = respReviewer.Body.Close() }()

	require.Equal(t, http.StatusOK, respReviewer.StatusCode)
	bodyReviewer, err := io.ReadAll(respReviewer.Body)
	require.NoError(t, err)

	var rpcRespReviewer struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(bodyReviewer, &rpcRespReviewer))
	require.True(t, rpcRespReviewer.Result.IsError, "must be error refusal for reviewer")
	require.NotEmpty(t, rpcRespReviewer.Result.Content)
	require.Contains(t, rpcRespReviewer.Result.Content[0].Text, "You have no active task.")
}
