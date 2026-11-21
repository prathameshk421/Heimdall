package agentloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// maxNudges is the number of times a run asks again after an answer with no tool call. The answer after the last
// nudge that still has no tool call ends the run with StopNoSubmission.
const maxNudges = 2

// The bounds of the per-turn transcript (ports.AgentTurn).
const (
	turnTextBytes  = 2 << 10
	turnCallBriefs = 16
)

// retriesAfterRefusal is the number of retries the model gets after a refused submission (ACTION_MODE 4.1: refused
// with field paths, one retry). The refusal that follows the retry ends the run.
const retriesAfterRefusal = 1

// Config holds the limits of one run. Every field is required, so a run always has an end.
type Config struct {
	Model string
	// MaxOutputTokens caps each answer (ACTION_MODE section 7, proposed 4k).
	MaxOutputTokens int
	// BudgetUSD is the cost cap of the run. Before each model call the loop adds the worst case of that call to the
	// spend, and a call that could pass the cap is not made (StopBudget).
	BudgetUSD float64
	// InputTokenCap and OutputTokenCap are the per-run token caps of ACTION_MODE section 7 (proposed 800k and 40k).
	// Input counts every prompt token, cached ones included. They are checked the same way as BudgetUSD.
	InputTokenCap  int64
	OutputTokenCap int64
	// Price is the model's price. It sizes the worst case of a call before the call, so it is required: without it
	// the cap could not see the spend.
	Price ports.ModelPrice
	// Deadline is read against the clock before each model call and each tool call. It also bounds a call in flight.
	Deadline time.Time
	// MaxTurns is the number of model answers allowed.
	MaxTurns int
	// RequireToolCall asks the model port for tool_choice "required" until the port reports a fallback to auto.
	RequireToolCall bool
	// Nudge is the fixed message sent after an answer with no tool call. Empty means one that names the submitters.
	Nudge string
	// Retry is how a model call that failed for now is sent again (retry.go). The zero value retries nothing.
	Retry Retry
}

// check refuses a config that would let a run go on without an end.
func (cfg Config) check() error {
	if strings.TrimSpace(cfg.Model) == "" || cfg.BudgetUSD <= 0 || cfg.MaxTurns <= 0 || cfg.Deadline.IsZero() {
		return errors.New("agent loop config needs a model, a positive budget, a positive turn cap and a deadline")
	}
	if cfg.MaxOutputTokens <= 0 || cfg.InputTokenCap <= 0 || cfg.OutputTokenCap <= 0 {
		return errors.New("agent loop config needs a positive output cap per answer and positive input and output token caps per run")
	}
	if !cfg.Price.Valid() {
		return errors.New("agent loop config needs a positive price for the model: without it the caps cannot see the spend")
	}
	return cfg.Retry.check()
}

// Loop runs the model against a fixed set of tools. It holds no state between runs.
type Loop struct {
	model      ports.ModelChat
	clock      ports.Clock
	cfg        Config
	tools      map[string]Tool
	submitters map[string]Submitter
	specs      []ports.ModelTool
	byName     map[string]ports.ModelTool
	finals     []string
}

// New checks the config and the registry and returns a Loop. Tool names must be non-empty and unique across tools
// and submitters.
func New(model ports.ModelChat, clock ports.Clock, cfg Config, tools []Tool, submitters []Submitter) (*Loop, error) {
	if model == nil || clock == nil {
		return nil, errors.New("agent loop needs a model and a clock")
	}
	if err := cfg.check(); err != nil {
		return nil, err
	}
	if len(submitters) == 0 {
		return nil, errors.New("agent loop needs at least one submitter")
	}
	l := &Loop{model: model, clock: clock, cfg: cfg, tools: map[string]Tool{}, submitters: map[string]Submitter{},
		byName: map[string]ports.ModelTool{}}
	for _, t := range tools {
		if err := l.register(t.Spec(), func() { l.tools[t.Spec().Name] = t }); err != nil {
			return nil, err
		}
	}
	for _, s := range submitters {
		if err := l.register(s.Spec(), func() { l.submitters[s.Spec().Name] = s }); err != nil {
			return nil, err
		}
		l.finals = append(l.finals, s.Spec().Name)
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
	l.byName[spec.Name] = spec
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
	// autoOnly is set once the model port reported that the endpoint refused tool_choice "required".
	autoOnly bool
	// failed holds the failed attempts of the model call in progress.
	failed []ports.AgentAttempt
}

// Run sends the conversation to the model and executes its tool calls until a stop condition holds. The returned
// Result is always filled. The error is nil for every stop that is an outcome of the run (submitted, refused,
// budget, deadline, turn cap, no submission). It wraps the cause for StopCancelled and StopModelError, and
// ports.ErrAgentUsage for a StopBudget that came from a usage report that cannot be true.
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
		resp, modelTime, ended, err := r.complete()
		if ended {
			return err
		}
		calls, fromText := r.calls(resp)
		if err := r.record(resp, calls, fromText, modelTime); err != nil {
			return err
		}
		if len(calls) == 0 {
			if r.res.Nudges >= maxNudges {
				r.res.Reason = StopNoSubmission
				return nil
			}
			r.nudge()
			continue
		}
		started := r.l.clock.Now()
		done := r.dispatch(calls)
		r.res.Turn[len(r.res.Turn)-1].ToolLatency = r.l.clock.Now().Sub(started)
		if done {
			return r.cancelErr()
		}
	}
}

// request is the next model call. The same value sizes the call before it starts and is sent after.
func (r *run) request() ports.ModelRequest {
	req := ports.ModelRequest{Model: r.l.cfg.Model, Messages: r.messages, Tools: r.l.specs, MaxOutputTokens: r.l.cfg.MaxOutputTokens}
	switch {
	case r.l.cfg.RequireToolCall && r.autoOnly:
		req.ToolChoice = ports.ModelToolChoiceAuto
	case r.l.cfg.RequireToolCall:
		req.ToolChoice = ports.ModelToolChoiceRequired
	}
	return req
}

// calls returns the answer's tool calls. An answer with none is read for calls written as text (textCalls), and
// fromText reports that it was.
func (r *run) calls(resp ports.ModelResponse) (calls []ports.ModelToolCall, fromText bool) {
	if len(resp.ToolCalls) > 0 {
		return resp.ToolCalls, false
	}
	calls = textCalls(resp.Content, r.l.byName, fmt.Sprintf("text-%d", r.res.Turns+1))
	return calls, len(calls) > 0
}

// nudge asks the model once more for a tool call, with the fixed protocol message of the run.
func (r *run) nudge() {
	r.res.Nudges++
	r.res.Turn[len(r.res.Turn)-1].Nudged = true
	msg := r.l.cfg.Nudge
	if msg == "" {
		msg = "Call a tool now. When you are done, call " + strings.Join(r.l.finals, " or ") + "."
	}
	r.messages = append(r.messages, ports.ModelMessage{Role: ports.ModelRoleUser, Content: msg})
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
	case r.overBudget():
		r.res.Reason = StopBudget
	case r.res.Turns >= r.l.cfg.MaxTurns:
		r.res.Reason = StopTurnCap
	default:
		return false
	}
	return true
}

// overBudget is the worst-case guard (ACTION_MODE 7): it reports true when the next call could pass a cap whatever the
// model answers. The worst case is the request's own size plus the full per-answer output cap, so a call that is
// refused is one the run could not afford. A request that cannot be sized is over budget too, because an unsized call
// is an unbounded one.
func (r *run) overBudget() bool {
	body, err := json.Marshal(r.request())
	if err != nil {
		return true
	}
	cfg := r.l.cfg
	input := int64(len(body) + ports.AgentFramingTokens)
	return r.res.CostUSD+cfg.Price.CallBound(len(body), cfg.MaxOutputTokens) > cfg.BudgetUSD ||
		r.res.Usage.InputTokens+input > cfg.InputTokenCap ||
		r.res.Usage.OutputTokens+int64(cfg.MaxOutputTokens) > cfg.OutputTokenCap
}

// complete makes one model call and sends it again after a failure that may pass (Config.Retry). modelTime is the time
// of the attempt that answered. ended is true when the run stopped instead: the stop reason is set, err is what Run
// returns, and Result.FailedCall holds the call's failed attempts.
func (r *run) complete() (resp ports.ModelResponse, modelTime time.Duration, ended bool, err error) {
	r.failed = nil
	for attempt := 1; ; attempt++ {
		started := r.l.clock.Now()
		resp, callErr := r.l.model.Complete(r.ctx, r.request())
		modelTime = r.l.clock.Now().Sub(started)
		if callErr == nil {
			return resp, modelTime, false, nil
		}
		r.failed = append(r.failed, attemptOf(callErr, r.failed))
		if ended, err := r.retry(resp, callErr, attempt); ended {
			r.res.FailedCall = r.failed
			return ports.ModelResponse{}, modelTime, true, err
		}
	}
}

// retry handles one failed attempt. It charges what the provider billed for it, then waits and reports false when the
// call may be sent again. It reports true with the run's error when the run ends: the caller or the deadline
// interrupted it, the failure may not pass, the wait would reach the deadline, or the worst case of the next attempt
// could pass a cap. A wait never runs past the deadline, and the next attempt passes the same guard as a new call.
func (r *run) retry(billed ports.ModelResponse, callErr error, attempt int) (bool, error) {
	if r.interrupted() {
		return true, r.cancelErr()
	}
	if err := r.charge(billed); err != nil {
		return true, err
	}
	wait, ok := r.l.cfg.Retry.delay(callErr, attempt)
	if !ok {
		r.res.Reason = StopModelError
		return true, fmt.Errorf("agent loop model call: %w", callErr)
	}
	if !r.l.clock.Now().Add(wait).Before(r.l.cfg.Deadline) {
		r.res.Reason = StopDeadline
		return true, nil
	}
	if r.overBudget() {
		r.res.Reason = StopBudget
		return true, nil
	}
	r.failed[len(r.failed)-1].Wait = wait
	if err := r.l.cfg.Retry.Wait(r.ctx, wait); err != nil && !r.interrupted() {
		r.res.Reason = StopModelError
		return true, fmt.Errorf("agent loop wait before a model retry: %w", err)
	}
	if r.interrupted() {
		return true, r.cancelErr()
	}
	r.res.Retries++
	return false, nil
}

// charge adds what the provider billed for a failed attempt. Most failures bill nothing and report nothing, so nothing
// is added; a billed failure is counted once, so the next attempt is sized against the true spend.
func (r *run) charge(billed ports.ModelResponse) error {
	if err := r.usageErr(billed); err != nil {
		return err
	}
	r.addUsage(billed)
	return nil
}

// usageErr ends the run on a cost or a token count that cannot be true: a missing measure must never look like a
// cheap one.
func (r *run) usageErr(resp ports.ModelResponse) error {
	if !(resp.CostUSD >= 0) || math.IsInf(resp.CostUSD, 0) || resp.Usage.InputTokens < 0 || resp.Usage.CachedInputTokens < 0 || resp.Usage.OutputTokens < 0 {
		r.res.Reason = StopBudget
		return fmt.Errorf("agent loop: %w: a cost of %v and %d/%d tokens", ports.ErrAgentUsage, resp.CostUSD,
			resp.Usage.InputTokens, resp.Usage.OutputTokens)
	}
	return nil
}

func (r *run) addUsage(resp ports.ModelResponse) {
	r.res.CostUSD += resp.CostUSD
	r.res.Usage.InputTokens += resp.Usage.InputTokens
	r.res.Usage.CachedInputTokens += resp.Usage.CachedInputTokens
	r.res.Usage.OutputTokens += resp.Usage.OutputTokens
}

// record adds one answer to the conversation and the tally. A cost or a token count that cannot be true ends the run:
// a missing measure must never look like a cheap one. calls are the answer's tool calls; when they were read from the
// text, the conversation holds them as real calls with no text, so the model sees its call in the wire form and the
// text is not sent back. The answer's reasoning is kept in either case. The turn record holds its first 2 KB and its
// size.
func (r *run) record(resp ports.ModelResponse, calls []ports.ModelToolCall, fromText bool, modelTime time.Duration) error {
	if err := r.usageErr(resp); err != nil {
		return err
	}
	r.res.Turns++
	r.res.Turn = append(r.res.Turn, ports.AgentTurn{Usage: resp.Usage, CostUSD: resp.CostUSD, ModelLatency: modelTime,
		ToolCalls: len(calls), Text: clipText(resp.Content, turnTextBytes), Calls: briefs(calls), Finish: resp.Finish,
		TextCalls: fromText, ToolChoice: resp.ToolChoice, Fallback: clipText(resp.Fallback, turnTextBytes),
		Attempts: len(r.failed) + 1, Failed: r.failed, Reasoning: clipText(resp.Reasoning.Text, turnTextBytes),
		ReasoningBytes: len(resp.Reasoning.Text)})
	if resp.Fallback != "" {
		r.autoOnly = true
	}
	r.addUsage(resp)
	r.res.LastText = resp.Content
	content := resp.Content
	if fromText {
		content = ""
	}
	// The reasoning stays on the message whole: an endpoint in thinking mode with tools refuses a later call whose
	// earlier assistant turns lack the reasoning they gave (AM12d, live run 3).
	r.messages = append(r.messages, ports.ModelMessage{Role: ports.ModelRoleAssistant, Content: content, ToolCalls: calls,
		Reasoning: resp.Reasoning})
	return nil
}

// briefs is the bounded record of the calls: the first turnCallBriefs names and argument sizes.
func briefs(calls []ports.ModelToolCall) []ports.AgentCallBrief {
	var out []ports.AgentCallBrief
	for _, c := range calls[:min(len(calls), turnCallBriefs)] {
		out = append(out, ports.AgentCallBrief{Name: c.Name, ArgumentBytes: len(c.Arguments)})
	}
	return out
}

// clipText keeps at most limit bytes of s, cut at a rune boundary.
func clipText(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
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
