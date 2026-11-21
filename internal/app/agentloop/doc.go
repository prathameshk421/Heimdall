// Package agentloop runs a model against a fixed set of tools until it submits a valid final answer or a limit stops it.
//
// # Purpose
//
// Action mode asks a model to plan tests, write them and prove findings (ACTION_MODE.md 4). This package is the
// built-in loop for that work: it sends the conversation to the model port, runs each tool call through a registry the
// caller passes in, appends the results and stops on one of the conditions below. It does not know any tool. The
// sandbox tools (AM11), the prompts and the validators belong to the caller. It does not retry a failed model call
// and it does not pick a model.
//
// # API
//
// NewRunner(model, clock, price) returns a Runner, which implements ports.AgentLoop. This is the entry point of the
// product: internal/app/action and cmd/heimdall-bench both call Runner.Run with a ports.AgentRun, so the bench measures
// the loop that ships. The caller implements ports.ToolDispatcher and names its final tools; a dispatcher error from a
// final tool that is a *ports.AgentRefusal is a refusal. The result is a ports.AgentResult: the stop (done, budget,
// deadline, turns, refused, cancelled, error), the final text, the submission, and usage, cost and latency per turn.
// A text-only answer is a done run with no submission.
//
// Underneath, New(model, clock, Config, []Tool, []Submitter) returns a Loop. Loop.Run(ctx, messages) returns a Result and an error.
// A Tool has Spec and Run. A Submitter is a final-answer tool: its Submit method holds the caller's validator and
// returns a Verdict. Result.Reason is one of StopSubmitted, StopRefused, StopBudget, StopDeadline, StopTurnCap,
// StopNoSubmission, StopCancelled and StopModelError. Result also carries the cost, the turn count, the refusal count,
// the token usage, the accepted Submission and the whole transcript, so the proof outcome (AM5) can map a stop to a
// verdict. The error is nil for every stop that is an outcome of the run. It wraps the cause for StopCancelled and
// StopModelError, so errors.Is works on context.Canceled and on ports.ErrUnavailable.
//
// # Invariants
//
// A turn is one model answer. The checks before each model call run in this order: cancellation, deadline, budget,
// turn cap. The budget is the cost and the input and output token caps of ACTION_MODE section 7. It is a worst-case
// guard: before each call the loop sizes the request (its JSON bytes, plus a framing allowance, as input tokens) and
// adds the full per-answer output cap, and a call whose worst case could pass a cap is not made (StopBudget). The
// cost bound uses the model's price (ports.ModelPrice.CallBound), so Config.Price is required. After a call the loop
// adds what the model port reports; a cost or a token count that cannot be true (negative, not finite) ends the run
// with StopBudget and an error that wraps ports.ErrAgentUsage. Each answer is also capped by MaxOutputTokens.
// The deadline is read from the Clock port before each model call and each tool call, and it also bounds a call in
// flight through a context timeout made from the clock. Time never comes from the system clock.
// A refused submission goes back to the model as a tool result with the caller's reason. The model gets one retry
// (ACTION_MODE 4.1). A second refusal ends the run with StopRefused. A submission whose arguments are not a JSON
// object counts as a refusal. A Submitter error does not.
// An unknown tool, malformed arguments and a tool error become a tool result that starts with "error:". The loop
// never panics on them, and each one costs the turn that carried it. When one answer holds several calls, they run in
// order, and the run ends at the first accepted submission. The loop holds no state between runs.
//
// # Failure modes
//
// A model answer with no tool call ends the run with StopNoSubmission, because the loop has no way to ask again
// without scripting the model (CODING_STANDARDS 12). A context cancel ends the run at the next check, or at once when
// the model or tool call honours the context. A tool that ignores its context can delay the stop until it returns.
// The loop does not cap the size of a tool result. Each tool bounds its own output.
//
// # Tests
//
// loop_test.go uses a scripted fake model and a fake clock: tool dispatch, one retry after a refusal, a second
// refusal, malformed submission arguments, budget stop, the worst-case guard for cost and for the input and output token
// caps, usage that cannot be true, per-turn latency, the per-answer output cap, deadline stop (before a call and between
// tool calls), turn cap, unknown tool, bad arguments, tool error, no submission, cancellation and a model error.
// runner_test.go is the contract test of ports.AgentLoop, run through the port only.
package agentloop
