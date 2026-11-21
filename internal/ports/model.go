package ports

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// ModelRole is who wrote a model message.
type ModelRole string

// The message roles an OpenAI-compatible chat accepts.
const (
	ModelRoleSystem    ModelRole = "system"
	ModelRoleUser      ModelRole = "user"
	ModelRoleAssistant ModelRole = "assistant"
	// ModelRoleTool carries the result of one tool call back to the model. ToolCallID names the call.
	ModelRoleTool ModelRole = "tool"
)

// ModelMessage is one message in a model conversation. Content is the text. An assistant message may carry ToolCalls
// instead of text, and a tool message names its call in ToolCallID.
type ModelMessage struct {
	Role       ModelRole
	Content    string
	ToolCalls  []ModelToolCall
	ToolCallID string
}

// ModelTool is one tool the model may call. Parameters is the tool's JSON schema for its arguments.
type ModelTool struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

// ModelToolCall is one tool call the model asks for. Arguments is the raw JSON object the model wrote, so the caller
// decodes it against the tool's schema. ID is the provider's id for the call, which the tool result must repeat.
type ModelToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// ModelUsage is the token count of one call. InputTokens includes CachedInputTokens; the cache read is billed
// separately by most providers.
type ModelUsage struct {
	InputTokens       int64
	CachedInputTokens int64
	OutputTokens      int64
}

// ModelFinish says why the model stopped.
type ModelFinish string

// The finish reasons a caller acts on. A provider reason outside these is ModelFinishOther.
const (
	ModelFinishStop          ModelFinish = "stop"
	ModelFinishToolCalls     ModelFinish = "tool_calls"
	ModelFinishLength        ModelFinish = "length"
	ModelFinishContentFilter ModelFinish = "content_filter"
	ModelFinishOther         ModelFinish = "other"
)

// ModelRequest is one chat-completion call. MaxOutputTokens caps the answer and is zero for no explicit cap.
// Consumer: the Action-mode agent loop (internal/app/agentloop).
type ModelRequest struct {
	Model           string
	Messages        []ModelMessage
	Tools           []ModelTool
	MaxOutputTokens int
}

// ModelResponse is the model's answer: text, or the tool calls it wants run, or both. CostUSD is computed from
// Usage and the caller's price table, so the loop can hold a budget.
type ModelResponse struct {
	Content   string
	ToolCalls []ModelToolCall
	Finish    ModelFinish
	Usage     ModelUsage
	CostUSD   float64
}

// ModelChat is one chat-completion endpoint with tool calling. Consumer: the Action-mode agent loop. Adapter:
// internal/adapters/openrouter (AM10).
type ModelChat interface {
	// Complete sends the conversation and the tool definitions and returns the model's answer.
	Complete(ctx context.Context, req ModelRequest) (ModelResponse, error)
}

// ModelErrorKind says why a model endpoint failed. A caller branches on the kind, so it never sees an HTTP status.
type ModelErrorKind string

// The failure kinds of a model call.
const (
	// ModelErrorRateLimited means the provider asks the caller to wait. RetryAfter carries its hint, or zero.
	ModelErrorRateLimited ModelErrorKind = "rate_limited"
	// ModelErrorRefused means the provider refused the content, by its content policy or its own filter.
	ModelErrorRefused ModelErrorKind = "refused"
	// ModelErrorContextTooLong means the conversation exceeds the model's context window.
	ModelErrorContextTooLong ModelErrorKind = "context_too_long"
	// ModelErrorAuth means the key was refused, or the account cannot pay. Retrying cannot fix it.
	ModelErrorAuth ModelErrorKind = "auth"
	// ModelErrorServer means the provider failed on its side and a later attempt may work.
	ModelErrorServer ModelErrorKind = "server"
	// ModelErrorInvalid means the provider rejected the request for another reason.
	ModelErrorInvalid ModelErrorKind = "invalid"
)

// ModelError is a model endpoint failure. Status and Message come from the provider answer and hold no credential.
// Unwrap maps the kind to ErrUnauthorized or ErrUnavailable, so errors.Is works for the retry decision.
type ModelError struct {
	Kind       ModelErrorKind
	Status     int
	RetryAfter time.Duration
	Message    string
}

// Error renders the failure without the endpoint, the key or the conversation.
func (e *ModelError) Error() string {
	msg := fmt.Sprintf("model %s", e.Kind)
	if e.Status != 0 {
		msg += fmt.Sprintf(" (HTTP %d)", e.Status)
	}
	if e.Message != "" {
		msg += ": " + e.Message
	}
	if e.RetryAfter > 0 {
		msg += fmt.Sprintf(" (retry after %s)", e.RetryAfter)
	}
	return msg
}

// Unwrap maps the kind to the sentinel errors callers already use for retries.
func (e *ModelError) Unwrap() error {
	switch e.Kind {
	case ModelErrorAuth:
		return ErrUnauthorized
	case ModelErrorRateLimited, ModelErrorServer:
		return ErrUnavailable
	case ModelErrorRefused, ModelErrorContextTooLong, ModelErrorInvalid:
		return nil
	}
	return nil
}
