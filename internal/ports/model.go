package ports

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
// instead of text, and a tool message names its call in ToolCallID. Reasoning is the reasoning the model gave with an
// assistant answer; the caller keeps it on that message, so the adapter can send it back (AM12d).
type ModelMessage struct {
	Role       ModelRole
	Content    string
	ToolCalls  []ModelToolCall
	ToolCallID string
	// Reasoning is left out of the JSON form when empty, so the size of a request that callers measure with
	// json.Marshal (the loop's worst-case guard) grows only by the reasoning a message holds.
	Reasoning ModelReasoning `json:",omitzero"`
}

// ModelReasoning is the reasoning a model gave with one answer. It is opaque to the caller: the adapter fills it from
// the answer and sends it back unchanged with the same assistant message in later calls. Some endpoints refuse a call
// whose earlier assistant turns lack the reasoning they gave (DeepSeek thinking mode with tools), so a caller never
// drops, edits or cuts it.
//
// Text is the reasoning text. Field is the adapter's name for where the text came from, so the adapter sends it back
// in the same place; it is empty when the answer had no reasoning field, and set with an empty Text when the field came
// empty, so the same empty field goes back. Details is the provider's structured reasoning, kept as the
// raw bytes it sent and echoed back byte for byte; nil when there is none.
type ModelReasoning struct {
	Text    string
	Field   string
	Details json.RawMessage
}

// IsZero reports whether the answer carried no reasoning.
func (r ModelReasoning) IsZero() bool { return r.Text == "" && r.Field == "" && len(r.Details) == 0 }

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

// ModelToolChoice says whether the model must call a tool. Empty means the provider's default.
type ModelToolChoice string

// The tool choices a caller can ask for.
const (
	// ModelToolChoiceAuto lets the model answer with text or with a tool call.
	ModelToolChoiceAuto ModelToolChoice = "auto"
	// ModelToolChoiceRequired asks the endpoint to force a tool call. An adapter sends it only to an endpoint that is
	// known to take it, and falls back to auto once when the endpoint refuses it.
	ModelToolChoiceRequired ModelToolChoice = "required"
)

// ModelRequest is one chat-completion call. MaxOutputTokens caps the answer and is zero for no explicit cap.
// ToolChoice is empty for the provider's default. Consumer: the Action-mode agent loop (internal/app/agentloop).
type ModelRequest struct {
	Model           string
	Messages        []ModelMessage
	Tools           []ModelTool
	MaxOutputTokens int
	ToolChoice      ModelToolChoice
}

// ModelResponse is the model's answer: text, or the tool calls it wants run, or both. CostUSD is computed from
// Usage and the caller's price table, so the loop can hold a budget. ToolChoice is the choice the adapter sent, empty
// for none. Fallback is set when the endpoint refused the requested choice and the adapter sent auto instead: it holds
// the refusal, without the key or the request body.
//
// Reasoning is the reasoning the model gave with the answer. The caller stores it on the assistant message it appends.
type ModelResponse struct {
	Content    string
	Reasoning  ModelReasoning `json:",omitzero"`
	ToolCalls  []ModelToolCall
	Finish     ModelFinish
	Usage      ModelUsage
	CostUSD    float64
	ToolChoice ModelToolChoice
	Fallback   string
}

// ModelChat is one chat-completion endpoint with tool calling. Consumer: the Action-mode agent loop. Adapter:
// internal/adapters/openrouter (AM10).
type ModelChat interface {
	// Complete sends the conversation and the tool definitions and returns the model's answer. On an error the
	// response is empty, unless the provider billed the failed call: then CostUSD and Usage say what it billed, and the
	// caller counts that once.
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

// ModelError is a model endpoint failure. Status, ContentType and Message come from the provider answer and hold no
// credential; Message is a bounded part of the answer body. RequestBytes is the size of the request body that was
// refused, never its content. RequestHash is the hex SHA-256 of that body, so a caller can tell a retry of the same
// body from a new one; it holds nothing a reader can turn back into the body. Unwrap maps the kind to ErrUnauthorized or ErrUnavailable, so errors.Is works for the
// retry decision.
type ModelError struct {
	Kind         ModelErrorKind
	Status       int
	ContentType  string
	RequestBytes int
	RequestHash  string
	RetryAfter   time.Duration
	Message      string
}

// Error renders the failure without the endpoint, the key or the conversation.
func (e *ModelError) Error() string {
	msg := fmt.Sprintf("model %s", e.Kind)
	var facts []string
	if e.Status != 0 {
		facts = append(facts, fmt.Sprintf("HTTP %d", e.Status))
	}
	if e.ContentType != "" {
		facts = append(facts, e.ContentType)
	}
	if e.RequestBytes > 0 {
		facts = append(facts, fmt.Sprintf("request %d bytes", e.RequestBytes))
	}
	if len(facts) > 0 {
		msg += " (" + strings.Join(facts, ", ") + ")"
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
