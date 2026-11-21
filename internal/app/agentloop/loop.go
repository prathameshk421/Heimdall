package agentloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// retriesAfterRefusal is the number of retries the model gets after a refused submission (ACTION_MODE 4.1: refused
// with field paths, one retry). The refusal that follows the retry ends the run.
const retriesAfterRefusal = 1

// Config holds the limits of one run. Every field is required, so a run always has an end.
type Config struct {
	Model           string
	MaxOutputTokens int
	// BudgetUSD is checked before each model call. The call that crosses it is not stopped, so the final cost can
	// pass the budget by one answer.
	BudgetUSD float64
	// Deadline is read against the clock before each model call and each tool call. It also bounds a call in flight.
	Deadline time.Time
	// MaxTurns is the number of model answers allowed.
	MaxTurns int
}

// Loop runs the model against a fixed set of tools. It holds no state between runs.
type Loop struct {
	model      ports.ModelChat
	clock      ports.Clock
	cfg        Config
	tools      map[string]Tool
	submitters map[string]Submitter
	specs      []ports.ModelTool
}

// New checks the config and the registry and returns a Loop. Tool names must be non-empty and unique across tools
// and submitters.
func New(model ports.ModelChat, clock ports.Clock, cfg Config, tools []Tool, submitters []Submitter) (*Loop, error) {
	if model == nil || clock == nil {
		return nil, errors.New("agent loop needs a model and a clock")
	}
	if strings.TrimSpace(cfg.Model) == "" || cfg.BudgetUSD <= 0 || cfg.MaxTurns <= 0 || cfg.Deadline.IsZero() {
		return nil, errors.New("agent loop config needs a model, a positive budget, a positive turn cap and a deadline")
	}
	if len(submitters) == 0 {
		return nil, errors.New("agent loop needs at least one submitter")
	}
	l := &Loop{model: model, clock: clock, cfg: cfg, tools: map[string]Tool{}, submitters: map[string]Submitter{}}
	for _, t := range tools {
		if err := l.register(t.Spec(), func() { l.tools[t.Spec().Name] = t }); err != nil {
			return nil, err
		}
	}
	for _, s := range submitters {
		if err := l.register(s.Spec(), func() { l.submitters[s.Spec().Name] = s }); err != nil {
			return nil, err
		}
	}
	return l, nil
}

func (l *Loop) register(spec ports.ModelTool, add func()) error {
	if strings.TrimSpace(spec.Name) == "" {
		return errors.New("agent loop tool has no name")
	}
	_, isTool := l.tools[spec.Name]
	_, isSubmitter := l.submitters[spec.Name]
	if isTool || isSubmitter {
		return fmt.Errorf("agent loop tool %q is registered twice", spec.Name)
	}
	add()
	l.specs = append(l.specs, spec)
	return nil
}

// run is the state of one Run call.
type run struct {
	l            *Loop
	parent       context.Context
	ctx          context.Context
	res          Result
	messages     []ports.ModelMessage
	refusalsLeft int
}

// Run sends the conversation to the model and executes its tool calls until a stop condition holds. The returned
// Result is always filled. The error is nil for every stop that is an outcome of the run (submitted, refused,
// budget, deadline, turn cap, no submission). It wraps the cause for StopCancelled and StopModelError.
func (l *Loop) Run(parent context.Context, messages []ports.ModelMessage) (Result, error) {
	r := &run{l: l, parent: parent, messages: append([]ports.ModelMessage(nil), messages...), refusalsLeft: retriesAfterRefusal}
	wait := l.cfg.Deadline.Sub(l.clock.Now())
	if wait <= 0 {
		r.res.Reason = StopDeadline
		r.res.Transcript = r.messages
		return r.res, nil
	}
	ctx, cancel := context.WithTimeout(parent, wait)
	defer cancel()
	r.ctx = ctx
	err := r.loop()
	r.res.Transcript = r.messages
	return r.res, err
}

func (r *run) loop() error {
	for {
		if r.stopBeforeCall() {
			return r.cancelErr()
		}
		resp, err := r.l.model.Complete(r.ctx, ports.ModelRequest{
			Model: r.l.cfg.Model, Messages: r.messages, Tools: r.l.specs, MaxOutputTokens: r.l.cfg.MaxOutputTokens,
		})
		if err != nil {
			return r.modelFailed(err)
		}
		r.record(resp)
		if len(resp.ToolCalls) == 0 {
			r.res.Reason = StopNoSubmission
			return nil
		}
		if r.dispatch(resp.ToolCalls) {
			return r.cancelErr()
		}
	}
}

// cancelErr is the error that goes with the stop reason: the context error for StopCancelled, otherwise nil.
func (r *run) cancelErr() error {
	if r.res.Reason == StopCancelled {
		return fmt.Errorf("agent loop cancelled: %w", r.parent.Err())
	}
	return nil
}

// interrupted sets the stop reason and reports true when the caller cancelled or the deadline passed.
func (r *run) interrupted() bool {
	switch {
	case r.parent.Err() != nil:
		r.res.Reason = StopCancelled
	case r.ctx.Err() != nil, !r.l.clock.Now().Before(r.l.cfg.Deadline):
		r.res.Reason = StopDeadline
	default:
		return false
	}
	return true
}

// stopBeforeCall runs the checks that come before each model call and sets the stop reason when one holds.
func (r *run) stopBeforeCall() bool {
	if r.interrupted() {
		return true
	}
	switch {
	case r.res.CostUSD >= r.l.cfg.BudgetUSD:
		r.res.Reason = StopBudget
	case r.res.Turns >= r.l.cfg.MaxTurns:
		r.res.Reason = StopTurnCap
	default:
		return false
	}
	return true
}

func (r *run) modelFailed(err error) error {
	if r.interrupted() {
		return r.cancelErr()
	}
	r.res.Reason = StopModelError
	return fmt.Errorf("agent loop model call: %w", err)
}

func (r *run) record(resp ports.ModelResponse) {
	r.res.Turns++
	r.res.CostUSD += resp.CostUSD
	r.res.Usage.InputTokens += resp.Usage.InputTokens
	r.res.Usage.CachedInputTokens += resp.Usage.CachedInputTokens
	r.res.Usage.OutputTokens += resp.Usage.OutputTokens
	r.res.LastText = resp.Content
	r.messages = append(r.messages, ports.ModelMessage{Role: ports.ModelRoleAssistant, Content: resp.Content, ToolCalls: resp.ToolCalls})
}

// dispatch runs the tool calls of one answer in order. It reports true when the run ended: a submission was accepted,
// a retried submission was refused, or the caller or the deadline interrupted it.
func (r *run) dispatch(calls []ports.ModelToolCall) bool {
	for _, c := range calls {
		if r.interrupted() {
			return true
		}
		if r.call(c) {
			return true
		}
	}
	return false
}

func (r *run) call(c ports.ModelToolCall) bool {
	args, argErr := parseArguments(c.Arguments)
	if s, ok := r.l.submitters[c.Name]; ok {
		return r.submit(c, s, args, argErr)
	}
	r.toolResult(c.ID, r.runTool(c, args, argErr))
	return false
}

func (r *run) runTool(c ports.ModelToolCall, args json.RawMessage, argErr error) string {
	t, ok := r.l.tools[c.Name]
	if !ok {
		return fmt.Sprintf("error: unknown tool %q", c.Name)
	}
	if argErr != nil {
		return "error: " + argErr.Error()
	}
	out, err := t.Run(r.ctx, args)
	if err != nil {
		return "error: " + err.Error()
	}
	return out
}

func (r *run) submit(c ports.ModelToolCall, s Submitter, args json.RawMessage, argErr error) bool {
	verdict := Verdict{Reason: ""}
	if argErr != nil {
		verdict.Reason = argErr.Error()
	} else {
		v, err := s.Submit(r.ctx, args)
		if err != nil {
			r.toolResult(c.ID, "error: "+err.Error())
			return false
		}
		verdict = v
	}
	if verdict.Accepted {
		r.toolResult(c.ID, "accepted")
		r.res.Reason = StopSubmitted
		r.res.Submission = &Submission{Tool: c.Name, Arguments: c.Arguments}
		return true
	}
	r.res.Refusals++
	r.toolResult(c.ID, "refused: "+verdict.Reason)
	if r.refusalsLeft == 0 {
		r.res.Reason = StopRefused
		return true
	}
	r.refusalsLeft--
	return false
}

func (r *run) toolResult(callID, text string) {
	r.messages = append(r.messages, ports.ModelMessage{Role: ports.ModelRoleTool, ToolCallID: callID, Content: text})
}

// parseArguments checks that the model wrote a JSON object. An empty string means no arguments.
func parseArguments(raw string) (json.RawMessage, error) {
	if strings.TrimSpace(raw) == "" {
		return json.RawMessage("{}"), nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &obj); err != nil || obj == nil {
		return nil, errors.New("tool arguments are not a JSON object")
	}
	return json.RawMessage(raw), nil
}
