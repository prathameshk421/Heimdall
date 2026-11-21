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

// Contract test of ports.AgentLoop: the checks any implementation must pass, run against Runner through the port only.
// The dispatcher is a fake the caller would write; the model is the scripted fake of loop_test.go.

// fakeDispatcher answers each tool by name. A final tool with a queue of refusals refuses that many times, then accepts.
type fakeDispatcher struct {
	calls    []string
	results  map[string]func(args json.RawMessage) (string, error)
	refusals map[string][]string
}

func (d *fakeDispatcher) Dispatch(_ context.Context, name string, args json.RawMessage) (string, error) {
	d.calls = append(d.calls, name+" "+string(args))
	if queue := d.refusals[name]; len(queue) > 0 {
		d.refusals[name] = queue[1:]
		return "", &ports.AgentRefusal{Reason: queue[0]}
	}
	if f, ok := d.results[name]; ok {
		return f(args)
	}
	return "ok", nil
}

type contract struct {
	model *scriptedModel
	clock *fakeClock
	disp  *fakeDispatcher
	run   ports.AgentRun
}

func newContract(t *testing.T, steps ...modelStep) *contract {
	t.Helper()
	clock := &fakeClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	return &contract{
		model: &scriptedModel{t: t, steps: steps},
		clock: clock,
		disp:  &fakeDispatcher{results: map[string]func(json.RawMessage) (string, error){}, refusals: map[string][]string{}},
		run: ports.AgentRun{
			Model: "m", System: "you review", User: "go",
			Tools:           []ports.ModelTool{{Name: "read_code"}, {Name: "finish"}},
			FinalTools:      []string{"finish"},
			Budget:          ports.AgentBudget{USD: 0.50, InputTokens: 800_000, OutputTokens: 40_000},
			MaxOutputTokens: 4000, MaxTurns: 10, Deadline: clock.now.Add(15 * time.Minute),
		},
	}
}

var testPrice = ports.ModelPrice{InputPerM: 0.09, CachedInputPerM: 0.018, OutputPerM: 0.18}

func (c *contract) do(ctx context.Context, t *testing.T) (ports.AgentResult, error) {
	t.Helper()
	r, err := NewRunner(c.model, c.clock, testPrice)
	require.NoError(t, err)
	var loop ports.AgentLoop = r
	c.run.Dispatcher = c.disp
	return loop.Run(ctx, c.run)
}

func TestContractAFinalToolEndsTheRunAsDone(t *testing.T) {
	c := newContract(t,
		answer(0.01, call("c1", "read_code", `{"path":"a.go"}`)),
		answer(0.02, call("c2", "finish", `{"summary":"s"}`)),
	)
	res, err := c.do(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, ports.AgentStopDone, res.Stop)
	require.NotNil(t, res.Submission)
	assert.Equal(t, ports.AgentSubmission{Tool: "finish", Arguments: `{"summary":"s"}`}, *res.Submission)
	assert.Equal(t, []string{`read_code {"path":"a.go"}`, `finish {"summary":"s"}`}, c.disp.calls)
	require.Len(t, res.Turns, 2)
	assert.InDelta(t, 0.03, res.CostUSD, 1e-12)
	assert.Equal(t, int64(200), res.Usage.InputTokens)
	// The system prompt and the first user message start the conversation.
	require.GreaterOrEqual(t, len(c.model.requests[0].Messages), 2)
	assert.Equal(t, ports.ModelRoleSystem, c.model.requests[0].Messages[0].Role)
	assert.Equal(t, "go", c.model.requests[0].Messages[1].Content)
	assert.Len(t, c.model.requests[0].Tools, 2)
	assert.Equal(t, 4000, c.model.requests[0].MaxOutputTokens)
}

func TestContractATextAnswerIsDoneWithNoSubmission(t *testing.T) {
	c := newContract(t, modelStep{resp: ports.ModelResponse{Content: "nothing to report", Finish: ports.ModelFinishStop,
		Usage: ports.ModelUsage{InputTokens: 10, OutputTokens: 5}, CostUSD: 0.001}})
	res, err := c.do(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, ports.AgentStopDone, res.Stop)
	assert.Nil(t, res.Submission)
	assert.Equal(t, "nothing to report", res.FinalText)
}

func TestContractARefusalGetsOneRetryThenEndsTheRun(t *testing.T) {
	c := newContract(t,
		answer(0.01, call("c1", "finish", `{}`)),
		answer(0.01, call("c2", "finish", `{}`)),
		answer(0.01, call("c3", "finish", `{}`)),
	)
	c.disp.refusals["finish"] = []string{"summary: required", "summary: still required"}
	res, err := c.do(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, ports.AgentStopRefused, res.Stop)
	assert.Equal(t, 2, res.Refusals)
	assert.Len(t, c.model.requests, 2, "one retry, then the run ends")
	assert.Equal(t, []string{"refused: summary: required", "refused: summary: still required"}, toolTexts(Result{Transcript: res.Transcript}))

	c = newContract(t, answer(0.01, call("c1", "finish", `{}`)), answer(0.01, call("c2", "finish", `{}`)))
	c.disp.refusals["finish"] = []string{"summary: required"}
	res, err = c.do(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, ports.AgentStopDone, res.Stop, "the retry was accepted")
	assert.Equal(t, 1, res.Refusals)
}

func TestContractADispatcherErrorGoesBackToTheModel(t *testing.T) {
	c := newContract(t,
		answer(0.01, call("c1", "read_code", `{}`)),
		answer(0.01, call("c2", "nope", `{}`)),
		answer(0.01, call("c3", "read_code", `not json`)),
		answer(0.01, call("c4", "finish", `{}`)),
	)
	c.disp.results["read_code"] = func(json.RawMessage) (string, error) { return "", errors.New("no such file") }
	res, err := c.do(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, ports.AgentStopDone, res.Stop)
	assert.Equal(t, []string{"error: no such file", `error: unknown tool "nope"`, "error: tool arguments are not a JSON object", "accepted"},
		toolTexts(Result{Transcript: res.Transcript}))
	assert.Equal(t, []string{"read_code {}", "finish {}"}, c.disp.calls, "an unknown tool and bad arguments never reach the dispatcher")
}

func TestContractAValidatorFailureIsNotARefusal(t *testing.T) {
	c := newContract(t, answer(0.01, call("c1", "finish", `{}`)), answer(0.01, call("c2", "finish", `{}`)))
	failed := false
	c.disp.results["finish"] = func(json.RawMessage) (string, error) {
		if !failed {
			failed = true
			return "", errors.New("validator down")
		}
		return "ok", nil
	}
	res, err := c.do(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, ports.AgentStopDone, res.Stop)
	assert.Zero(t, res.Refusals)
}

func TestContractTheCapsAreHeldBeforeEachCall(t *testing.T) {
	c := newContract(t, answer(0.30, call("c1", "read_code", `{}`)), answer(0.25, call("c2", "read_code", `{}`)))
	res, err := c.do(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, ports.AgentStopBudget, res.Stop)
	assert.Len(t, c.model.requests, 2)

	// A price high enough that the first call could pass the cap: no call is made at all.
	c = newContract(t, answer(0.001, call("c1", "finish", `{}`)))
	r, err := NewRunner(c.model, c.clock, ports.ModelPrice{InputPerM: 5000, CachedInputPerM: 5000, OutputPerM: 5000})
	require.NoError(t, err)
	c.run.Dispatcher = c.disp
	res, err = r.Run(context.Background(), c.run)
	require.NoError(t, err)
	assert.Equal(t, ports.AgentStopBudget, res.Stop)
	assert.Empty(t, c.model.requests)
}

func TestContractTheDeadlineTheTurnCapAndCancellationEndTheRun(t *testing.T) {
	c := newContract(t, answer(0.01, call("c1", "read_code", `{}`)))
	c.run.Deadline = c.clock.now
	res, err := c.do(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, ports.AgentStopDeadline, res.Stop)
	assert.Empty(t, c.model.requests)

	c = newContract(t, answer(0.01, call("c1", "read_code", `{}`)), answer(0.01, call("c2", "read_code", `{}`)))
	c.run.MaxTurns = 2
	res, err = c.do(context.Background(), t)
	require.NoError(t, err)
	assert.Equal(t, ports.AgentStopTurns, res.Stop)
	assert.Len(t, res.Turns, 2)

	ctx, cancel := context.WithCancel(context.Background())
	c = newContract(t, answer(0.01, call("c1", "read_code", `{}`)))
	c.model.steps[0].hook = cancel
	res, err = c.do(ctx, t)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, ports.AgentStopCancelled, res.Stop)
}

func TestContractAModelFailureIsAnErrorStopThatWrapsTheCause(t *testing.T) {
	c := newContract(t, modelStep{err: ports.ErrUnavailable})
	res, err := c.do(context.Background(), t)
	require.ErrorIs(t, err, ports.ErrUnavailable)
	assert.Equal(t, ports.AgentStopError, res.Stop)

	c = newContract(t, modelStep{resp: ports.ModelResponse{CostUSD: -1, ToolCalls: []ports.ModelToolCall{call("c1", "read_code", `{}`)}}})
	res, err = c.do(context.Background(), t)
	require.ErrorIs(t, err, ports.ErrAgentUsage)
	assert.Equal(t, ports.AgentStopBudget, res.Stop)
}

func TestContractTurnsCarryLatency(t *testing.T) {
	c := newContract(t, answer(0.01, call("c1", "read_code", `{}`)), answer(0.01, call("c2", "finish", `{}`)))
	c.model.steps[0].hook = func() { c.clock.now = c.clock.now.Add(2 * time.Second) }
	c.disp.results["read_code"] = func(json.RawMessage) (string, error) {
		c.clock.now = c.clock.now.Add(3 * time.Second)
		return "ok", nil
	}
	res, err := c.do(context.Background(), t)
	require.NoError(t, err)
	require.Len(t, res.Turns, 2)
	assert.Equal(t, 2*time.Second, res.Turns[0].ModelLatency)
	assert.Equal(t, 3*time.Second, res.Turns[0].ToolLatency)
}

func TestNewRunnerAndRunRefuseABadWiring(t *testing.T) {
	c := newContract(t)
	_, err := NewRunner(nil, c.clock, testPrice)
	require.Error(t, err)
	_, err = NewRunner(c.model, nil, testPrice)
	require.Error(t, err)
	_, err = NewRunner(c.model, c.clock, ports.ModelPrice{})
	require.Error(t, err, "no price: the cap would be blind")

	r, err := NewRunner(c.model, c.clock, testPrice)
	require.NoError(t, err)
	for name, mutate := range map[string]func(*ports.AgentRun){
		"no dispatcher":         func(q *ports.AgentRun) { q.Dispatcher = nil },
		"no final tool":         func(q *ports.AgentRun) { q.FinalTools = nil },
		"final tool not listed": func(q *ports.AgentRun) { q.FinalTools = []string{"other"} },
		"duplicate tool":        func(q *ports.AgentRun) { q.Tools = append(q.Tools, ports.ModelTool{Name: "read_code"}) },
		"no model":              func(q *ports.AgentRun) { q.Model = "" },
		"no budget":             func(q *ports.AgentRun) { q.Budget.USD = 0 },
		"no deadline":           func(q *ports.AgentRun) { q.Deadline = time.Time{} },
	} {
		req := c.run
		req.Dispatcher = c.disp
		mutate(&req)
		_, err := r.Run(context.Background(), req)
		require.Error(t, err, name)
	}
	assert.Empty(t, c.model.requests, "a miswired run makes no model call")
}
