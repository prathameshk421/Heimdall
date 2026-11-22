//go:build component

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/postgres"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/rootlessrunner"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/outbox"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	testpg "github.com/prathameshk421/flip_the_switch_hackathon/internal/testkit/postgres"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport"
)

// The flow rig boots the real server wiring (selectAdapters and assemble, the same calls build makes) on a real
// Postgres, with fake Jira, Switch, GitHub and model provider, and the real runner on a Unix socket over a fake container
// runtime. The clock is fake: the test moves it, then calls Sweep and drains the outbox instead of waiting for the loops.

const (
	flowChange    = id.ChangeID("KAN-1")
	flowBranch    = "KAN-1-add-refunds"
	flowTestsDir  = "tests/heimdall/KAN-1"
	flowProduct   = "pkg/pay/refund.go"
	flowProfileID = "acme-pay"
	flowGoImage   = "registry.example/go@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	flowChecksImg = "registry.example/checks@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

// flowPolicy is the repository's heimdall.yaml: bob approves payments; three reviewers; an E2 finding of medium
// severity blocks.
const flowPolicy = `version: 1
workflow: default
approvers: { default: [bob], payments: [bob] }
policy_owners: [bob]
people:
  bob: { github: bob, chat: dev-bob, teams: [payments] }
  ana: { github: dev-ana, chat: dev-ana, teams: [payments] }
domains: { "pkg/**": payments }
reviewers: { count: 3, pick_timeout: 15m }
ci_required: [build]
github: { mode: poll, interval: 10s }
slop: { max_changed_lines: 400, require_ticket: true }
tests:
  dir: tests/heimdall
  allowed_imports: [github.com/stretchr/testify]
  min_mutation_score: 0.6
blocking: { min_severity: medium, min_tier: E2 }
consensus: { min_high_reporters: 2, block_score: 1.5, min_reporters: majority, confidence: { high: 1.0, medium: 0.5, low: 0.2 }, weight: { start: 0.7, min: 0.3, max: 1.0, min_decided: 5 } }
deliberation: { max_rounds: 2, human_only_domains: [money_movement, data_deletion] }
keys: { openrouter: production }
models: { provider: openrouter, test_author: openrouter/deepseek/deepseek-v4-flash, reviewer: openrouter/deepseek/deepseek-v4-flash, resolver: openrouter/deepseek/deepseek-v4-flash, extraction: deepseek/deepseek-v4-flash, embedding: "openrouter:mongodb/voyage-4-lite" }
budgets: { test_author: 400000, reviewer: 150000, resolver: 120000, per_change: 1200000 }
skills: { test_author: "1.0.0", reviewer: "1.0.0", resolver: "1.0.0" }
memory: { rooms: [payments-eng], chat_keywords: [decided, decision, "we will", incident, postmortem] }
pack: { max_age_days: 14 }
timeouts: { question: 4h, remind_after: 2h, escalate_after: 8h, reject_after: 24h }
chat: { bridge: mattermost, verbosity: milestones, decisions_in_room: true }
tracker: { kind: jira, mode: poll, interval: 30s }
agents: { runtime: opencode }
`

// flowProfile is the repository's promoted profile: Go tests and one "patterns" check over the Go files.
const flowProfile = `languages: [go]
image: ` + flowGoImage + `
acceptance: "go test -json ./{dir}/..."
seam: "the exported functions of pkg/pay"
ac_tag: "// AC:"
paths:
  tests: tests/heimdall
checks:
  - id: patterns
    lens: security
    when: ["**/*.go"]
    image: checks
    run: "patterns-scan --sarif-output /out/result.sarif /work"
    baseline: true
`

// flowProductBefore is the product at main: refunds are not implemented.
const flowProductBefore = `package pay

// Refund refunds part of a payment.
func Refund(payment, amount int) (int, error) {
	return amount, nil
}
`

// flowProductFixed is the developer's push: the amount is checked.
const flowProductFixed = `package pay

import "errors"

// refunds: implemented
// Refund refunds part of a payment.
func Refund(payment, amount int) (int, error) {
	if amount > payment {
		return 0, errors.New("a refund cannot exceed the payment")
	}
	return amount, nil
}
`

// flowClock is the fake clock of the rig. The test moves it; nothing else does.
type flowClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *flowClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *flowClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// flowRig is one booted server and its fakes.
type flowRig struct {
	t       *testing.T
	ctx     context.Context
	clock   *flowClock
	app     *app
	store   *postgres.Store
	jira    *fakeJira
	sw      *fakeSwitch
	gh      *fakeRepoGitHub
	model   *fakeModelProvider
	runner  *flowContainers
	mcp     *httptest.Server
	logs    *lockedBuffer
	rpcID   int
	allowed []string // effect failures the test expects, by effect kind
}

// newFlowRig boots the server on a fresh database. The repository has the policy, the product without refunds and a
// promoted profile.
func newFlowRig(t *testing.T) *flowRig {
	t.Helper()
	ctx := context.Background()
	r := &flowRig{t: t, ctx: ctx, clock: &flowClock{now: time.Now().UTC()}, logs: &lockedBuffer{}}
	dsn := testpg.Start(t)
	r.jira = newFakeJira(t, time.Now().Add(-time.Minute))
	r.sw = newFakeSwitch(t)
	r.gh = newFakeRepoGitHub(t, map[string]string{"heimdall.yaml": flowPolicy, flowProduct: flowProductBefore})
	r.model = newFakeModelProvider(t)
	profile, socket := r.startRunner()
	secrets := writeAcceptanceSecrets(t)
	for name, value := range map[string]string{"agent_token_reviewer-3": "agent-token-reviewer-3-value", "deploy_api_key": flowModelKey} {
		require.NoError(t, os.WriteFile(filepath.Join(secrets, name), []byte(value+"\n"), 0o600))
	}
	cfg, err := config.Load(writeFlowConfig(t, secrets))
	require.NoError(t, err)
	logger := slog.New(slog.NewTextHandler(r.logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	d := deps{
		databaseURL: dsn, configuration: cfg,
		endpoints: endpoints{jiraBaseURL: r.jira.srv.URL, githubAPI: r.gh.srv.URL, switchBaseURL: r.sw.srv.URL,
			switchAgentID: acceptanceAgentID, switchAgentName: acceptanceAgentName, switchBridgeID: acceptanceBridgeID,
			runnerSocket: socket, graphitiURL: testGraphitiURL, modelURL: r.model.srv.URL},
		httpClient: &http.Client{}, logger: logger,
		pollEvery: time.Second, syncEvery: time.Second, drainTick: time.Second,
		openPool: pgxpool.New, migrate: postgres.Migrate,
	}
	require.NoError(t, d.validate())
	ad, err := selectAdapters(cfg, d)
	require.NoError(t, err)
	pool, err := d.openPool(ctx, d.databaseURL)
	require.NoError(t, err)
	a, err := assemble(ctx, d, ad, pool, r.clock)
	require.NoError(t, err, r.logs.String())
	t.Cleanup(a.close)
	r.app, r.store = a, postgres.NewStore(pool)
	require.NotContains(t, r.logs.String(), "run_check withheld", "the runner answered the boot self-check")
	r.promote(profile)

	router, err := transport.NewDegradedRouter(logger, a.degraded)
	require.NoError(t, err)
	r.mcp = httptest.NewServer(mountHTTP(router, a))
	t.Cleanup(r.mcp.Close)
	t.Cleanup(func() {
		for _, line := range strings.Split(r.logs.String(), "\n") {
			if strings.Contains(line, "outbox effect failed") && !r.allowedFailure(line) {
				t.Errorf("an effect failed: %s", line)
			}
		}
	})
	return r
}

func (r *flowRig) allowedFailure(line string) bool {
	for _, kind := range r.allowed {
		if strings.Contains(line, "kind="+kind) {
			return true
		}
	}
	return false
}

// startRunner serves the real runner handler on a fresh socket, with the promoted profile and the fake containers.
func (r *flowRig) startRunner() (rootlessrunner.RepoProfile, string) {
	t := r.t
	allow := []string{flowGoImage, flowChecksImg}
	profile, err := rootlessrunner.ParseApprovedProfile([]byte(flowProfile), append(allow, "checks", "go"))
	require.NoError(t, err)
	r.runner = &flowContainers{findingAt: map[string]string{}}
	policy := rootlessrunner.Policy{Images: map[string]string{"go": flowGoImage, "checks": flowChecksImg},
		Profiles: map[string]rootlessrunner.RepoProfile{flowProfileID: profile}}
	run, err := rootlessrunner.NewRunner(policy, r.runner, t.TempDir(), "", allow)
	require.NoError(t, err)
	dir, err := os.MkdirTemp("", "hf")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "runner.sock")
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- rootlessrunner.Listen(ctx, socket, run.Handler()) }()
	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-served)
	})
	client := rootlessrunner.Client{Socket: socket}
	require.Eventually(t, func() bool {
		_, err := client.Health(context.Background())
		return err == nil
	}, 5*time.Second, 5*time.Millisecond, "the runner socket answers")
	return profile, socket
}

// promote records the profile as the repository's promoted one, through the store's own lifecycle.
func (r *flowRig) promote(profile rootlessrunner.RepoProfile) {
	t, now := r.t, r.clock.Now()
	p := ports.ProfileProposal{ID: strings.Repeat("e", 64), Repo: flowRepo, State: ports.ProfileProposed, Author: "bob",
		Artifact: []byte(flowProfile), CreatedAt: now, Spec: rootlessrunner.Spec(profile)}
	ok, err := r.store.PutProfile(r.ctx, p)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = r.store.ApproveProfile(r.ctx, p.ID, "bob", now)
	require.NoError(t, err)
	require.True(t, ok)
	trial := ports.ProfileTrial{Outcome: ports.TrialPassed, Passed: 1, SHA: flowBaseSHA, LogRef: workflow.BlobRef("blob:sha256:" + strings.Repeat("d", 64))}
	ok, err = r.store.RecordProfileTrial(r.ctx, p.ID, trial, now)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = r.store.PromoteProfile(r.ctx, p.ID, now)
	require.NoError(t, err)
	require.True(t, ok)
}

// poll runs every ticket poller once.
func (r *flowRig) poll() {
	for _, p := range r.app.pollers {
		_, err := p.PollOnce(r.ctx)
		require.NoError(r.t, err)
	}
}

// syncPRs runs one pull request sync.
func (r *flowRig) syncPRs() {
	res, err := r.app.syncer.SyncOnce(r.ctx)
	require.NoError(r.t, err)
	require.Empty(r.t, res.Failed, "every pull request synced")
}

// settle sweeps and drains until a pass runs no effect and records no fact: what the loops would do if the clock stood
// still.
func (r *flowRig) settle() {
	quick, ok := r.app.drainer.(*outbox.Drainer)
	require.True(r.t, ok)
	drainers := []*outbox.Drainer{quick}
	if slow, ok := r.app.slowDrainer.(*outbox.Drainer); ok && slow != nil {
		drainers = append(drainers, slow)
	}
	for pass := 0; pass < 30; pass++ {
		before := r.events()
		require.NoError(r.t, r.app.reconciler.Sweep(r.ctx))
		ran := 0
		for _, d := range drainers {
			for {
				res, err := d.RunOnce(r.ctx)
				require.NoError(r.t, err)
				if res.Claimed == 0 {
					break
				}
				ran += res.Claimed
			}
		}
		if ran == 0 && r.events() == before {
			return
		}
	}
	r.t.Fatal("the change did not settle in 30 passes")
}

// events counts the events of the flow's change.
func (r *flowRig) events() int {
	evs, err := r.store.Load(r.ctx, flowChange)
	require.NoError(r.t, err)
	return len(evs)
}

// view folds the change's log.
func (r *flowRig) view() change.View {
	evs, err := r.store.Load(r.ctx, flowChange)
	require.NoError(r.t, err)
	v, err := change.Fold(evs)
	require.NoError(r.t, err)
	return v
}

// count counts the events of one type of the change.
func (r *flowRig) count(typ change.EventType) int {
	evs, err := r.store.Load(r.ctx, flowChange)
	require.NoError(r.t, err)
	return eventTypeCount(evs, typ)
}

// call runs one MCP tool as the agent with token.
func (r *flowRig) call(token, tool string, args any) componentMCPResult {
	r.rpcID++
	return callComponentMCPTool(r.t, r.mcp, token, r.rpcID, tool, args)
}

// flowTask is the part of a get_task answer the scripted agents read.
type flowTask struct {
	Task struct {
		ID     string `json:"id"`
		Kind   string `json:"kind"`
		Change string `json:"change"`
	} `json:"task"`
	Inputs []struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	} `json:"inputs"`
	Needs []string `json:"needs"`
}

// input returns the content of the named input.
func (f flowTask) input(name string) string {
	for _, in := range f.Inputs {
		if in.Name == name {
			return in.Content
		}
	}
	return ""
}

// getTask calls get_task and decodes the task. It fails the test when the agent has no task.
func (r *flowRig) getTask(token string) (flowTask, string) {
	res := r.call(token, "get_task", map[string]any{})
	require.False(r.t, res.IsError, res.Text)
	var task flowTask
	require.NoError(r.t, json.Unmarshal([]byte(res.Text), &task), res.Text)
	require.NotEmpty(r.t, task.Task.Kind, "the agent has a task: %s", res.Text)
	return task, res.Text
}

const (
	authorToken = "agent-token-author-value"
)

func reviewerToken(seat string) string { return "agent-token-" + seat + "-value" }

// flowTest is a red-first test file for the three criteria of KAN-1. When passesAnyway is set, the test passes on the
// base commit too, which the verifier must reject.
func flowTest(passesAnyway bool) string {
	note := "fails until refunds are implemented"
	if passesAnyway {
		note = "passes without the feature"
	}
	return fmt.Sprintf(`package tests

import (
	"testing"

	"github.com/acme/pay/pkg/pay"
)

// The test %s.

// AC: AC1
func TestRefundCannotExceedPayment(t *testing.T) {
	if _, err := pay.Refund(100, 101); err == nil {
		t.Fatal("a refund above the payment was accepted")
	}
}

// AC: AC2
func TestRefundTwiceReturnsFirst(t *testing.T) {
	if _, err := pay.Refund(100, 101); err == nil {
		t.Fatal("the second refund was not refused")
	}
}

// AC: AC3
func TestOnlyMerchantRefunds(t *testing.T) {
	if _, err := pay.Refund(100, 101); err == nil {
		t.Fatal("a refund by another account was accepted")
	}
}
`, note)
}

// submitTests sends the scripted test author's submission.
func (r *flowRig) submitTests(passesAnyway bool) componentMCPResult {
	return r.call(authorToken, "submit_tests", map[string]any{
		"files": []map[string]string{{"path": flowTestsDir + "/refund_test.go", "content": flowTest(passesAnyway)}},
		"acceptance_criteria": map[string][]string{
			"AC1": {"TestRefundCannotExceedPayment"}, "AC2": {"TestRefundTwiceReturnsFirst"}, "AC3": {"TestOnlyMerchantRefunds"},
		},
		// The slop scope rule holds the pull request to these paths and the tests directory.
		"expected_paths": []string{flowProduct},
	})
}

// runTestsFlow plays B9 item 1 up to the committed tests: ticket, room, branch, tests task, a rejected submission, the
// rejection in get_task, a good resubmission, verified, committed.
func (r *flowRig) runTestsFlow() {
	t := r.t
	r.poll()
	r.settle()
	v := r.view()
	require.NotEmpty(t, v.Room, "the room is opened")
	require.NotNil(t, v.Branch, "the branch is made")
	require.Equal(t, string(flowBaseSHA), string(r.gh.branchHead(flowBranch)), "the branch starts at main")
	require.Equal(t, 1, r.count(change.EventTaskAssigned), "one tests task")
	require.Contains(t, strings.Join(r.sw.postBodies(), "\n"), "@test-author", "the test author is woken in the room")

	task, _ := r.getTask(authorToken)
	require.Equal(t, "tests", task.Task.Kind)
	require.Contains(t, task.input("ticket"), "A refund cannot exceed the original payment.")

	first := r.submitTests(true)
	require.False(t, first.IsError, first.Text)
	r.settle()
	v = r.view()
	require.NotNil(t, v.Tests)
	require.NotEmpty(t, v.Tests.Rejected, "a test that passes on main is rejected")
	require.Equal(t, 2, r.count(change.EventTaskAssigned), "a rejection makes a new tests task")

	_, again := r.getTask(authorToken)
	require.Contains(t, again, "passed without the feature on base", "get_task tells the author why the tests were rejected")

	second := r.submitTests(false)
	require.False(t, second.IsError, second.Text)
	r.settle()
	v = r.view()
	require.NotNil(t, v.Tests.Verified, "the red-first tests are verified")
	require.NotNil(t, v.Tests.Committed, "the verified tests are committed")
	head := r.gh.branchHead(flowBranch)
	require.Equal(t, v.Tests.Committed.SHA, head, "the commit is the branch head")
	require.Equal(t, flowTest(false), r.gh.filesAt(head)[flowTestsDir+"/refund_test.go"], "the committed file is the submission")
	require.Equal(t, 1, r.count(change.EventTestsCommitted))
}

// state says what the change still needs and what the server logged, for a failure message.
func (r *flowRig) state() string {
	needs, err := r.app.reconciler.Needs(r.ctx, flowChange)
	evs, _ := r.store.Load(r.ctx, flowChange)
	var types []string
	for _, e := range evs {
		types = append(types, string(e.Type))
	}
	return fmt.Sprintf("needs %v (err %v); events %v; logs:\n%s", needs, err, types, r.logs.String())
}
