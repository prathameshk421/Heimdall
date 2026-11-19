//go:build component

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/postgres"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	testpg "github.com/prathameshk421/flip_the_switch_hackathon/internal/testkit/postgres"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport"
)

// lockedBuffer is a log sink that several goroutines may write.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestAServerWithoutARunnerStartsWithoutRunCheck boots the real wiring (build, the real Postgres, fake Jira, GitHub
// and Switch) with no runner on the socket. The server starts, logs "run_check withheld: runner unreachable", leaves
// run_check out of a reviewer's tool list and reports the withheld tool on /healthz (HARNESS_DESIGN B8, H9).
func TestAServerWithoutARunnerStartsWithoutRunCheck(t *testing.T) {
	ctx := context.Background()
	dsn := testpg.Start(t)
	jira := newFakeJira(t, time.Now().Add(-time.Minute))
	sw := newFakeSwitch(t)
	gh := newFakeGitHub(t)
	cfg, err := config.Load(writeAcceptanceConfig(t, writeAcceptanceSecrets(t)))
	require.NoError(t, err)
	logs := &lockedBuffer{}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	d := deps{
		databaseURL: dsn,
		endpoints: endpoints{jiraBaseURL: jira.srv.URL, githubAPI: gh.srv.URL, switchBaseURL: sw.srv.URL,
			switchAgentID: acceptanceAgentID, switchAgentName: acceptanceAgentName, switchBridgeID: acceptanceBridgeID,
			runnerSocket: testRunnerSocket, graphitiURL: testGraphitiURL, modelURL: testModelURL},
		httpClient: &http.Client{},
		logger:     logger,
		pollEvery:  time.Second,
		syncEvery:  time.Second,
		drainTick:  time.Second,
		openPool:   pgxpool.New,
		migrate:    postgres.Migrate,
	}
	a, err := build(ctx, cfg, d)
	require.NoError(t, err, "a missing runner degrades the server; it does not stop it")
	t.Cleanup(a.close)

	require.Equal(t, 1, strings.Count(logs.String(), `msg="run_check withheld: runner unreachable"`), logs.String())
	require.NotContains(t, logs.String(), "read_code withheld", "the fake code host answered, so its tools stay")
	require.Contains(t, logs.String(), `msg="read_card withheld: cards_dir is not configured"`, "a tool the config turns off is said once")

	// The same handler run() serves: the degraded router under the app's routes.
	router, err := transport.NewDegradedRouter(logger, a.degraded)
	require.NoError(t, err)
	srv := httptest.NewServer(mountHTTP(router, a))
	t.Cleanup(srv.Close)

	tools := listedTools(t, srv, "agent-token-reviewer-1-value")
	require.NotContains(t, tools, "run_check")
	require.Contains(t, tools, "submit_review", "the reviewer keeps every tool whose dependency answered")
	require.Contains(t, tools, "read_code")

	hreq, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/healthz", nil)
	require.NoError(t, err)
	resp, err := srv.Client().Do(hreq)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	var health struct {
		Status   string   `json:"status"`
		Degraded []string `json:"degraded"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&health))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "ok", health.Status)
	require.Contains(t, health.Degraded, "run_check: runner unreachable")
	require.Contains(t, health.Degraded, "read_card: cards_dir is not configured")
}

// listedTools returns the names tools/list gives the agent with token.
func listedTools(t *testing.T, srv *httptest.Server, token string) []string {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+"/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var rpc struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(body, &rpc))
	names := make([]string, 0, len(rpc.Result.Tools))
	for _, tool := range rpc.Result.Tools {
		names = append(names, tool.Name)
	}
	return names
}
