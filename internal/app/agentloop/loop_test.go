package agentloop

import (
	"context"
	"encoding/json"
	"errors"
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
	assert.Equal(t, 2, res.Turns)
	assert.InDelta(t, 0.55, res.CostUSD, 1e-9)
	assert.Len(t, h.model.requests, 2, "no third call after the budget is reached")
}

// ACTION_MODE section 7: the per-PR token cap stops the run like the cost budget. Each scripted answer reads 100
// input tokens (cached ones included) and writes 10.
func TestInputTokenCapStopsBeforeTheNextCall(t *testing.T) {
	h := newHarness(t,
		answer(0.01, call("c1", "read_code", `{}`)),
		answer(0.01, call("c2", "read_code", `{}`)),
	)
	h.cfg.InputTokenCap = 150
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, StopBudget, res.Reason)
	assert.Equal(t, int64(200), res.Usage.InputTokens)
	assert.Len(t, h.model.requests, 2, "no third call after the input token cap is reached")
}

func TestOutputTokenCapStopsBeforeTheNextCall(t *testing.T) {
	h := newHarness(t,
		answer(0.01, call("c1", "read_code", `{}`)),
		answer(0.01, call("c2", "read_code", `{}`)),
	)
	h.cfg.OutputTokenCap = 15
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, StopBudget, res.Reason)
	assert.Equal(t, int64(20), res.Usage.OutputTokens)
	assert.Len(t, h.model.requests, 2, "no third call after the output token cap is reached")
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

func TestTextOnlyAnswerEndsWithNoSubmission(t *testing.T) {
	h := newHarness(t, modelStep{resp: ports.ModelResponse{Content: "I am done", Finish: ports.ModelFinishStop}})
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, StopNoSubmission, res.Reason)
	assert.Equal(t, "I am done", res.LastText)
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
	} {
		cfg := h.cfg
		mutate(&cfg)
		_, err = New(h.model, h.clock, cfg, nil, []Submitter{h.sub})
		require.Error(t, err)
	}
}
