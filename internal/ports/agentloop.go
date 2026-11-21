package ports

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"
)

// AgentLoop runs one model against a set of tools until it finishes or a limit stops it. It is the one agent loop of
// the product: the proof run of Action mode and the bench both call it, so what the bench measures is what ships.
// The caller owns the tools, the prompts and the validators; the loop owns the conversation, the caps and the stop
// rules. Consumer: internal/app/action, cmd/heimdall-bench. Implementation: internal/app/agentloop.
type AgentLoop interface {
	// Run sends the conversation to the model and runs its tool calls through the request's dispatcher until a stop
	// condition holds. The result is always filled. The error is nil for every stop that is an outcome of the run
	// (done, budget, deadline, turns, refused). It wraps the cause for AgentStopCancelled and AgentStopError, so
	// errors.Is works on context.Canceled and on the model's own failure.
	Run(ctx context.Context, req AgentRun) (AgentResult, error)
}

// ModelPrice is a model's price in USD per million tokens. The loop sizes each call with CallBound before it starts,
// so a call that could pass a cap is never made. A price of zero would make every call look free, so Valid refuses it.
type ModelPrice struct {
	InputPerM       float64
	CachedInputPerM float64
	OutputPerM      float64
}

// Valid reports whether the price can bound a call: positive finite input and output prices, and a cache-read price
// that is not negative.
func (p ModelPrice) Valid() bool {
	return finitePositive(p.InputPerM) && finitePositive(p.OutputPerM) && p.CachedInputPerM >= 0 && !math.IsInf(p.CachedInputPerM, 0)
}

func finitePositive(v float64) bool { return v > 0 && !math.IsInf(v, 0) }

// AgentFramingTokens is an allowance for the tokens a provider adds around the messages and the tool schemas.
const AgentFramingTokens = 1000

// CallBound is an upper bound on the cost of one answer: a request of n bytes holds at most n tokens, because a token
// covers at least one byte; the answer holds at most maxOutput tokens; and every input token is billed at the full
// input price, which is never below the cache-read price.
func (p ModelPrice) CallBound(requestBytes, maxOutput int) float64 {
	return (float64(requestBytes+AgentFramingTokens)*p.InputPerM + float64(maxOutput)*p.OutputPerM) / 1e6
}

// AgentBudget holds the per-run caps. The loop checks them before each model call against the worst case of that call
// (the request's own size plus the full per-answer output cap), so the run's spend never passes a cap, whatever the
// model answers. Every field is required.
type AgentBudget struct {
	// USD is the cost cap. The cost of each answer is what the model port reports.
	USD float64
	// InputTokens counts every prompt token, cached ones included. OutputTokens counts every answer token.
	InputTokens  int64
	OutputTokens int64
}

// AgentRun is one run of the loop. Every field is required except System, so a run always has an end.
type AgentRun struct {
	// Model is the model id the run sends to the model port.
	Model string
	// System is the system prompt and User is the first user message.
	System string
	User   string
	// Tools are the tools the model may call, and FinalTools names the ones among them that end the run. Each name
	// must be non-empty and unique, and at least one tool must be final.
	Tools      []ModelTool
	FinalTools []string
	// Dispatcher runs every tool call, a final tool's included.
	Dispatcher ToolDispatcher
	// Budget holds the per-run cost and token caps, and MaxOutputTokens caps each answer.
	Budget          AgentBudget
	MaxOutputTokens int
	// MaxTurns is the number of model answers allowed.
	MaxTurns int
	// Deadline is read against the clock before each model call and each tool call. It also bounds a call in flight.
	Deadline time.Time
	// RequireToolCall asks the model port for tool_choice "required" on every call. The adapter may send "auto"
	// instead (see ModelResponse.Fallback); the run then stops asking for it.
	RequireToolCall bool
	// Nudge is the fixed user message the loop sends after an answer with no tool call, at most twice a run. Empty
	// means a default that names the final tools. It is a protocol message, never reasoning for the model.
	Nudge string
}

// ToolDispatcher is what the caller implements: it runs one tool call and returns the text the model sees.
//
// The text is bounded by the dispatcher: the loop does not cut it. A returned error goes back to the model as the tool
// result, prefixed "error:", and the run goes on. For a final tool, a nil error means the answer is valid and ends the
// run. An *AgentRefusal means the answer is not valid: its reason goes back to the model once, and a second refusal
// ends the run. Any other error from a final tool is a failure of the validator itself and is not a refusal.
type ToolDispatcher interface {
	Dispatch(ctx context.Context, name string, arguments json.RawMessage) (string, error)
}

// AgentRefusal is the error a dispatcher returns for a final tool whose answer is not valid. Reason says what to fix,
// with field paths, and the loop shows it to the model unchanged.
type AgentRefusal struct{ Reason string }

// Error returns the reason.
func (e *AgentRefusal) Error() string { return e.Reason }

// ErrAgentUsage means the model reported a cost or a token count that cannot be true. The run stops, because a missing
// measure must never look like a cheap one. The stop reason is AgentStopBudget.
var ErrAgentUsage = errors.New("the model reported a cost or a token count that cannot be true")

// AgentStop says why a run ended.
type AgentStop string

// The ways a run ends.
const (
	// AgentStopDone means a final tool accepted an answer, or the model kept answering with text and no tool call
	// after the loop asked again. Submission tells the two apart: it is nil for a text answer.
	AgentStopDone AgentStop = "done"
	// AgentStopBudget means the next call could pass a cap, so it was not made. It also covers a usage report that
	// cannot be true.
	AgentStopBudget AgentStop = "budget"
	// AgentStopDeadline means the clock reached the deadline.
	AgentStopDeadline AgentStop = "deadline"
	// AgentStopTurns means the model used every answer it had without a final answer.
	AgentStopTurns AgentStop = "turns"
	// AgentStopRefused means the retry after a refused final answer was refused too.
	AgentStopRefused AgentStop = "refused"
	// AgentStopCancelled means the caller's context ended. Run also returns the context error.
	AgentStopCancelled AgentStop = "cancelled"
	// AgentStopError means the model call failed. Run also returns that error.
	AgentStopError AgentStop = "error"
)

// AgentSubmission is the accepted final answer: the tool the model called and the JSON arguments it wrote.
type AgentSubmission struct {
	Tool      string
	Arguments string
}

// AgentTurn is the measure of one model answer and of the tool calls that followed it.
type AgentTurn struct {
	Usage   ModelUsage
	CostUSD float64
	// ModelLatency is the time of the model call, and ToolLatency the time of the tool calls it asked for. Both are
	// read from the Clock port.
	ModelLatency time.Duration
	ToolLatency  time.Duration
	// ToolCalls is the number of tool calls the answer held.
	ToolCalls int
	// The bounded transcript of the turn, so a failed run explains itself. Text is the first 2 KB of the answer's
	// text, Calls the first 16 tool calls (name and argument size, never the arguments), and Finish the provider's
	// finish reason. TextCalls is set when the calls were read from the text, and Nudged when the loop asked again
	// after this answer. ToolChoice and Fallback are what the model port reported.
	Text       string
	Calls      []AgentCallBrief
	Finish     ModelFinish
	TextCalls  bool
	Nudged     bool
	ToolChoice ModelToolChoice
	Fallback   string
	// Attempts is the number of times the loop sent this call: 1, or more when it retried a failure that may pass
	// (rate limited or a server error). Failed holds one entry per failed attempt, in order.
	Attempts int
	Failed   []AgentAttempt
	// Reasoning is the first 2 KB of the reasoning the model gave with the answer, and ReasoningBytes its full size.
	// The conversation keeps the whole reasoning; only this record is cut.
	Reasoning      string
	ReasoningBytes int
}

// AgentAttempt is one failed attempt of a model call. Kind, Status, RequestBytes and RequestHash come from the
// *ModelError, and are empty for another error. SameRequest is set when the attempt sent a body with the same hash as
// the failed attempt before it, so a refusal that repeats on an identical body shows at once. Wait is the pause before
// the next attempt, read from the retry rule; it is zero when no attempt followed.
type AgentAttempt struct {
	Kind         ModelErrorKind
	Status       int
	RequestBytes int
	RequestHash  string
	SameRequest  bool
	Wait         time.Duration
}

// AgentCallBrief is one tool call in the bounded transcript: its name and the size of its arguments in bytes.
type AgentCallBrief struct {
	Name          string
	ArgumentBytes int
}

// AgentResult is the typed end of a run. It is always filled, also when Run returns an error.
type AgentResult struct {
	Stop AgentStop
	// FinalText is the text of the last model answer.
	FinalText string
	// Submission is set only when a final tool accepted an answer.
	Submission *AgentSubmission
	// Turns holds one entry per model answer, in order.
	Turns []AgentTurn
	// Refusals is the number of refused final answers, and Nudges the number of times the loop asked again after an
	// answer with no tool call.
	Refusals int
	Nudges   int
	// Usage and CostUSD are the sums over Turns.
	Usage   ModelUsage
	CostUSD float64
	// Transcript is the whole conversation, starting with the caller's messages.
	Transcript []ModelMessage
	// Retries is the number of model calls the loop sent again after a failure, over the whole run.
	Retries int
	// FailedCall holds the failed attempts of the model call that ended the run, and is nil when no failed call
	// ended it.
	FailedCall []AgentAttempt
}
