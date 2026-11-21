package action

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// The proof run's tool-calling loop, written to the design of internal/app/agentloop (ACTION_MODE 4.2): send the
// messages and the tool schemas, run each tool call through a fixed table, append the results, and stop on a submitter,
// the turn cap, the deadline or the cost cap.
//
// It lives here because the dependency rule (R3, archcheck) lets an app package import domain and ports only, so this
// package cannot import the agent loop in another app package. The shared parts are the port values and the tool
// shape, not the code: the tools, the prompts, the budget guard and the proving step are this package's own, and the
// bench keeps its copy for its own runs. Moving the loop to a package both layers may import needs the owner's yes.

// loopStop says why one run of the reviewer ended.
type loopStop string

// The ways the reviewer's loop ends.
const (
	loopSubmitted    loopStop = "submitted"     // a submitter accepted the final answer
	loopRefused      loopStop = "refused"       // the one retry after a refusal was refused too
	loopBudget       loopStop = "budget"        // the next call could pass a cap, so it was not made
	loopDeadline     loopStop = "deadline"      // the clock reached the deadline
	loopTurnCap      loopStop = "turn_cap"      // the model used every answer it had
	loopNoSubmission loopStop = "no_submission" // the model answered with text and no tool call
	loopCancelled    loopStop = "cancelled"     // the caller's context ended
	loopModelError   loopStop = "model_error"   // the model call failed for another reason
)

// refusalsAfterRetry is the retries the model gets after a refused submission (ACTION_MODE 4.1: refused with field
// paths, one retry). A second refusal ends the run.
const refusalsAfterRetry = 1

// loopConfig bounds one run. Every field is required, so a run always has an end.
type loopConfig struct {
	Model           string
	MaxOutputTokens int
	// MaxTurns is the number of model answers allowed.
	MaxTurns int
	Deadline time.Time
}

// loopTool is one tool the model may call. A returned string and a returned error both go back to the model as its
// result; the caller bounds the string.
type loopTool interface {
	Spec() ports.ModelTool
	Run(ctx context.Context, arguments json.RawMessage) (string, error)
}

// loopVerdict is the caller's decision on one submission. Reason says what to fix, with field paths.
type loopVerdict struct {
	Accepted bool
	Reason   string
}

// loopSubmitter is a final-answer tool. Its Submit method holds the caller's validator. A refused submission goes back
// to the model once, and a second refusal ends the run.
type loopSubmitter interface {
	Spec() ports.ModelTool
	Submit(ctx context.Context, arguments json.RawMessage) (loopVerdict, error)
}

// loopResult is the typed end of one run. It is always filled.
type loopResult struct {
	Stop     loopStop
	Turns    int
	Refusals int
}

// loop drives one conversation through the model port. It holds no state between runs.
type loop struct {
	model      ports.ModelChat
	clock      ports.Clock
	cfg        loopConfig
	tools      map[string]loopTool
	submitters map[string]loopSubmitter
	specs      []ports.ModelTool
}

// newLoop checks the config and the registry, so a miswired run fails before a model is called.
func newLoop(model ports.ModelChat, clock ports.Clock, cfg loopConfig, tools []loopTool, submitters []loopSubmitter) (*loop, error) {
	if model == nil || clock == nil {
		return nil, errors.New("reviewer loop needs a model and a clock")
	}
	if strings.TrimSpace(cfg.Model) == "" || cfg.MaxOutputTokens <= 0 || cfg.MaxTurns <= 0 || cfg.Deadline.IsZero() {
		return nil, errors.New("reviewer loop needs a model name, a positive output cap and turn cap, and a deadline")
	}
	if len(submitters) == 0 {
		return nil, errors.New("reviewer loop needs at least one submitter")
	}
	l := &loop{model: model, clock: clock, cfg: cfg, tools: map[string]loopTool{}, submitters: map[string]loopSubmitter{}}
	for _, t := range tools {
		if err := l.register(t.Spec()); err != nil {
			return nil, err
		}
		l.tools[t.Spec().Name] = t
	}
	for _, s := range submitters {
		if err := l.register(s.Spec()); err != nil {
			return nil, err
		}
		l.submitters[s.Spec().Name] = s
	}
	return l, nil
}

// register refuses a tool whose name is empty or already taken.
func (l *loop) register(spec ports.ModelTool) error {
	if strings.TrimSpace(spec.Name) == "" {
		return errors.New("reviewer tool has no name")
	}
	if _, ok := l.tools[spec.Name]; ok {
		return fmt.Errorf("reviewer tool %q is registered twice", spec.Name)
	}
	if _, ok := l.submitters[spec.Name]; ok {
		return fmt.Errorf("reviewer tool %q is registered twice", spec.Name)
	}
	l.specs = append(l.specs, spec)
	return nil
}

// loopRun is the state of one Run call.
type loopRun struct {
	l            *loop
	parent       context.Context
	ctx          context.Context
	messages     []ports.ModelMessage
	res          loopResult
	refusalsLeft int
}

// run sends the conversation to the model and runs its tool calls until a stop condition holds. The error wraps the
// cause for loopCancelled and loopModelError, so errors.Is works on context.Canceled and on ErrProofBudget.
func (l *loop) run(parent context.Context, messages []ports.ModelMessage) (loopResult, error) {
	r := &loopRun{l: l, parent: parent, messages: append([]ports.ModelMessage(nil), messages...),
		refusalsLeft: refusalsAfterRetry}
	wait := l.cfg.Deadline.Sub(l.clock.Now())
	if wait <= 0 {
		r.res.Stop = loopDeadline
		return r.res, nil
	}
	ctx, cancel := context.WithTimeout(parent, wait)
	defer cancel()
	r.ctx = ctx
	return r.res, r.converse()
}

// converse is the conversation: one answer, its tool calls, then the next answer.
func (r *loopRun) converse() error {
	for {
		if r.stopBeforeCall() {
			return r.cancelErr()
		}
		resp, err := r.l.model.Complete(r.ctx, ports.ModelRequest{Model: r.l.cfg.Model, Messages: r.messages,
			Tools: r.l.specs, MaxOutputTokens: r.l.cfg.MaxOutputTokens})
		if err != nil {
			return r.modelFailed(err)
		}
		r.record(resp)
		if len(resp.ToolCalls) == 0 {
			r.res.Stop = loopNoSubmission
			return nil
		}
		if r.dispatch(resp.ToolCalls) {
			return r.cancelErr()
		}
	}
}

// cancelErr is the error that goes with the stop reason: the context error for a cancel, otherwise nil.
func (r *loopRun) cancelErr() error {
	if r.res.Stop == loopCancelled {
		return fmt.Errorf("reviewer loop cancelled: %w", r.parent.Err())
	}
	return nil
}

// interrupted sets the stop reason and reports true when the caller cancelled or the deadline passed.
func (r *loopRun) interrupted() bool {
	switch {
	case r.parent.Err() != nil:
		r.res.Stop = loopCancelled
	case r.ctx.Err() != nil, !r.l.clock.Now().Before(r.l.cfg.Deadline):
		r.res.Stop = loopDeadline
	default:
		return false
	}
	return true
}

// stopBeforeCall runs the checks that come before each model call. The caps themselves are the budget wrapper's check,
// so a call that could pass a cap is refused at the port and never reaches the provider.
func (r *loopRun) stopBeforeCall() bool {
	if r.interrupted() {
		return true
	}
	if r.res.Turns >= r.l.cfg.MaxTurns {
		r.res.Stop = loopTurnCap
		return true
	}
	return false
}

// modelFailed records a failed model call. A budget refusal is the budget stop, not a model failure.
func (r *loopRun) modelFailed(err error) error {
	if r.interrupted() {
		return r.cancelErr()
	}
	if errors.Is(err, ErrProofBudget) {
		r.res.Stop = loopBudget
		return fmt.Errorf("reviewer loop budget: %w", err)
	}
	r.res.Stop = loopModelError
	return fmt.Errorf("reviewer loop model call: %w", err)
}

// record adds one answer to the conversation and the tally.
func (r *loopRun) record(resp ports.ModelResponse) {
	r.res.Turns++
	r.messages = append(r.messages, ports.ModelMessage{Role: ports.ModelRoleAssistant, Content: resp.Content,
		ToolCalls: resp.ToolCalls})
}

// dispatch runs the tool calls of one answer in order, and reports true when the run ended.
func (r *loopRun) dispatch(calls []ports.ModelToolCall) bool {
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

// call runs one tool call or submission.
func (r *loopRun) call(c ports.ModelToolCall) bool {
	args, argErr := loopArguments(c.Arguments)
	if s, ok := r.l.submitters[c.Name]; ok {
		return r.submit(c, s, args, argErr)
	}
	r.toolResult(c.ID, r.runTool(c, args, argErr))
	return false
}

// runTool runs one tool and turns its result, or its error, into the text the model sees.
func (r *loopRun) runTool(c ports.ModelToolCall, args json.RawMessage, argErr error) string {
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

// submit runs one submission. An accepted one ends the run; a refused one goes back to the model once.
func (r *loopRun) submit(c ports.ModelToolCall, s loopSubmitter, args json.RawMessage, argErr error) bool {
	verdict := loopVerdict{Reason: ""}
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
		r.res.Stop = loopSubmitted
		return true
	}
	r.res.Refusals++
	r.toolResult(c.ID, "refused: "+verdict.Reason)
	if r.refusalsLeft == 0 {
		r.res.Stop = loopRefused
		return true
	}
	r.refusalsLeft--
	return false
}

// toolResult appends one tool message.
func (r *loopRun) toolResult(callID, text string) {
	r.messages = append(r.messages, ports.ModelMessage{Role: ports.ModelRoleTool, ToolCallID: callID, Content: text})
}

// loopArguments checks that the model wrote a JSON object. An empty string means no arguments.
func loopArguments(raw string) (json.RawMessage, error) {
	if strings.TrimSpace(raw) == "" {
		return json.RawMessage("{}"), nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &obj); err != nil || obj == nil {
		return nil, errors.New("tool arguments are not a JSON object")
	}
	return json.RawMessage(raw), nil
}
