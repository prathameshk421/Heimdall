package agentloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Runner implements ports.AgentLoop with Loop. It holds the model, the clock and the model's price, and nothing else:
// each Run call builds its own Loop from the request, so two runs share no state.
type Runner struct {
	model ports.ModelChat
	clock ports.Clock
	price ports.ModelPrice
	retry Retry
}

var _ ports.AgentLoop = (*Runner)(nil)

// NewRunner checks the wiring. A run with no price could not size a call before it, so a missing price fails here, at
// boot, and not after a model has been paid for.
func NewRunner(model ports.ModelChat, clock ports.Clock, price ports.ModelPrice) (*Runner, error) {
	switch {
	case model == nil:
		return nil, errors.New("agent loop runner needs a model")
	case clock == nil:
		return nil, errors.New("agent loop runner needs a clock")
	case !price.Valid():
		return nil, errors.New("agent loop runner needs a positive price for the model: without it the caps cannot see the spend")
	}
	return &Runner{model: model, clock: clock, price: price}, nil
}

// WithRetry returns a copy of the Runner that sends a model call again after a rate limit or a server error, up to 3
// attempts, waiting through wait with jitter from random (retry.go). A Runner without it ends the run on the first
// failed call.
func (r *Runner) WithRetry(wait ports.Waiter, random ports.Random) (*Runner, error) {
	if wait == nil || random == nil {
		return nil, errors.New("agent loop runner retry needs a waiter and a jitter source")
	}
	out := *r
	out.retry = Retry{Wait: wait, Random: random}
	return &out, nil
}

// Run builds a Loop from the request, runs it and maps its result to the port's.
func (r *Runner) Run(ctx context.Context, req ports.AgentRun) (ports.AgentResult, error) {
	if req.Dispatcher == nil {
		return ports.AgentResult{}, errors.New("agent loop run needs a tool dispatcher")
	}
	tools, finals, err := r.registry(req)
	if err != nil {
		return ports.AgentResult{}, err
	}
	l, err := New(r.model, r.clock, Config{Model: req.Model, MaxOutputTokens: req.MaxOutputTokens, BudgetUSD: req.Budget.USD,
		InputTokenCap: req.Budget.InputTokens, OutputTokenCap: req.Budget.OutputTokens, Price: r.price,
		Deadline: req.Deadline, MaxTurns: req.MaxTurns, RequireToolCall: req.RequireToolCall, Nudge: req.Nudge, Retry: r.retry}, tools, finals)
	if err != nil {
		return ports.AgentResult{}, err
	}
	messages := make([]ports.ModelMessage, 0, 2)
	if req.System != "" {
		messages = append(messages, ports.ModelMessage{Role: ports.ModelRoleSystem, Content: req.System})
	}
	messages = append(messages, ports.ModelMessage{Role: ports.ModelRoleUser, Content: req.User})
	res, runErr := l.Run(ctx, messages)
	return portResult(res), runErr
}

// registry splits the request's tools into plain tools and final ones, each backed by the dispatcher. A final name that
// is not among the tools is a miswired request.
func (r *Runner) registry(req ports.AgentRun) ([]Tool, []Submitter, error) {
	var tools []Tool
	var finals []Submitter
	seen := map[string]bool{}
	for _, spec := range req.Tools {
		seen[spec.Name] = true
		if slices.Contains(req.FinalTools, spec.Name) {
			finals = append(finals, dispatchFinal{spec: spec, d: req.Dispatcher})
			continue
		}
		tools = append(tools, dispatchTool{spec: spec, d: req.Dispatcher})
	}
	for _, name := range req.FinalTools {
		if !seen[name] {
			return nil, nil, fmt.Errorf("agent loop final tool %q is not one of the tools", name)
		}
	}
	return tools, finals, nil
}

// dispatchTool is a Tool whose work is the request's dispatcher.
type dispatchTool struct {
	spec ports.ModelTool
	d    ports.ToolDispatcher
}

func (t dispatchTool) Spec() ports.ModelTool { return t.spec }

func (t dispatchTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	return t.d.Dispatch(ctx, t.spec.Name, args)
}

// dispatchFinal is a Submitter whose validator is the request's dispatcher: a nil error accepts, an *AgentRefusal
// refuses with its reason, and any other error is a failure of the validator.
type dispatchFinal struct {
	spec ports.ModelTool
	d    ports.ToolDispatcher
}

func (f dispatchFinal) Spec() ports.ModelTool { return f.spec }

func (f dispatchFinal) Submit(ctx context.Context, args json.RawMessage) (Verdict, error) {
	_, err := f.d.Dispatch(ctx, f.spec.Name, args)
	var refusal *ports.AgentRefusal
	switch {
	case err == nil:
		return Verdict{Accepted: true}, nil
	case errors.As(err, &refusal):
		return Verdict{Reason: refusal.Reason}, nil
	}
	return Verdict{}, err
}

// portResult maps the loop's result to the port's. The loop's text-only stop is a done run with no submission, as the
// port says.
func portResult(res Result) ports.AgentResult {
	out := ports.AgentResult{Stop: portStop(res.Reason), FinalText: res.LastText, Turns: res.Turn, Refusals: res.Refusals,
		Nudges: res.Nudges, Usage: res.Usage, CostUSD: res.CostUSD, Transcript: res.Transcript, Retries: res.Retries,
		FailedCall: res.FailedCall}
	if res.Submission != nil {
		out.Submission = &ports.AgentSubmission{Tool: res.Submission.Tool, Arguments: res.Submission.Arguments}
	}
	return out
}

func portStop(r StopReason) ports.AgentStop {
	switch r {
	case StopSubmitted, StopNoSubmission:
		return ports.AgentStopDone
	case StopRefused:
		return ports.AgentStopRefused
	case StopBudget:
		return ports.AgentStopBudget
	case StopDeadline:
		return ports.AgentStopDeadline
	case StopTurnCap:
		return ports.AgentStopTurns
	case StopCancelled:
		return ports.AgentStopCancelled
	case StopModelError:
		return ports.AgentStopError
	}
	return ports.AgentStopError
}
