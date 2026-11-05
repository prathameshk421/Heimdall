package main

// The tests run the real MCP server from internal/transport/mcp against fakes for its ports, the way the MCP
// package's own tests build it. Only the agent under test is scripted.

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/deliberation"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testset"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport/mcp"
)

// The fixed world of these tests: one test-author task with two acceptance criteria, one changed file and one
// deadline an hour out.
const (
	// testToken is the canary every output test looks for. It must never appear in an output line or an error.
	testToken   = "canary-token-7f3c1e"
	testAgent   = id.AgentName("scripted-author")
	testRepo    = "acme/pay-demo"
	changedFile = "internal/refunds/refunds.go"
	testsDir    = "tests/heimdall/PAY-142"
)

var testNow = time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)

// testTask is the envelope get_task answers with.
func testTask() workflow.TaskEnvelope {
	blob := func(byteHex string) workflow.BlobRef {
		return workflow.BlobRef("blob:sha256:" + byteHex)
	}
	return workflow.TaskEnvelope{
		TaskID: id.TaskID("tsk_01J8Z3K9QW7M2P5R8T0V4YB6CD"),
		Role:   workflow.RoleTestAuthor,
		Skill:  "test-author@1.0.0",
		Change: workflow.ChangeRef{
			ID:      id.ChangeID("PAY-142"),
			Repo:    testRepo,
			BaseSHA: id.SHA("a01b2c3d4e5f60718293a4b5c6d7e8f901234567"),
			HeadSHA: id.SHA("9f1c2ab0c3d4e5f60718293a4b5c6d7e8f901234"),
		},
		Goal: "Write failing tests for every acceptance criterion.",
		Inputs: workflow.Inputs{
			Diff:         blob("1111111111111111111111111111111111111111111111111111111111111111"),
			FilesChanged: []string{changedFile},
			AcceptanceCriteria: []workflow.CriterionRef{
				{ID: "AC1", Ref: blob("2222222222222222222222222222222222222222222222222222222222222222")},
				{ID: "AC2", Ref: blob("3333333333333333333333333333333333333333333333333333333333333333")},
			},
		},
		ContextBudgetTokens: 150000,
		OutputSchema:        "TestSubmission@1",
		Deadline:            testNow.Add(time.Hour),
	}
}

// testClock is the fixed clock the server checks the ack window and the deadline against.
type testClock struct{ now time.Time }

func (c testClock) Now() time.Time { return c.now }

// testTokens resolves the canary token's hash to one test-author principal and remembers every lookup.
type testTokens struct {
	hash      string
	principal mcp.Principal
	seen      []string
}

func (f *testTokens) Resolve(_ context.Context, hash string) (mcp.Principal, error) {
	f.seen = append(f.seen, hash)
	if hash != f.hash {
		return mcp.Principal{}, ports.ErrUnauthorized
	}
	return f.principal, nil
}

// testTasks records every effect the server carries out. The two ready channels fire once, so a test can wait for a
// step without sleeping.
type testTasks struct {
	task      workflow.TaskEnvelope
	assigned  time.Time
	acked     bool
	acks      int
	submits   []testset.Submission
	progress  []string
	ackReady  chan struct{}
	progReady chan struct{}
}

func (f *testTasks) Current(_ context.Context, _ mcp.Principal) (mcp.Assignment, error) {
	return mcp.Assignment{Task: f.task, AssignedAt: f.assigned, Acked: f.acked}, nil
}

func (f *testTasks) Ack(_ context.Context, _ mcp.Assignment, _ time.Time) error {
	f.acked = true
	f.acks++
	notify(f.ackReady)
	return nil
}

func (f *testTasks) SubmitTests(_ context.Context, _ mcp.Assignment, sub testset.Submission, _ bool) error {
	f.submits = append(f.submits, sub)
	return nil
}

func (f *testTasks) AskHumans(_ context.Context, _ mcp.Assignment, _ []string) error { return nil }

func (f *testTasks) Progress(_ context.Context, _ mcp.Assignment, text string, _ time.Time) error {
	f.progress = append(f.progress, text)
	notify(f.progReady)
	return nil
}

// notify sends on ch without blocking when it is nil or already full.
func notify(ch chan struct{}) {
	if ch == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

// testCode serves the changed file at the base commit only, which is all a test-author may read.
type testCode struct{}

func (testCode) ReadFile(_ context.Context, _ change.Repo, sha id.SHA, _ string) ([]byte, error) {
	if sha != id.SHA("a01b2c3d4e5f60718293a4b5c6d7e8f901234567") {
		return nil, ports.ErrNotFound
	}
	return []byte("package refunds\n"), nil
}

// testBlobs serves any blob reference with fixed text.
type testBlobs struct{}

func (testBlobs) Get(_ context.Context, _ workflow.BlobRef) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("criterion text")), nil
}

// newWorld wires the real MCP server to the fakes and serves it over HTTP. It returns the fakes a test asserts on.
func newWorld(t *testing.T) (*testTokens, *testTasks, *httptest.Server) {
	t.Helper()
	tokens := &testTokens{
		hash:      mcp.HashToken(testToken),
		principal: mcp.Principal{Agent: testAgent, Role: workflow.RoleTestAuthor},
	}
	tasks := &testTasks{task: testTask(), assigned: testNow}
	logger := slog.New(slog.DiscardHandler)
	srv, err := mcp.NewServer(mcp.Deps{
		Logger:  logger,
		Clock:   testClock{now: testNow},
		Tokens:  tokens,
		Tasks:   tasks,
		Reviews: testReviews{},
		Code:    testCode{},
		Blobs:   testBlobs{},
	})
	require.NoError(t, err)
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)
	return tokens, tasks, hs
}

// writeToken writes the canary token to a file the way admin init does, with the trailing newline a file holds.
func writeToken(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(path, []byte(testToken+"\n"), 0o600))
	return path
}

// hashOf is the lookup key the server holds for a token; it is the only form the server sees.
func hashOf(token string) string { return mcp.HashToken(token) }

// testReviews is the review side of the server. The scripted test-author never submits a review or a verdict.
type testReviews struct{}

func (testReviews) SubmitReview(context.Context, mcp.Assignment, finding.Review, bool) (mcp.SubmitReceipt, error) {
	return mcp.SubmitReceipt{Accepted: true}, nil
}

func (testReviews) SubmitVerdict(context.Context, mcp.Assignment, deliberation.Verdict, bool) error {
	return nil
}
