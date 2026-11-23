package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/deliberation"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testset"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/ids"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport/mcp"
)

const mcpInitJSON = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`

func TestMCPHandlerMountedAnswersInitializeAndRefusesBadTokenWithoutLeak(t *testing.T) {
	tokStore := &tokenStore{
		principals: map[string]mcp.Principal{
			mcp.HashToken("valid-token-123"): {
				Agent: id.AgentName("test-author"),
				Role:  workflow.RoleTestAuthor,
			},
		},
	}
	blobs := newMemBlobStore()
	clk := clock.System{}
	vcs := dummyVCS{}
	tasks := &fakeMCPTasks{}
	reviews := &fakeMCPReviews{}

	mcpSrv, err := mcp.NewServer(mcp.Deps{
		Logger:    discardLogger(),
		Clock:     clk,
		Tokens:    tokStore,
		Tasks:     tasks,
		Reviews:   reviews,
		Code:      vcs,
		Blobs:     blobs,
		Knowledge: testMCPKnowledge{},
	})
	require.NoError(t, err)

	rootMux := http.NewServeMux()
	rootMux.Handle("/mcp", mcpSrv.Handler())

	ts := httptest.NewServer(rootMux)
	defer ts.Close()

	// 1. Valid token initialize
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, ts.URL+"/mcp", bytes.NewReader([]byte(mcpInitJSON)))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer valid-token-123")
	req.Header.Set("Content-Type", "application/json")

	resp, err := ts.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Contains(t, string(body), `"serverInfo"`)
	require.Contains(t, string(body), `"heimdall"`)

	// 2. Wrong token: 401 Unauthorized, never leaks the token
	badToken := "secret-wrong-token-CANARY-xyz"
	reqBad, err := http.NewRequestWithContext(context.Background(), http.MethodPost, ts.URL+"/mcp", bytes.NewReader([]byte(mcpInitJSON)))
	require.NoError(t, err)
	reqBad.Header.Set("Authorization", "Bearer "+badToken)
	reqBad.Header.Set("Content-Type", "application/json")

	respBad, err := ts.Client().Do(reqBad)
	require.NoError(t, err)
	defer func() { _ = respBad.Body.Close() }()

	require.Equal(t, http.StatusUnauthorized, respBad.StatusCode)
	require.Equal(t, "Bearer", respBad.Header.Get("WWW-Authenticate"))
	badBody, err := io.ReadAll(respBad.Body)
	require.NoError(t, err)
	require.NotContains(t, string(badBody), badToken)
}

type testMCPKnowledge struct{}

func (testMCPKnowledge) Ask(context.Context, id.AgentName, id.TaskID, change.Repo, id.SHA, ports.MemoryQuery) ([]ports.MemoryItem, error) {
	return nil, nil
}

func TestEveryEffectKindEmittedByWorkflowHasAnExecutor(t *testing.T) {
	gen := ids.New()
	clk := clock.System{}

	// Effects that rows in the workflow emit:
	// open_room -> EffectRoomOpen
	// branch -> EffectGitHubBranch
	// write_tests -> EffectTaskAssign
	// test_qa -> EffectTestQuality
	// commit -> EffectGitHubCommitTests
	// review -> EffectTaskAssign
	emitted := map[change.EffectKind]bool{
		change.EffectRoomOpen:          true,
		change.EffectGitHubBranch:      true,
		change.EffectTaskAssign:        true,
		change.EffectTestQuality:       true,
		change.EffectGitHubCommitTests: true,
		change.EffectGitHubCheck:       true, // emitted by gate evaluator / PR syncer
		change.EffectVerifyEvidence:    true,
		change.EffectAcceptance:        true, // emitted by PR intake
		change.EffectRunChecks:         true, // the checks row of the review flow
		change.EffectProfileTrial:      true, // the profile row of the review flow (D175)
		change.EffectRoomPost:          true, // U1 asks and F2 close summary
		change.EffectRoomClose:         true, // F2 archive room after merge
		change.EffectTrackerWrite:      true, // F1 tracker write-back after commit and merge
	}

	store := newMemStore()
	blobs := newMemBlobStore()
	eng := newSliceEngine(t, store)
	ad := adapters{
		tickets: dummyTickets{},
		vcs:     dummyVCS{},
		chat:    dummyChat{},
	}

	execs, err := executors(ad, store, selectedProfileStore{}, blobs, ingestor{eng: eng}, gen, clk, nil)
	require.NoError(t, err)

	registered := make(map[change.EffectKind]bool)
	for k := range execs {
		registered[k] = true
	}

	require.Equal(t, emitted, registered, "registered executors must match every effect kind the workflow emits")
}

func TestAgentListValidation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		agents  string
		wantErr string
	}{
		{"missing test_author", `reviewers = ["rev1"]` + "\n" + `resolver = "res1"`, "agents.test_author"},
		{"missing reviewers", `test_author = "ta1"` + "\n" + `resolver = "res1"`, "agents.reviewers"},
		{"duplicate reviewers", `test_author = "ta1"` + "\n" + `reviewers = ["rev1", "rev1"]` + "\n" + `resolver = "res1"`, "listed twice"},
		{"missing resolver", `test_author = "ta1"` + "\n" + `reviewers = ["rev1"]`, "agents.resolver"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			toml := fmt.Sprintf("listen = \"127.0.0.1:0\"\nsecrets_dir = \"/tmp\"\n[agents]\n%s\n[[projects]]\ntracker_project = \"KAN\"\nrepo = \"a/b\"\nbase_branch = \"main\"\n", tc.agents)
			path := filepath.Join(t.TempDir(), "heimdall.toml")
			require.NoError(t, os.WriteFile(path, []byte(toml), 0o600))
			_, err := config.Load(path)
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

type fakeMCPTasks struct {
	currentFn func(ctx context.Context, p mcp.Principal) (mcp.Assignment, error)
}

func (f *fakeMCPTasks) Current(ctx context.Context, p mcp.Principal) (mcp.Assignment, error) {
	if f.currentFn != nil {
		return f.currentFn(ctx, p)
	}
	return mcp.Assignment{}, mcp.ErrNoTask
}

func (f *fakeMCPTasks) Ack(context.Context, mcp.Assignment, time.Time) error { return nil }
func (f *fakeMCPTasks) SubmitTests(context.Context, mcp.Assignment, testset.Submission, bool) error {
	return nil
}
func (f *fakeMCPTasks) AskHumans(context.Context, mcp.Assignment, []string) error { return nil }
func (f *fakeMCPTasks) Progress(context.Context, mcp.Assignment, string, time.Time) error {
	return nil
}

type fakeMCPReviews struct{}

func (f *fakeMCPReviews) SubmitReview(context.Context, mcp.Assignment, finding.Review, bool) (mcp.SubmitReceipt, error) {
	return mcp.SubmitReceipt{Accepted: true}, nil
}
func (f *fakeMCPReviews) SubmitVerdict(context.Context, mcp.Assignment, deliberation.Verdict, bool) error {
	return nil
}

func (f *fakeMCPReviews) RecordProfileProposed(context.Context, mcp.Assignment, string, change.Repo) error {
	return nil
}
