package agentloop

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// scriptedModel answers each call with the next step. A step may run a hook first, to move the fake clock or
// cancel the context. It fails the test when the loop asks for more steps than the script holds.
type scriptedModel struct {
	t        *testing.T
	steps    []modelStep
	requests []ports.ModelRequest
}

type modelStep struct {
	resp ports.ModelResponse
	err  error
	hook func()
}

func (m *scriptedModel) Complete(ctx context.Context, req ports.ModelRequest) (ports.ModelResponse, error) {
	m.requests = append(m.requests, req)
	if len(m.requests) > len(m.steps) {
		m.t.Fatalf("model called %d times, script has %d steps", len(m.requests), len(m.steps))
	}
	step := m.steps[len(m.requests)-1]
	if step.hook != nil {
		step.hook()
	}
	if err := ctx.Err(); err != nil {
		return ports.ModelResponse{}, err
	}
	return step.resp, step.err
}

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

type fakeTool struct {
	name string
	run  func(args json.RawMessage) (string, error)
	got  []string
}

func (f *fakeTool) Spec() ports.ModelTool { return ports.ModelTool{Name: f.name} }
func (f *fakeTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	f.got = append(f.got, string(args))
	return f.run(args)
}

type fakeSubmitter struct {
	name     string
	verdicts []Verdict
	err      error
	got      []string
}

func (f *fakeSubmitter) Spec() ports.ModelTool { return ports.ModelTool{Name: f.name} }
func (f *fakeSubmitter) Submit(_ context.Context, args json.RawMessage) (Verdict, error) {
	f.got = append(f.got, string(args))
	if f.err != nil {
		return Verdict{}, f.err
	}
	v := f.verdicts[0]
	if len(f.verdicts) > 1 {
		f.verdicts = f.verdicts[1:]
	}
	return v, nil
}

func call(id, name, args string) ports.ModelToolCall {
	return ports.ModelToolCall{ID: id, Name: name, Arguments: args}
}

func answer(cost float64, calls ...ports.ModelToolCall) modelStep {
	return modelStep{resp: ports.ModelResponse{
		ToolCalls: calls, Finish: ports.ModelFinishToolCalls, CostUSD: cost,
		Usage: ports.ModelUsage{InputTokens: 100, CachedInputTokens: 40, OutputTokens: 10},
	}}
}

type harness struct {
	model *scriptedModel
	clock *fakeClock
	read  *fakeTool
	sub   *fakeSubmitter
	cfg   Config
}

func newHarness(t *testing.T, steps ...modelStep) *harness {
	t.Helper()
	clock := &fakeClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	return &harness{
		model: &scriptedModel{t: t, steps: steps},
		clock: clock,
		read:  &fakeTool{name: "read_code", run: func(json.RawMessage) (string, error) { return "package x", nil }},
		sub:   &fakeSubmitter{name: "submit_plan", verdicts: []Verdict{{Accepted: true}}},
		cfg: Config{
			Model: "m", MaxOutputTokens: 4000, BudgetUSD: 0.50, InputTokenCap: 800_000, OutputTokenCap: 40_000,
			Price:    ports.ModelPrice{InputPerM: 0.09, CachedInputPerM: 0.018, OutputPerM: 0.18},
			Deadline: clock.now.Add(15 * time.Minute), MaxTurns: 10,
		},
	}
}

func (h *harness) run(ctx context.Context, t *testing.T) (Result, error) {
	t.Helper()
	l, err := New(h.model, h.clock, h.cfg, []Tool{h.read}, []Submitter{h.sub})
	require.NoError(t, err)
	return l.Run(ctx, []ports.ModelMessage{{Role: ports.ModelRoleUser, Content: "go"}})
}

func toolTexts(res Result) []string {
	var out []string
	for _, m := range res.Transcript {
		if m.Role == ports.ModelRoleTool {
			out = append(out, m.Content)
		}
	}
	return out
}

func TestToolDispatchThenAcceptedSubmission(t *testing.T) {
	h := newHarness(t,
		answer(0.01, call("c1", "read_code", `{"path":"a.go"}`)),
		answer(0.02, call("c2", "submit_plan", `{"plan":"p"}`)),
	)
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, StopSubmitted, res.Reason)
	assert.Equal(t, 2, res.Turns)
	assert.InDelta(t, 0.03, res.CostUSD, 1e-9)
	assert.Equal(t, &Submission{Tool: "submit_plan", Arguments: `{"plan":"p"}`}, res.Submission)
	assert.Equal(t, []string{`{"path":"a.go"}`}, h.read.got)
	assert.Equal(t, []string{"package x", "accepted"}, toolTexts(res))
	assert.Equal(t, int64(200), res.Usage.InputTokens)
	assert.Equal(t, int64(80), res.Usage.CachedInputTokens)
	assert.Equal(t, int64(20), res.Usage.OutputTokens)
	// The model sees both tools, in registry order, and the configured limits.
	req := h.model.requests[0]
	assert.Equal(t, []string{"read_code", "submit_plan"}, []string{req.Tools[0].Name, req.Tools[1].Name})
	assert.Equal(t, 4000, req.MaxOutputTokens)
	// The second request carries the first answer and its tool result.
	second := h.model.requests[1].Messages
	require.Len(t, second, 3)
	assert.Equal(t, ports.ModelRoleTool, second[2].Role)
	assert.Equal(t, "c1", second[2].ToolCallID)
}

func TestRefusedSubmissionGetsOneRetry(t *testing.T) {
	h := newHarness(t,
		answer(0.01, call("c1", "submit_plan", `{"plan":""}`)),
		answer(0.01, call("c2", "submit_plan", `{"plan":"better"}`)),
	)
	h.sub.verdicts = []Verdict{{Reason: "plan.cases: at least one case"}, {Accepted: true}}
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, StopSubmitted, res.Reason)
	assert.Equal(t, 1, res.Refusals)
	assert.Equal(t, []string{"refused: plan.cases: at least one case", "accepted"}, toolTexts(res))
}

func TestSecondRefusalStopsTheRun(t *testing.T) {
	h := newHarness(t,
		answer(0.01, call("c1", "submit_plan", `{}`)),
		answer(0.01, call("c2", "submit_plan", `{}`)),
	)
	h.sub.verdicts = []Verdict{{Reason: "first"}, {Reason: "second"}}
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, StopRefused, res.Reason)
	assert.Equal(t, 2, res.Refusals)
	assert.Equal(t, 2, res.Turns)
	assert.Nil(t, res.Submission)
	assert.Len(t, h.sub.got, 2)
}

func TestMalformedSubmissionArgumentsCountAsRefusal(t *testing.T) {
	h := newHarness(t,
		answer(0.01, call("c1", "submit_plan", `not json`)),
		answer(0.01, call("c2", "submit_plan", `{"plan":"p"}`)),
	)
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, StopSubmitted, res.Reason)
	assert.Equal(t, 1, res.Refusals)
	assert.Equal(t, []string{`{"plan":"p"}`}, h.sub.got, "the validator never sees the malformed call")
}

func TestSubmitterFailureIsAToolErrorNotARefusal(t *testing.T) {
	h := newHarness(t,
		answer(0.01, call("c1", "submit_plan", `{}`)),
		answer(0.01, call("c2", "submit_plan", `{}`)),
		answer(0.01, call("c3", "submit_plan", `{}`)),
	)
	h.sub.err = errors.New("validator down")
	h.cfg.MaxTurns = 3
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, StopTurnCap, res.Reason)
	assert.Zero(t, res.Refusals)
	assert.Equal(t, "error: validator down", toolTexts(res)[0])
}

func TestBudgetStopsBeforeTheNextCall(t *testing.T) {
	h := newHarness(t,
		answer(0.30, call("c1", "read_code", `{}`)),
		answer(0.25, call("c2", "read_code", `{}`)),
	)
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, StopBudget, res.Reason)
	assert.Equal(t, ports.AgentBudgetUSD, res.BudgetCause)
	assert.Equal(t, 2, res.Turns)
	assert.InDelta(t, 0.55, res.CostUSD, 1e-9)
	assert.Len(t, h.model.requests, 2, "no third call after the budget is reached")
}

// ACTION_MODE section 7: the per-PR token cap stops the run like the cost budget, and the guard sizes the worst case
// of the next call. Each scripted answer reads 100 input tokens (cached ones included) and writes 10. The first request
// is about 150 bytes, so a call needs a little over 1,100 input tokens of room (the request plus the framing allowance).
func TestInputTokenCapStopsBeforeTheCallThatCouldPassIt(t *testing.T) {
	h := newHarness(t,
		answer(0.01, call("c1", "read_code", `{}`)),
		answer(0.01, call("c2", "read_code", `{}`)),
	)
	h.cfg.InputTokenCap = 1300
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, StopBudget, res.Reason)
	assert.Equal(t, ports.AgentBudgetInputTokens, res.BudgetCause)
	assert.Equal(t, int64(100), res.Usage.InputTokens)
	assert.Len(t, h.model.requests, 1, "the second call could send more than the cap, so it is not made")
}

func TestOutputTokenCapStopsBeforeTheCallThatCouldPassIt(t *testing.T) {
	h := newHarness(t,
		answer(0.01, call("c1", "read_code", `{}`)),
		answer(0.01, call("c2", "read_code", `{}`)),
	)
	h.cfg.OutputTokenCap = int64(h.cfg.MaxOutputTokens) + 5
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, StopBudget, res.Reason)
	assert.Equal(t, ports.AgentBudgetOutputTokens, res.BudgetCause)
	assert.Equal(t, int64(10), res.Usage.OutputTokens)
	assert.Len(t, h.model.requests, 1, "a full answer after the first one could pass the cap, so the call is not made")
}

// The worst case of a call is its own size and a full answer, whatever the model would have written. A cap below the
// worst case of the first call stops the run before any call.
func TestACapBelowTheWorstCaseOfTheFirstCallMakesNoCall(t *testing.T) {
	for name, tt := range map[string]struct {
		mutate func(*Config)
		cause  ports.AgentBudgetCause
	}{
		"input":  {func(c *Config) { c.InputTokenCap = 900 }, ports.AgentBudgetInputTokens},
		"output": {func(c *Config) { c.OutputTokenCap = int64(c.MaxOutputTokens) - 1 }, ports.AgentBudgetOutputTokens},
		"cost":   {func(c *Config) { c.Price = ports.ModelPrice{InputPerM: 5000, CachedInputPerM: 5000, OutputPerM: 5000} }, ports.AgentBudgetUSD},
	} {
		h := newHarness(t, answer(0.001, call("s1", "submit_plan", `{}`)))
		tt.mutate(&h.cfg)
		res, err := h.run(context.Background(), t)
		require.NoError(t, err, name)
		assert.Equal(t, StopBudget, res.Reason, name)
		assert.Equal(t, tt.cause, res.BudgetCause, name)
		assert.Empty(t, h.model.requests, name)
		assert.Zero(t, res.Turns, name)
	}
}

// A request that cannot be encoded has no size, so the run refuses to send it. The caller must see why: an unsized
// call is a different problem from a cap that was actually reached.
func TestAnUnsizableRequestStopsWithTheRequestSizeCause(t *testing.T) {
	h := newHarness(t, modelStep{resp: ports.ModelResponse{
		ToolCalls: []ports.ModelToolCall{call("c1", "read_code", `{}`)},
		Finish:    ports.ModelFinishToolCalls,
		Reasoning: ports.ModelReasoning{Text: "thought", Details: json.RawMessage("{")},
		Usage:     ports.ModelUsage{InputTokens: 100, OutputTokens: 10}, CostUSD: 0.01,
	}})
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, StopBudget, res.Reason)
	assert.Equal(t, ports.AgentBudgetRequestSize, res.BudgetCause)
	assert.Len(t, h.model.requests, 1, "the next request cannot be sized, so it is not sent")
}

// A cost or a token count that cannot be true ends the run: a missing measure must never look like a cheap one.
func TestAUsageThatCannotBeTrueStopsTheRun(t *testing.T) {
	bad := []ports.ModelResponse{
		{CostUSD: -0.01, ToolCalls: []ports.ModelToolCall{call("c1", "read_code", `{}`)}},
		{CostUSD: math.NaN(), ToolCalls: []ports.ModelToolCall{call("c1", "read_code", `{}`)}},
		{Usage: ports.ModelUsage{OutputTokens: -1}, ToolCalls: []ports.ModelToolCall{call("c1", "read_code", `{}`)}},
	}
	for _, resp := range bad {
		h := newHarness(t, modelStep{resp: resp}, answer(0.01, call("s1", "submit_plan", `{}`)))
		res, err := h.run(context.Background(), t)
		require.ErrorIs(t, err, ports.ErrAgentUsage)
		assert.Equal(t, StopBudget, res.Reason)
		assert.Equal(t, ports.AgentBudgetUsage, res.BudgetCause)
		assert.Len(t, h.model.requests, 1)
		assert.Zero(t, res.Turns, "an answer that cannot be measured is not counted")
	}
}

// Each answer carries its usage, cost and latency, and the tool calls that followed carry theirs.
func TestEachTurnReportsUsageCostAndLatency(t *testing.T) {
	h := newHarness(t,
		answer(0.01, call("c1", "read_code", `{}`)),
		answer(0.02, call("s1", "submit_plan", `{}`)),
	)
	h.model.steps[0].hook = func() { h.clock.now = h.clock.now.Add(2 * time.Second) }
	h.read.run = func(json.RawMessage) (string, error) {
		h.clock.now = h.clock.now.Add(3 * time.Second)
		return "ok", nil
	}
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	require.Len(t, res.Turn, 2)
	assert.Equal(t, 2*time.Second, res.Turn[0].ModelLatency)
	assert.Equal(t, 3*time.Second, res.Turn[0].ToolLatency)
	assert.Equal(t, 1, res.Turn[0].ToolCalls)
	assert.InDelta(t, 0.01, res.Turn[0].CostUSD, 1e-12)
	assert.Equal(t, ports.ModelUsage{InputTokens: 100, CachedInputTokens: 40, OutputTokens: 10}, res.Turn[0].Usage)
	assert.Zero(t, res.Turn[1].ModelLatency)
	assert.InDelta(t, 0.02, res.Turn[1].CostUSD, 1e-12)
}

func TestEachCallCarriesTheOutputCap(t *testing.T) {
	h := newHarness(t, answer(0.01, call("s1", "submit_plan", `{}`)))
	_, err := h.run(context.Background(), t)
	require.NoError(t, err)
	require.Len(t, h.model.requests, 1)
	assert.Equal(t, 4000, h.model.requests[0].MaxOutputTokens)
}

func TestDeadlineStopsBeforeTheNextCall(t *testing.T) {
	h := newHarness(t,
		answer(0.01, call("c1", "read_code", `{}`)),
		answer(0.01, call("c2", "read_code", `{}`)),
	)
	h.read.run = func(json.RawMessage) (string, error) {
		h.clock.now = h.clock.now.Add(16 * time.Minute)
		return "slow", nil
	}
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, StopDeadline, res.Reason)
	assert.Equal(t, 1, res.Turns)
	assert.Len(t, h.model.requests, 1)
}

func TestDeadlineStopsBetweenToolCallsOfOneAnswer(t *testing.T) {
	h := newHarness(t, answer(0.01, call("c1", "read_code", `{}`), call("c2", "read_code", `{}`)))
	h.read.run = func(json.RawMessage) (string, error) {
		h.clock.now = h.clock.now.Add(16 * time.Minute)
		return "slow", nil
	}
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, StopDeadline, res.Reason)
	assert.Len(t, h.read.got, 1, "the second call is not run")
}

func TestDeadlineAlreadyPassedMakesNoCall(t *testing.T) {
	h := newHarness(t)
	h.clock.now = h.cfg.Deadline
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, StopDeadline, res.Reason)
	assert.Empty(t, h.model.requests)
}

func TestTurnCapStopsTheRun(t *testing.T) {
	h := newHarness(t,
		answer(0.01, call("c1", "read_code", `{}`)),
		answer(0.01, call("c2", "read_code", `{}`)),
	)
	h.cfg.MaxTurns = 2
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, StopTurnCap, res.Reason)
	assert.Equal(t, 2, res.Turns)
}

func TestUnknownToolAndBadArgumentsAndToolErrorReachTheModel(t *testing.T) {
	h := newHarness(t,
		answer(0.01, call("c1", "rm_rf", `{}`), call("c2", "read_code", `[1]`), call("c3", "read_code", `{"x":1}`)),
		answer(0.01, call("c4", "submit_plan", `{}`)),
	)
	h.read.run = func(json.RawMessage) (string, error) { return "", errors.New("no such file") }
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, StopSubmitted, res.Reason)
	assert.Equal(t, []string{
		`error: unknown tool "rm_rf"`,
		"error: tool arguments are not a JSON object",
		"error: no such file",
		"accepted",
	}, toolTexts(res))
	assert.Equal(t, []string{`{"x":1}`}, h.read.got, "the malformed call never reaches the tool")
}

func TestErrorsCountTowardTheTurnCap(t *testing.T) {
	h := newHarness(t,
		answer(0.01, call("c1", "rm_rf", `{}`)),
		answer(0.01, call("c2", "rm_rf", `{}`)),
	)
	h.cfg.MaxTurns = 2
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, StopTurnCap, res.Reason)
}

func TestTextOnlyAnswerEndsWithNoSubmissionAfterTheNudges(t *testing.T) {
	text := modelStep{resp: ports.ModelResponse{Content: "I am done", Finish: ports.ModelFinishStop}}
	h := newHarness(t, text, text, text)
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, StopNoSubmission, res.Reason)
	assert.Equal(t, "I am done", res.LastText)
	assert.Equal(t, 3, res.Turns, "two nudges, then the third text answer ends the run")
	assert.Equal(t, 2, res.Nudges)
}

func TestCancellationStopsTheRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	h := newHarness(t, answer(0.01, call("c1", "read_code", `{}`)))
	h.model.steps[0].hook = cancel
	res, err := h.run(ctx, t)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, StopCancelled, res.Reason)
	assert.Zero(t, res.Turns)
}

func TestCancellationBetweenToolCalls(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	h := newHarness(t, answer(0.01, call("c1", "read_code", `{}`), call("c2", "read_code", `{}`)))
	h.read.run = func(json.RawMessage) (string, error) { cancel(); return "ok", nil }
	res, err := h.run(ctx, t)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, StopCancelled, res.Reason)
	assert.Len(t, h.read.got, 1)
}

func TestModelErrorIsWrapped(t *testing.T) {
	h := newHarness(t, modelStep{err: &ports.ModelError{Kind: ports.ModelErrorServer, Status: 503}})
	res, err := h.run(context.Background(), t)
	require.ErrorIs(t, err, ports.ErrUnavailable)
	var me *ports.ModelError
	require.ErrorAs(t, err, &me)
	assert.Equal(t, StopModelError, res.Reason)
}

func TestNewRefusesABadRegistryOrConfig(t *testing.T) {
	h := newHarness(t)
	_, err := New(h.model, h.clock, h.cfg, []Tool{h.read, h.read}, []Submitter{h.sub})
	require.Error(t, err, "duplicate tool")
	_, err = New(h.model, h.clock, h.cfg, []Tool{&fakeTool{name: "submit_plan"}}, []Submitter{h.sub})
	require.Error(t, err, "tool and submitter share a name")
	_, err = New(h.model, h.clock, h.cfg, nil, nil)
	require.Error(t, err, "no submitter")
	for _, mutate := range []func(*Config){
		func(c *Config) { c.Model = "" },
		func(c *Config) { c.BudgetUSD = 0 },
		func(c *Config) { c.MaxTurns = 0 },
		func(c *Config) { c.Deadline = time.Time{} },
		func(c *Config) { c.MaxOutputTokens = 0 },
		func(c *Config) { c.InputTokenCap = 0 },
		func(c *Config) { c.OutputTokenCap = 0 },
		func(c *Config) { c.Price = ports.ModelPrice{} },
		func(c *Config) { c.Price.OutputPerM = 0 },
	} {
		cfg := h.cfg
		mutate(&cfg)
		_, err = New(h.model, h.clock, cfg, nil, []Submitter{h.sub})
		require.Error(t, err)
	}
}
