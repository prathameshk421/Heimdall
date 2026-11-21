// Package agentloop runs a model against a fixed set of tools until it submits a valid final answer or a limit stops it.
//
// # Purpose
//
// Action mode asks a model to plan tests, write them and prove findings (ACTION_MODE.md 4). This package is the
// built-in loop for that work: it sends the conversation to the model port, runs each tool call through a registry the
// caller passes in, appends the results and stops on one of the conditions below. It does not know any tool. The
// sandbox tools (AM11), the prompts and the validators belong to the caller. It retries a model call that failed for
// now (see Retries below), and it does not pick a model.
//
// # API
//
// NewRunner(model, clock, price) returns a Runner, which implements ports.AgentLoop. This is the entry point of the
// product: internal/app/action and cmd/heimdall-bench both call Runner.Run with a ports.AgentRun, so the bench measures
// the loop that ships. The caller implements ports.ToolDispatcher and names its final tools; a dispatcher error from a
// final tool that is a *ports.AgentRefusal is a refusal. The result is a ports.AgentResult: the stop (done, budget,
// deadline, turns, refused, cancelled, error), the final text, the submission, and usage, cost and latency per turn.
// A run that keeps answering with text after two nudges is a done run with no submission.
// Runner.WithRetry(wait, random) returns a Runner that retries; a Runner without it ends a run on the first failed
// model call.
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
// # Answers with no tool call
//
// Some models (DeepSeek among them) answer in text, or write a tool call as text. The loop handles this in three steps.
//
// 1. Config.RequireToolCall asks the model port for tool_choice "required". When the port reports a fallback to
// "auto" (ModelResponse.Fallback), the rest of the run asks for "auto", so the endpoint is not refused on every turn.
//
// 2. An answer with no tool call is read for calls written as text (textcall.go): DeepSeek DSML invoke blocks, the
// DeepSeek V3 tool tokens, <tool_call> tags and fenced JSON call objects, tried in that order. A call is accepted only
// when it names one of the run's tools and its arguments are a JSON object that fits the tool's schema (required
// properties, declared types, no unknown property when the schema forbids them). When one call fails the checks, the
// whole answer is text. The parse is bounded: an answer over 64 KB is not read, and more than 8 calls is text. It
// never panics (FuzzTextCalls). Accepted calls go into the conversation as real tool calls with no text, so the
// model sees the wire form and the raw text is not sent back.
//
// 3. An answer that is still only text gets one fixed user message (Config.Nudge, or "Call a tool now. When you are
// done, call <final tools>."), at most twice a run. The next text answer after the second nudge ends the run with
// StopNoSubmission. Each nudge costs a turn and goes through the same budget, token and deadline checks as any call.
// The nudge is a fixed protocol message about the tool interface. It does not tell the model what to think or what
// to find, so it is not scripted reasoning (CODING_STANDARDS 12).
//
// Each turn keeps a bounded transcript in ports.AgentTurn: the first 2 KB of the text, the first 16 calls (name and
// argument size), the finish reason, the tool choice and fallback the port reported, and whether the calls were read
// from text or the loop nudged after the answer. It also keeps the first 2 KB of the answer's reasoning and its full
// size.
//
// The conversation keeps each answer's ports.ModelReasoning on its assistant message, whole, also when the calls were
// read from the text or the loop nudged after it. An endpoint in thinking mode with tools refuses a call whose earlier
// turns lack it (AM12d). The reasoning counts toward no new cap: the provider bills it as output.
//
// # Retries
//
// Config.Retry (Runner.WithRetry) sends a model call again when it failed with a rate limit (ModelErrorRateLimited,
// HTTP 429) or a server error (ModelErrorServer, HTTP 5xx). Any other failure is never retried: an ordinary 4xx is a
// fault of the request. The adapter types an HTML 400 page as ModelErrorInvalid, so it is not retried either. A call
// is sent at most 3 times. The wait before attempt n+1 is the provider's Retry-After, capped at 60 s, or else a
// random time in [0, min(30 s, 2 s * 2^(n-1))] (exponential backoff with full jitter, drawn from the Random port).
// The loop waits only through the Waiter port, never time.Sleep. Before each wait the loop checks, in order: the
// caller and the deadline; the cost the provider billed for the failed attempt, which is added once (most failures
// bill nothing and report nothing); the retry rule; that the wait ends before the deadline (else StopDeadline, with no
// wait); and the worst-case budget guard for the next attempt (else StopBudget, with no wait). A retried call is one
// turn. The turn records Attempts and one ports.AgentAttempt (kind, status, request size and hash, wait) per failed
// attempt; SameRequest marks an attempt whose body hash equals the attempt before it. Result.Retries
// counts the retries of the run, and Result.FailedCall holds the attempts of the call that ended it. The bench stops a
// whole run when several calls in a row fail after their retries (cmd/heimdall-bench, the provider breaker).
//
// # Failure modes
//
// A context cancel ends the run at the next check, or at once when
// the model or tool call honours the context. A tool that ignores its context can delay the stop until it returns.
// The loop does not cap the size of a tool result. Each tool bounds its own output.
//
// # Tests
//
// loop_test.go uses a scripted fake model and a fake clock: tool dispatch, one retry after a refusal, a second
// refusal, malformed submission arguments, budget stop, the worst-case guard for cost and for the input and output token
// caps, usage that cannot be true, per-turn latency, the per-answer output cap, deadline stop (before a call and between
// tool calls), turn cap, unknown tool, bad arguments, tool error, no submission, cancellation and a model error.
// retry_test.go covers the retry rule with a fake waiter and a fixed jitter: a 500 then success, a 429 with
// Retry-After then success, the Retry-After cap, three 500s and the wrapped error, the backoff ceiling, a deadline with
// no time for the wait, a cancel during the wait, the budget guard on a retry after a billed failure, usage that
// cannot be true on a failure, ordinary 4xx and other errors with no retry, and the Runner through the port.
// robust_test.go covers text calls, nudges against the turn and budget caps, the tool choice and its fallback, and
// the bounded transcript. reasoning_test.go covers the reasoning kept through tool calls, text calls and nudges, its
// 2 KB record, and the same-body mark on failed attempts. textcall_test.go covers each text format, the refusals, the bounds and a fuzz target.
// runner_test.go is the contract test of ports.AgentLoop, run through the port only.
package agentloop
