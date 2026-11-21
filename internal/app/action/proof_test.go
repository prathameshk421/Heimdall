package action_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/policyyaml"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/action"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/agentloop"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/external"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Component test of the proof use case (ACTION_MODE 4, 5.2, 6, 7; work package AM14b). Every port is an in-memory fake
// in this file: a scripted model, a fake sandbox that answers per test name, a fake code reader and a real temporary
// directory for the head checkout. The test drives app.action.Proof through its public API only.
//
// The agent loop is the product's own, agentloop.Runner, wired here over the scripted model. Only this test file
// imports it: the dependency rule (R3) restricts production imports, and the point of the test is that the proof run
// works with the loop that ships. proof_loop_port_test.go drives the same use case through a fake ports.AgentLoop.

const (
	proofRepo = change.Repo("acme/shop")
	proofNum  = 9
	// The run's input. The hashes are real 40-hex ids and the policy hash is a real sha256.
	proofHead  = id.SHA("4444444444444444444444444444444444444444")
	proofBase  = id.SHA("5555555555555555555555555555555555555555")
	proofHash  = "9f2c0f0d3a2b1e4c5d6f7a8b9c0d1e2f3a4b5c6d7e8f9012345678abcdef0123"
	proofRunID = int64(7001)
	sourcePath = "internal/pay/refund.go"
	proofTest  = "internal/pay/refund_proof_test.go"
	testName   = "TestRefundTotalIsWithinTheOrder"
)

// --- the scripted model ---

// scriptedModel answers each call with the next step, as the bench's fake model does.
type scriptedModel struct {
	t        *testing.T
	steps    []modelStep
	requests []ports.ModelRequest
}

type modelStep struct {
	resp ports.ModelResponse
	err  error
}

func (m *scriptedModel) Complete(ctx context.Context, req ports.ModelRequest) (ports.ModelResponse, error) {
	m.requests = append(m.requests, req)
	if len(m.requests) > len(m.steps) {
		m.t.Fatalf("model called %d times, the script holds %d steps", len(m.requests), len(m.steps))
	}
	step := m.steps[len(m.requests)-1]
	if err := ctx.Err(); err != nil {
		return ports.ModelResponse{}, err
	}
	return step.resp, step.err
}

// toolCall answers with one tool call and a fixed cost, as a provider does.
func toolCall(id, name string, args any, costUSD float64) modelStep {
	raw, err := json.Marshal(args)
	if err != nil {
		panic(err)
	}
	return modelStep{resp: ports.ModelResponse{ToolCalls: []ports.ModelToolCall{{ID: id, Name: name, Arguments: string(raw)}},
		Finish: ports.ModelFinishToolCalls, Usage: ports.ModelUsage{InputTokens: 1200, OutputTokens: 200}, CostUSD: costUSD}}
}

// --- the fake sandbox ---

// fakeSandbox answers a repro job from a table keyed by tree and test name. It records every job, so a test can check
// that PR code ran only through it and only on the trees the run named.
type fakeSandbox struct {
	mu sync.Mutex
	// outcomes is the outcome per "tree|test name". A missing entry is a passing run of the whole package.
	outcomes map[string]ports.SandboxResult
	jobs     []ports.SandboxJob
	overlays [][]ports.File
	fail     error
}

var _ action.OverlaySandbox = (*fakeSandbox)(nil)

func newFakeSandbox() *fakeSandbox {
	return &fakeSandbox{outcomes: map[string]ports.SandboxResult{}}
}

// set answers the proposed test's runs on one tree.
func (s *fakeSandbox) set(tree string, passed, failed int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res := ports.SandboxResult{}
	var tests []ports.TestResult
	for i := range passed {
		tests = append(tests, ports.TestResult{Name: fmt.Sprintf("TestOther%d", i), Outcome: ports.OutcomePassed})
	}
	res.Passed = passed
	if passed == 0 && failed == 0 {
		res.Passed = 1
		tests = append(tests, ports.TestResult{Name: "TestOther", Outcome: ports.OutcomePassed})
	}
	if failed > 0 {
		res.Failed = failed
		tests = append(tests, ports.TestResult{Name: testName, Outcome: ports.OutcomeFailure})
	} else {
		tests = append(tests, ports.TestResult{Name: testName, Outcome: ports.OutcomePassed})
	}
	res.Tests = tests
	res.Output = "--- FAIL: " + testName + "\n"
	s.outcomes[tree+"|"+testName] = res
}

func (s *fakeSandbox) Run(ctx context.Context, j ports.SandboxJob) (ports.SandboxResult, error) {
	return s.RunOverlay(ctx, j, nil)
}

func (s *fakeSandbox) RunOverlay(_ context.Context, j ports.SandboxJob, overlays []ports.File) (ports.SandboxResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = append(s.jobs, j)
	s.overlays = append(s.overlays, overlays)
	if s.fail != nil {
		return ports.SandboxResult{}, s.fail
	}
	name := testName
	for _, f := range overlays {
		if !strings.HasSuffix(f.Path, "_test.go") {
			continue
		}
		if n := declaredTestName(string(f.Content)); n != "" {
			name = n
		}
	}
	if res, ok := s.outcomes[string(j.SHA)+"|"+name]; ok {
		return res, nil
	}
	return ports.SandboxResult{Passed: 1, Tests: []ports.TestResult{{Name: name, Outcome: ports.OutcomePassed}}}, nil
}

// declaredTestName reads the first top-level test function a Go test source declares, the way the product reads it.
func declaredTestName(source string) string {
	for _, line := range strings.Split(source, "\n") {
		trimmed := strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(trimmed, "func Test")
		if !ok {
			continue
		}
		name, _, ok := strings.Cut(rest, "(")
		if ok {
			return "Test" + name
		}
	}
	return ""
}

// trees is the SHA of each job, in order, deduplicated by first appearance.
func (s *fakeSandbox) trees() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, j := range s.jobs {
		if !slices.Contains(out, string(j.SHA)) {
			out = append(out, string(j.SHA))
		}
	}
	return out
}

// --- the fake code reader ---

// fakeCode serves the base version of every file the run reads, and a tree hash that is the commit itself.
type fakeCode struct{ base map[string][]byte }

var _ ports.CodeReader = (*fakeCode)(nil)

func (c *fakeCode) ReadFile(_ context.Context, _ change.Repo, sha id.SHA, path string) ([]byte, error) {
	if sha != proofBase {
		return nil, ports.ErrNotFound
	}
	b, ok := c.base[path]
	if !ok {
		return nil, ports.ErrNotFound
	}
	return b, nil
}

func (c *fakeCode) TreeHash(_ context.Context, _ change.Repo, sha id.SHA, _ string) (id.SHA, error) {
	return sha, nil
}

func (c *fakeCode) Tarball(context.Context, change.Repo, id.SHA) (io.ReadCloser, error) {
	return nil, ports.ErrNotFound
}

// --- the harness ---

// proofHarness wires one proof run and its fakes.
type proofHarness struct {
	model   *scriptedModel
	sandbox *fakeSandbox
	code    *fakeCode
	dir     string
	clock   evidenceClock
	pol     policy.Policy
	in      action.ProofIn
}

// newProofHarness wires the proof with a head checkout holding one source file, and a base with other text on its lines.
func newProofHarness(t *testing.T, steps []modelStep) *proofHarness {
	t.Helper()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		sourcePath: "package pay\n\nfunc RefundTotal(total, fee int) int {\n\treturn total\n}\n",
	})
	raw, err := os.ReadFile(policyFixture)
	require.NoError(t, err)
	pol, err := policyyaml.Decode(raw)
	require.NoError(t, err)
	pol.Blocking = policy.Blocking{MinSeverity: finding.SeverityMedium, MinTier: finding.TierE3}
	require.NoError(t, pol.Validate())
	h := &proofHarness{model: &scriptedModel{t: t, steps: steps}, sandbox: newFakeSandbox(),
		code:  &fakeCode{base: map[string][]byte{sourcePath: []byte("package pay\n\nfunc RefundTotal(total, fee int) int {\n\treturn total - fee\n}\n")}},
		dir:   dir,
		clock: evidenceClock{now: time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)}, pol: pol}
	h.in = action.ProofIn{ChangeID: proofChangeID(t), Repo: proofRepo, Number: proofNum, Head: proofHead, Base: proofBase,
		PolicyHash: proofHash, WorkflowRun: proofRunID, Action: pol.Action, Policy: pol, Checkout: dir,
		Files: []ports.ChangedFile{{Path: sourcePath, Status: ports.FileModified, Additions: 4, Deletions: 1}}}
	return h
}

// proofChangeID is the change id of the proof's pull request.
func proofChangeID(t *testing.T) id.ChangeID {
	t.Helper()
	c, err := id.PRChangeID(string(proofRepo), proofNum)
	require.NoError(t, err)
	return c
}

// writeTree writes text files under dir.
func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for p, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o750))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o600))
	}
}

// evidenceClock is a fixed clock. The proof run reads it for the loop's deadline.
type evidenceClock struct{ now time.Time }

func (c evidenceClock) Now() time.Time { return c.now }

// loopFor is the product's agent loop over the scripted model, with the given price.
func (h *proofHarness) loopFor(t *testing.T, price ports.ModelPrice) ports.AgentLoop {
	t.Helper()
	l, err := agentloop.NewRunner(h.model, h.clock, price)
	require.NoError(t, err)
	return l
}

// config is the proof wiring of the harness over the given loop and sandbox.
func (h *proofHarness) config(loop ports.AgentLoop, sandbox action.OverlaySandbox) action.ProofConfig {
	return action.ProofConfig{Loop: loop, Sandbox: sandbox, Code: h.code, Clock: h.clock, ModelName: "deepseek/deepseek-v4-flash"}
}

// run executes one proof run with the product's loop at the given price.
func (h *proofHarness) run(t *testing.T, price ports.ModelPrice) (action.ProofResult, error) {
	t.Helper()
	p, err := action.NewProof(h.config(h.loopFor(t, price), h.sandbox))
	require.NoError(t, err)
	return p.Run(t.Context(), h.in)
}

// cheapPrice is a price low enough that a few answers stay far under the default per-PR cap.
func cheapPrice() ports.ModelPrice {
	return ports.ModelPrice{InputPerM: 0.09, CachedInputPerM: 0.018, OutputPerM: 0.18}
}

// proposeArgs is one proposal of the bug on sourcePath.
func proposeArgs() map[string]any {
	return map[string]any{"file": sourcePath, "start_line": 4, "end_line": 4, "severity": "high",
		"claim":     "the refund subtracts nothing, so the caller pays the full total",
		"test_path": proofTest, "test_name": testName,
		"test_source": "package pay\n\nimport \"testing\"\n\nfunc " + testName + "(t *testing.T) {\n\tif RefundTotal(100, 10) != 90 {\n\t\tt.Fatalf(\"got %d\", RefundTotal(100, 10))\n\t}\n}\n"}
}

// --- the cases ---

// TestAProvenBugInEnforceIsBlocked is the whole run: the model reads the change, proposes one test on the new line, the
// sandbox fails it twice on the head and passes it on the reverted tree, and the run is blocked because the finding is
// high severity at or above the policy's minimum.
func TestAProvenBugInEnforceIsBlocked(t *testing.T) {
	h := newProofHarness(t, []modelStep{
		toolCall("c1", "read_file", map[string]any{"path": sourcePath}, 0.002),
		toolCall("c2", "propose_finding", proposeArgs(), 0.002),
		toolCall("c3", "finish_review", map[string]any{"summary": "one bug"}, 0.002),
	})
	h.sandbox.set(string(proofHead), 0, 1)
	h.sandbox.set(string(proofBase), 1, 0)

	res, err := h.run(t, cheapPrice())
	require.NoError(t, err)

	require.Equal(t, proof.KindBlocked, res.Outcome.Kind, res.StopDetail)
	require.Equal(t, 1, res.Outcome.Counts.Proven)
	require.Equal(t, 1, res.Outcome.Counts.Blocking)
	require.Len(t, res.Candidates, 1)
	require.Equal(t, proof.ClassProven, res.Candidates[0].Class)
	require.True(t, res.Candidates[0].Blocks)
	// Two head runs and one reverted run, all through the port.
	require.Equal(t, []string{string(proofHead), string(proofBase)}, h.sandbox.trees())
	require.Len(t, h.sandbox.jobs, 3, "two red runs on the head and one on the fixed tree")
	require.Greater(t, res.Spend.USD, 0.0, "a run that called the model costs money")
	require.NotEmpty(t, res.Artifact)

	a, perr := action.ParseProofArtifact(res.Artifact, action.ArtifactKey{Head: proofHead, Base: proofBase,
		PolicyHash: proofHash, WorkflowRun: proofRunID})
	require.NoError(t, perr)
	require.Equal(t, proof.KindBlocked, a.Kind)
	require.Len(t, a.Candidates, 1)
	require.True(t, a.Candidates[0].Blocking)
}

// TestACleanChangeProvesNothing makes the sandbox pass the proposed test on the head. The run is not reproduced, which is
// unknown and never a pass, and the candidate is dropped with no proof.
func TestACleanChangeProvesNothing(t *testing.T) {
	h := newProofHarness(t, []modelStep{
		toolCall("c1", "list_changes", map[string]any{}, 0.001),
		toolCall("c2", "propose_finding", proposeArgs(), 0.001),
		toolCall("c3", "finish_review", map[string]any{"summary": "clean"}, 0.001),
	})
	h.sandbox.set(string(proofHead), 1, 0)

	res, err := h.run(t, cheapPrice())
	require.NoError(t, err)

	require.Equal(t, proof.KindNotReproduced, res.Outcome.Kind)
	require.Equal(t, 0, res.Outcome.Counts.Proven)
	require.Equal(t, 1, res.Outcome.Counts.NoProof)
	require.Len(t, res.Candidates, 1)
	require.Equal(t, proof.ClassDropped, res.Candidates[0].Class)
	require.Equal(t, proof.DropNoProof, res.Candidates[0].Dropped)
	require.Len(t, h.sandbox.jobs, 1, "a test that passes on the head is not run on the fixed tree")
}

// TestATrivialDocsChangeCostsNothing proves the $0 skip: a change of one markdown file makes no model call and no sandbox
// run, and the result is a skipped run at $0.
func TestATrivialDocsChangeCostsNothing(t *testing.T) {
	h := newProofHarness(t, nil)
	h.in.Files = []ports.ChangedFile{{Path: "docs/refunds.md", Status: ports.FileAdded, Additions: 40}}

	res, err := h.run(t, cheapPrice())
	require.NoError(t, err)

	require.Equal(t, proof.KindSkippedTrivial, res.Outcome.Kind)
	require.True(t, res.Trivial.Skip)
	require.InDelta(t, 0.0, res.Spend.USD, 1e-9)
	require.InDelta(t, 0.0, res.Outcome.Cost.USD, 1e-9)
	require.Empty(t, h.model.requests, "a trivial change makes no model call")
	require.Empty(t, h.sandbox.jobs, "a trivial change runs no PR code")
	a, perr := action.ParseProofArtifact(res.Artifact, action.ArtifactKey{Head: proofHead, Base: proofBase,
		PolicyHash: proofHash, WorkflowRun: proofRunID})
	require.NoError(t, perr)
	require.Equal(t, change.Cost{}, a.Cost, "a skipped run costs nothing")
}

// TestTheBudgetStopsTheRunBeforeTheCallThatWouldPassTheCap uses a price high enough that the first answer could pass the
// per-PR cap. The run must make no call and end unknown for the reason budget.
func TestTheBudgetStopsTheRunBeforeTheCallThatWouldPassTheCap(t *testing.T) {
	h := newProofHarness(t, []modelStep{toolCall("c1", "finish_review", map[string]any{}, 0.001)})

	res, err := h.run(t, ports.ModelPrice{InputPerM: 5000, CachedInputPerM: 5000, OutputPerM: 5000})
	require.NoError(t, err)

	require.Equal(t, proof.KindUnknown, res.Outcome.Kind)
	require.Equal(t, proof.ReasonBudget, res.Outcome.Reason)
	require.Empty(t, h.model.requests, "no call may start that could pass the cap")
	require.InDelta(t, 0.0, res.Spend.USD, 1e-9)
}

// TestACodeRabbitClaimThatFailsToReproduceIsCountedUnproven feeds one third-party claim and a model that answers it with
// nothing. The claim is dropped with no proof and counted, and it never blocks.
func TestACodeRabbitClaimThatFailsToReproduceIsCountedUnproven(t *testing.T) {
	h := newProofHarness(t, []modelStep{
		toolCall("c1", "finish_review", map[string]any{"summary": "the claim does not reproduce"}, 0.001),
	})
	h.in.Claims = []action.Claim{{Source: external.ToolCodeRabbit, CommentID: 90125,
		Location: finding.Location{File: sourcePath, Lines: [2]int{4, 4}},
		Claim:    "the refund adds the fee a second time", Severity: finding.SeverityHigh}}

	res, err := h.run(t, cheapPrice())
	require.NoError(t, err)

	require.Equal(t, proof.KindNotReproduced, res.Outcome.Kind)
	require.Equal(t, 1, res.Outcome.Counts.In)
	require.Equal(t, 1, res.Outcome.Counts.NoProof)
	require.Equal(t, 0, res.Outcome.Counts.Blocking)
	require.Len(t, res.Candidates, 1)
	require.Equal(t, int64(90125), res.Candidates[0].CommentID)
	require.Equal(t, proof.ClassDropped, res.Candidates[0].Class)
	require.Equal(t, proof.DropNoProof, res.Candidates[0].Dropped)
	require.False(t, res.Candidates[0].Blocks)
}

// TestAToolAskedForAFileOutsideTheCheckoutIsRefused gives the model a path that climbs out of the tree. The tool must
// refuse it, the refusal goes back to the model, and no file outside the checkout is read.
func TestAToolAskedForAFileOutsideTheCheckoutIsRefused(t *testing.T) {
	for _, p := range []string{"../../etc/passwd", "/etc/passwd", ".git/config", "../secret.txt", "a/../../secret.txt"} {
		h := newProofHarness(t, []modelStep{
			toolCall("c1", "read_file", map[string]any{"path": p}, 0.001),
			toolCall("c2", "finish_review", map[string]any{}, 0.001),
		})
		_, err := h.run(t, cheapPrice())
		require.NoError(t, err)

		require.NotEmpty(t, h.model.requests, "the run went on after the refusal")
		// The refusal is a tool result. The loop turns an error into "error: ..." so the model can fix its path.
		refusal := h.model.requests[len(h.model.requests)-1].Messages
		require.True(t, hasToolError(refusal), "the refused path must come back as a tool error, not a file")
		for _, m := range refusal {
			require.NotContains(t, m.Content, "root:x:0:0")
		}
	}
}

// hasToolError reports whether one message of the conversation holds a tool error.
func hasToolError(msgs []ports.ModelMessage) bool {
	return slices.ContainsFunc(msgs, func(m ports.ModelMessage) bool {
		return m.Role == ports.ModelRoleTool && strings.HasPrefix(m.Content, "error:")
	})
}

// TestAProvenClaimOnlyBlocksWhenThePolicySeverityIsMet runs the same proven claim with the policy's blocking severity
// above the claim's severity: the finding is proven but a person decides, so the run is proven and not blocked.
func TestAProvenClaimOnlyBlocksWhenThePolicySeverityIsMet(t *testing.T) {
	h := newProofHarness(t, []modelStep{
		toolCall("c1", "propose_finding", proposeArgs(), 0.002),
		toolCall("c2", "finish_review", map[string]any{}, 0.002),
	})
	h.pol.Blocking = policy.Blocking{MinSeverity: finding.SeverityCritical, MinTier: finding.TierE3}
	require.NoError(t, h.pol.Validate())
	h.in.Policy, h.in.Action = h.pol, h.pol.Action
	h.sandbox.set(string(proofHead), 0, 1)
	h.sandbox.set(string(proofBase), 1, 0)

	res, err := h.run(t, cheapPrice())
	require.NoError(t, err)

	require.Equal(t, proof.KindProven, res.Outcome.Kind, res.StopDetail)
	require.Equal(t, 1, res.Outcome.Counts.Proven)
	require.Equal(t, 0, res.Outcome.Counts.Blocking)
	require.Equal(t, 1, res.Outcome.Counts.NeedsPerson)
}

// TestAFlakyHeadRunProvesNothing makes the two head runs disagree. Prove calls that flaky, the candidate is dropped, and
// the run never becomes blocked.
func TestAFlakyHeadRunProvesNothing(t *testing.T) {
	h := newProofHarness(t, []modelStep{
		toolCall("c1", "propose_finding", proposeArgs(), 0.002),
		toolCall("c2", "finish_review", map[string]any{}, 0.002),
	})
	h.sandbox.set(string(proofHead), 0, 1)
	flaky := &flakySandbox{fakeSandbox: h.sandbox, call: map[int]ports.SandboxResult{
		2: {Passed: 1, Tests: []ports.TestResult{{Name: testName, Outcome: ports.OutcomePassed}}},
	}}
	p, err := action.NewProof(h.config(h.loopFor(t, cheapPrice()), flaky))
	require.NoError(t, err)
	res, err := p.Run(t.Context(), h.in)
	require.NoError(t, err)

	require.NotEqual(t, proof.KindBlocked, res.Outcome.Kind)
	require.Equal(t, 0, res.Outcome.Counts.Proven)
	require.Equal(t, 1, res.Outcome.Counts.NoProof)
	require.Len(t, res.Candidates, 1)
	require.Equal(t, proof.ClassDropped, res.Candidates[0].Class)
}

// flakySandbox answers one call with a different result, so the two head runs disagree.
type flakySandbox struct {
	*fakeSandbox
	mu   sync.Mutex
	n    int
	call map[int]ports.SandboxResult
}

func (s *flakySandbox) RunOverlay(ctx context.Context, j ports.SandboxJob, overlays []ports.File) (ports.SandboxResult, error) {
	s.mu.Lock()
	s.n++
	n := s.n
	res, ok := s.call[n]
	s.mu.Unlock()
	if ok {
		return res, nil
	}
	return s.fakeSandbox.RunOverlay(ctx, j, overlays)
}

// TestTheArtifactNamesTheRunAndRefusesAnother is the gate-side check: a correct artifact parses, and one for another
// head, base, policy hash or run does not.
func TestTheArtifactNamesTheRunAndRefusesAnother(t *testing.T) {
	h := newProofHarness(t, nil)
	h.in.Files = []ports.ChangedFile{{Path: "docs/refunds.md", Status: ports.FileAdded, Additions: 5}}
	res, err := h.run(t, cheapPrice())
	require.NoError(t, err)
	want := action.ArtifactKey{Head: proofHead, Base: proofBase, PolicyHash: proofHash, WorkflowRun: proofRunID}

	_, perr := action.ParseProofArtifact(res.Artifact, want)
	require.NoError(t, perr)

	other := id.SHA("6666666666666666666666666666666666666666")
	cases := map[string]action.ArtifactKey{
		"another head":        {Head: other, Base: proofBase, PolicyHash: proofHash, WorkflowRun: proofRunID},
		"another base":        {Head: proofHead, Base: other, PolicyHash: proofHash, WorkflowRun: proofRunID},
		"another policy hash": {Head: proofHead, Base: proofBase, PolicyHash: strings.Repeat("a", 64), WorkflowRun: proofRunID},
		"another run":         {Head: proofHead, Base: proofBase, PolicyHash: proofHash, WorkflowRun: proofRunID + 1},
	}
	for name, key := range cases {
		_, aerr := action.ParseProofArtifact(res.Artifact, key)
		require.ErrorIs(t, aerr, action.ErrProofArtifact, name)
		require.Equal(t, proof.KindUnknown, action.ArtifactOutcome(res.Artifact, key).Kind, name)
	}
	require.Equal(t, proof.KindSkippedTrivial, action.ArtifactOutcome(res.Artifact, want).Kind)
}

// TestASandboxFailureMakesTheRunUnknownNeverPass makes the sandbox fail outright. The run is unknown for the sandbox
// reason and the candidate is dropped with no proof.
func TestASandboxFailureMakesTheRunUnknownNeverPass(t *testing.T) {
	h := newProofHarness(t, []modelStep{
		toolCall("c1", "propose_finding", proposeArgs(), 0.002),
		toolCall("c2", "finish_review", map[string]any{}, 0.002),
	})
	h.sandbox.fail = errors.New("sandbox: no such image")

	res, err := h.run(t, cheapPrice())
	require.NoError(t, err)

	require.Equal(t, proof.KindUnknown, res.Outcome.Kind)
	require.Equal(t, proof.ReasonSandbox, res.Outcome.Reason)
	require.Equal(t, 0, res.Outcome.Counts.Proven)
}

// TestTheMonthlyCapStopsARunBeforeItStarts passes a month spend that leaves no room for one PR. The run makes no call and
// is unknown for the monthly cap.
func TestTheMonthlyCapStopsARunBeforeItStarts(t *testing.T) {
	h := newProofHarness(t, nil)
	h.in.MonthKnown, h.in.MonthSpentUSD = true, 49.99

	res, err := h.run(t, cheapPrice())
	require.NoError(t, err)

	require.Equal(t, proof.KindUnknown, res.Outcome.Kind)
	require.Equal(t, proof.ReasonMonthlyCap, res.Outcome.Reason)
	require.Empty(t, h.model.requests)
}

// TestNewProofNeedsTheLoopAndTheSandbox refuses a wiring that could run PR code unsandboxed, or run with no loop.
func TestNewProofNeedsTheLoopAndTheSandbox(t *testing.T) {
	h := newProofHarness(t, nil)
	base := h.config(h.loopFor(t, cheapPrice()), h.sandbox)
	_, err := action.NewProof(base)
	require.NoError(t, err)
	for name, mutate := range map[string]func(*action.ProofConfig){
		"no loop":     func(c *action.ProofConfig) { c.Loop = nil },
		"no sandbox":  func(c *action.ProofConfig) { c.Sandbox = nil },
		"no code":     func(c *action.ProofConfig) { c.Code = nil },
		"no clock":    func(c *action.ProofConfig) { c.Clock = nil },
		"no model id": func(c *action.ProofConfig) { c.ModelName = "" },
	} {
		cfg := base
		mutate(&cfg)
		_, err := action.NewProof(cfg)
		require.Error(t, err, name)
	}
	// A loop with no price cannot be built at all, so a proof run never bills blind.
	_, err = agentloop.NewRunner(h.model, h.clock, ports.ModelPrice{})
	require.Error(t, err)
}
