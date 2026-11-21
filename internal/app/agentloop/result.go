package agentloop

import "github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"

// StopReason says why the loop ended. The proof outcome (AM5) maps each value to a verdict and a reason.
type StopReason string

// The ways a run ends.
const (
	// StopSubmitted means a Submitter accepted a final answer. Result.Submission holds it.
	StopSubmitted StopReason = "submitted"
	// StopRefused means the model's retry was refused too, so the run gave up on a valid submission.
	StopRefused StopReason = "refused"
	// StopBudget means the next model call could pass Config.BudgetUSD, Config.InputTokenCap or Config.OutputTokenCap,
	// so it was not made. It also ends a run whose model reported a cost or a token count that cannot be true; Run
	// then returns an error that wraps ports.ErrAgentUsage.
	StopBudget StopReason = "budget"
	// StopDeadline means the clock reached Config.Deadline.
	StopDeadline StopReason = "deadline"
	// StopTurnCap means the model used Config.MaxTurns answers without a final submission.
	StopTurnCap StopReason = "turn_cap"
	// StopNoSubmission means the model answered with text and no tool call after the loop had asked again twice.
	StopNoSubmission StopReason = "no_submission"
	// StopCancelled means the caller's context ended. Run also returns the context error.
	StopCancelled StopReason = "cancelled"
	// StopModelError means the model call failed for another reason. Run also returns that error.
	StopModelError StopReason = "model_error"
)

// Submission is the accepted final answer: the tool the model called and the JSON arguments it wrote.
type Submission struct {
	Tool      string
	Arguments string
}

// Result is the typed end of a run. It is always filled, also when Run returns an error.
type Result struct {
	Reason StopReason
	// CostUSD is the sum of the cost of every model answer, as the model port reports it.
	CostUSD float64
	// Turns is the number of model answers received.
	Turns int
	// Refusals is the number of refused submissions.
	Refusals int
	// Nudges is the number of times the loop asked again after an answer with no tool call.
	Nudges int
	// Usage is the token count of every model answer, added up.
	Usage ports.ModelUsage
	// Turn holds the usage, cost and latency of each model answer, in order.
	Turn []ports.AgentTurn
	// Submission is set only when Reason is StopSubmitted.
	Submission *Submission
	// LastText is the text of the last model answer, for a run that ended without a submission.
	LastText string
	// Transcript is the whole conversation, starting with the caller's messages.
	Transcript []ports.ModelMessage
	// Retries is the number of model calls sent again after a failure, over the whole run.
	Retries int
	// FailedCall holds the failed attempts of the model call that ended the run, nil when none did.
	FailedCall []ports.AgentAttempt
}
