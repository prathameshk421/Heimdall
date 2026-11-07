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
	"slices"
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

// The fixed world of these tests: one task with two acceptance criteria, changed files and one deadline an hour out.
const (
	// testToken is the canary every output test looks for. It must never appear in an output line or an error.
	testToken   = "canary-token-7f3c1e"
	testAgent   = id.AgentName("scripted-author")
	testRepo    = "acme/pay-demo"
	changedFile = "internal/refunds/refunds.go"
	testsDir    = "tests/heimdall/PAY-142"
	testBase    = id.SHA("a01b2c3d4e5f60718293a4b5c6d7e8f901234567")
	testHead    = id.SHA("9f1c2ab0c3d4e5f60718293a4b5c6d7e8f901234")
	// baseFile is what the fake repository holds at the base commit, for every path.
	baseFile = "package refunds\n"
	// headFile is what the fake repository holds at the head commit, for every path. It has three lines.
	headFile = "package refunds\n\nfunc Refund() error { return nil }\n"
)

var testNow = time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)

// testTask is the envelope get_task answers with for a test-author.
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
			BaseSHA: testBase,
			HeadSHA: testHead,
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

// testTokens resolves the canary token's hash to one principal and remembers every lookup.
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

// testCode serves a fixed file at the base and the head commit, and records every read so a test can see what the
// agent read and in which order.
type testCode struct {
	reads []string
}

func (c *testCode) ReadFile(_ context.Context, _ change.Repo, sha id.SHA, path string) ([]byte, error) {
	c.reads = append(c.reads, string(sha)+" "+path)
	switch sha {
	case testBase:
		return []byte(baseFile), nil
	case testHead:
		return []byte(headFile), nil
	}
	return nil, ports.ErrNotFound
}

// testBlobs serves the text of each blob it is given, or a fixed text for a blob it does not know, and records every
// reference that was read.
type testBlobs struct {
	texts map[workflow.BlobRef]string
	reads []workflow.BlobRef
}

func (b *testBlobs) Get(_ context.Context, ref workflow.BlobRef) (io.ReadCloser, error) {
	b.reads = append(b.reads, ref)
	if text, ok := b.texts[ref]; ok {
		return io.NopCloser(strings.NewReader(text)), nil
	}
	return io.NopCloser(strings.NewReader("criterion text")), nil
}

// testReviews is the review side of the server. It records every review and verdict. It returns a finding error for
// each finding whose file is not one of the change's files, as the application does.
type testReviews struct {
	reviews  []finding.Review
	verdicts []deliberation.Verdict
}

func (f *testReviews) SubmitReview(_ context.Context, a mcp.Assignment, r finding.Review, _ bool) (mcp.SubmitReceipt, error) {
	f.reviews = append(f.reviews, r)
	var errs []mcp.FindingError
	for i, fi := range r.Findings {
		if !slices.Contains(a.Task.Inputs.FilesChanged, fi.Location.File) {
			errs = append(errs, mcp.FindingError{Index: i, Message: "location.file is not a file of this change"})
		}
	}
	return mcp.SubmitReceipt{Accepted: len(errs) == 0, FindingErrors: errs}, nil
}

func (f *testReviews) SubmitVerdict(_ context.Context, _ mcp.Assignment, v deliberation.Verdict, _ bool) error {
	f.verdicts = append(f.verdicts, v)
	return nil
}

// world is the real MCP server wired to the fakes, and the fakes a test asserts on.
type world struct {
	tokens  *testTokens
	tasks   *testTasks
	reviews *testReviews
	code    *testCode
	blobs   *testBlobs
	hs      *httptest.Server
}

// buildWorld serves the real MCP server for a token of tokenRole and a task. The task is what get_task answers with.
func buildWorld(t *testing.T, tokenRole workflow.Role, task workflow.TaskEnvelope, blobs *testBlobs) world {
	t.Helper()
	w := world{
		tokens: &testTokens{
			hash:      mcp.HashToken(testToken),
			principal: mcp.Principal{Agent: testAgent, Role: tokenRole},
		},
		tasks:   &testTasks{task: task, assigned: testNow},
		reviews: &testReviews{},
		code:    &testCode{},
		blobs:   blobs,
	}
	srv, err := mcp.NewServer(mcp.Deps{
		Logger:    slog.New(slog.DiscardHandler),
		Clock:     testClock{now: testNow},
		Tokens:    w.tokens,
		Tasks:     w.tasks,
		Reviews:   w.reviews,
		Code:      w.code,
		Blobs:     w.blobs,
		Knowledge: knowledgeStub{},
	})
	require.NoError(t, err)
	w.hs = httptest.NewServer(srv.Handler())
	t.Cleanup(w.hs.Close)
	return w
}

type knowledgeStub struct{}

func (knowledgeStub) Ask(context.Context, id.AgentName, id.TaskID, change.Repo, id.SHA, ports.MemoryQuery) ([]ports.MemoryItem, error) {
	return nil, nil
}

// newWorld wires the real MCP server to the fakes for a test-author and serves it over HTTP. It returns the fakes a
// test asserts on.
func newWorld(t *testing.T) (*testTokens, *testTasks, *httptest.Server) {
	t.Helper()
	w := buildWorld(t, workflow.RoleTestAuthor, testTask(), &testBlobs{})
	return w.tokens, w.tasks, w.hs
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
