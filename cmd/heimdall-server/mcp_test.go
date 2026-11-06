package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/engine"
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

func TestWorkflowBuildsAndEngineAcceptsIt(t *testing.T) {
	agents := config.Agents{
		TestAuthor: "test-author",
		Reviewers:  []string{"reviewer-1", "reviewer-2"},
		Resolver:   "resolver",
	}
	gen := ids.New()
	clk := clock.System{}

	wf, steps, err := sliceSteps(agents, gen, clk)
	require.NoError(t, err)

	store := newMemStore()
	eng, err := engine.New(engine.Config{
		Workflow:    wf,
		Steps:       steps,
		Store:       store,
		Clock:       clk,
		IDs:         gen,
		MaxAttempts: maxAttempts,
		Roles:       sliceRoles(),
		Schemas:     sliceSchemas(),
	})
	require.NoError(t, err)
	require.NotNil(t, eng)

	// Step count and ids: exactly the 11 implemented steps
	expectedIDs := []string{
		"open_room", "branch", "write_tests", "test_qa", "commit",
		"wait_pr", "slop", "pick", "review", "verify", "deliberate",
	}
	actualIDs := make([]string, len(wf.Steps))
	for i, s := range wf.Steps {
		actualIDs[i] = s.ID
	}
	require.Equal(t, expectedIDs, actualIDs)

	// Commit step after list must be trimmed to surviving ancestors
	for _, s := range wf.Steps {
		if s.ID == "commit" {
			require.Equal(t, []string{"test_qa"}, s.After, "commit after must be trimmed to surviving ancestors")
		}
	}
}

func TestEveryEffectKindEmittedByWorkflowHasAnExecutor(t *testing.T) {
	agents := config.Agents{
		TestAuthor: "test-author",
		Reviewers:  []string{"reviewer-1", "reviewer-2"},
		Resolver:   "resolver",
	}
	gen := ids.New()
	clk := clock.System{}

	_, _, err := sliceSteps(agents, gen, clk)
	require.NoError(t, err)

	// Effects that steps in the workflow emit:
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
	}

	store := newMemStore()
	blobs := newMemBlobStore()
	eng := newSliceEngine(t, store)
	ad := adapters{
		tickets: dummyTickets{},
		vcs:     dummyVCS{},
		chat:    dummyChat{},
	}

	execs, err := executors(context.Background(), ad, store, blobs, ingestor{eng: eng}, gen, clk)
	require.NoError(t, err)

	registered := make(map[change.EffectKind]bool)
	for k := range execs {
		registered[k] = true
	}

	require.Equal(t, emitted, registered, "registered executors must match every effect kind the workflow emits")
}

func TestAgentListValidation(t *testing.T) {
	// 1. Missing test_author
	bad1 := `
listen = "127.0.0.1:0"
secrets_dir = "/tmp"

[agents]
reviewers = ["rev1"]
resolver = "res1"

[[projects]]
tracker_project = "KAN"
repo = "a/b"
base_branch = "main"
`
	dir1 := t.TempDir()
	path1 := filepath.Join(dir1, "heimdall.toml")
	require.NoError(t, os.WriteFile(path1, []byte(bad1), 0o600))
	_, err1 := config.Load(path1)
	require.ErrorContains(t, err1, "agents.test_author")

	// 2. Missing reviewers
	bad2 := `
listen = "127.0.0.1:0"
secrets_dir = "/tmp"

[agents]
test_author = "ta1"
resolver = "res1"

[[projects]]
tracker_project = "KAN"
repo = "a/b"
base_branch = "main"
`
	dir2 := t.TempDir()
	path2 := filepath.Join(dir2, "heimdall.toml")
	require.NoError(t, os.WriteFile(path2, []byte(bad2), 0o600))
	_, err2 := config.Load(path2)
	require.ErrorContains(t, err2, "agents.reviewers")

	// 3. Duplicate reviewers
	bad3 := `
listen = "127.0.0.1:0"
secrets_dir = "/tmp"

[agents]
test_author = "ta1"
reviewers = ["rev1", "rev1"]
resolver = "res1"

[[projects]]
tracker_project = "KAN"
repo = "a/b"
base_branch = "main"
`
	dir3 := t.TempDir()
	path3 := filepath.Join(dir3, "heimdall.toml")
	require.NoError(t, os.WriteFile(path3, []byte(bad3), 0o600))
	_, err3 := config.Load(path3)
	require.ErrorContains(t, err3, "agents.reviewers")
	require.ErrorContains(t, err3, "listed twice")

	// 4. Missing resolver
	bad4 := `
listen = "127.0.0.1:0"
secrets_dir = "/tmp"

[agents]
test_author = "ta1"
reviewers = ["rev1"]

[[projects]]
tracker_project = "KAN"
repo = "a/b"
base_branch = "main"
`
	dir4 := t.TempDir()
	path4 := filepath.Join(dir4, "heimdall.toml")
	require.NoError(t, os.WriteFile(path4, []byte(bad4), 0o600))
	_, err4 := config.Load(path4)
	require.ErrorContains(t, err4, "agents.resolver")
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
