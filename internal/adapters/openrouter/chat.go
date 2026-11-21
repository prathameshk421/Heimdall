package openrouter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

var _ ports.ModelChat = ChatClient{}

// chatTimeout caps one completion call. The caller's context deadline still wins when it is sooner.
const chatTimeout = 2 * time.Minute

// maxChatResponseBytes bounds the provider answer. A longer body is refused unread, so a hostile endpoint cannot make
// the loop hold an unbounded string.
const maxChatResponseBytes = 4 << 20

// Price is the price in USD of one million tokens of a model. CachedInput is the cache-read price; zero means the
// provider charges nothing for a cache read. A model with no entry in ChatClient.Prices reports zero cost.
type Price struct {
	Input       float64
	CachedInput float64
	Output      float64
}

// ChatClient sends chat completions to an OpenAI-compatible endpoint, with tool calling. It holds no key: KeyName
// names the secret and Secrets reads it on each call. Empty KeyName sends no Authorization header, for a local
// endpoint. Headers are extra, non-secret request headers. Prices bills each answer from its usage.
//
// RequiredToolChoice is the capability flag for tool_choice "required". When it is false, a request that asks for a
// required tool call is sent with "auto", because an endpoint that does not know the value may refuse the whole call.
// When it is true and the endpoint still refuses the call as a bad request, the client sends the same request once more
// with "auto" and records the first refusal in ModelResponse.Fallback.
type ChatClient struct {
	BaseURL            string
	Secrets            Secrets
	KeyName            string
	Headers            map[string]string
	HTTP               *http.Client
	Prices             map[string]Price
	RequiredToolChoice bool
}

// Complete implements ports.ModelChat over POST /chat/completions.
func (c ChatClient) Complete(parent context.Context, req ports.ModelRequest) (ports.ModelResponse, error) {
	if strings.TrimSpace(req.Model) == "" || len(req.Messages) == 0 {
		return ports.ModelResponse{}, errors.New("model chat request is incomplete")
	}
	key, err := bearerKey(c.Secrets, c.KeyName)
	if err != nil {
		return ports.ModelResponse{}, errors.New("model chat credential is unavailable")
	}
	choice := c.toolChoice(req)
	answer, err := c.send(parent, req, key, choice)
	if choice != ports.ModelToolChoiceRequired || !refusedRequest(err) {
		return answer, err
	}
	answer, retryErr := c.send(parent, req, key, ports.ModelToolChoiceAuto)
	if retryErr != nil {
		return answer, fmt.Errorf("model chat fell back to tool_choice auto after tool_choice required was refused (%s): %w", err.Error(), retryErr)
	}
	answer.Fallback = "tool_choice required refused: " + err.Error()
	return answer, nil
}

// toolChoice is the choice the client sends: none without tools or without a choice in the request, and auto in place
// of required for an endpoint that is not known to take it.
func (c ChatClient) toolChoice(req ports.ModelRequest) ports.ModelToolChoice {
	if len(req.Tools) == 0 || req.ToolChoice == "" {
		return ""
	}
	if req.ToolChoice == ports.ModelToolChoiceRequired && !c.RequiredToolChoice {
		return ports.ModelToolChoiceAuto
	}
	return req.ToolChoice
}

// refusedRequest reports whether the endpoint refused the call as a bad request it could not take. A context that is
// too long, a content filter, auth, a rate limit and a server error are not refusals of the tool choice.
func refusedRequest(err error) bool {
	var modelErr *ports.ModelError
	if !errors.As(err, &modelErr) || modelErr.Kind != ports.ModelErrorInvalid {
		return false
	}
	return modelErr.Status == http.StatusBadRequest || modelErr.Status == http.StatusUnprocessableEntity
}

// send makes one call with the given tool choice.
func (c ChatClient) send(parent context.Context, req ports.ModelRequest, key string, choice ports.ModelToolChoice) (ports.ModelResponse, error) {
	body, err := chatRequestBody(req, choice)
	if err != nil {
		return ports.ModelResponse{}, err
	}
	ctx, cancel := context.WithTimeout(parent, chatTimeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(), bytes.NewReader(body))
	if err != nil {
		return ports.ModelResponse{}, fmt.Errorf("build model chat request: %w", err)
	}
	setModelHeaders(httpReq, key, c.Headers)
	resp, err := c.client().Do(httpReq)
	if err != nil {
		return ports.ModelResponse{}, fmt.Errorf("call model chat: %w", errors.Join(ports.ErrUnavailable, err))
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := readChatResponse(resp.Body)
	if err != nil {
		return ports.ModelResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ports.ModelResponse{}, chatStatusError(resp, raw, body)
	}
	answer, err := chatAnswer(raw, req, c.Prices)
	answer.ToolChoice = choice
	return answer, err
}

func (c ChatClient) endpoint() string {
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = "https://openrouter.ai/api/v1"
	}
	return base + "/chat/completions"
}

func (c ChatClient) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: chatTimeout}
}

// The wire fields that carry an answer's reasoning (ports.ModelReasoning.Field holds one of them). DeepSeek and the
// endpoints that serve it use reasoning_content; OpenRouter uses reasoning, and may add reasoning_details.
const (
	fieldReasoningContent = "reasoning_content"
	fieldReasoning        = "reasoning"
)

// chatMessage is the wire form of ports.ModelMessage. Content is always sent, empty for an assistant tool call. An
// assistant message sends its reasoning back in the field it came in, and reasoning_details unchanged. DeepSeek
// thinking mode with tools: "the reasoning_content must be fully passed back to the API in all subsequent requests",
// else "the API will return a 400 error" (https://api-docs.deepseek.com/guides/thinking_mode). The doc never asks for
// an empty value on a turn without reasoning, so such a turn sends none.
type chatMessage struct {
	Role             string          `json:"role"`
	Content          string          `json:"content"`
	ReasoningContent *string         `json:"reasoning_content,omitempty"`
	Reasoning        *string         `json:"reasoning,omitempty"`
	ReasoningDetails json.RawMessage `json:"reasoning_details,omitempty"`
	ToolCalls        []chatToolCall  `json:"tool_calls,omitempty"`
	ToolCallID       string          `json:"tool_call_id,omitempty"`
}

type chatToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function chatToolFunction `json:"function"`
}

type chatToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Arguments   string          `json:"arguments,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type chatTool struct {
	Type     string           `json:"type"`
	Function chatToolFunction `json:"function"`
}

type chatRequestBodyWire struct {
	Model      string        `json:"model"`
	Messages   []chatMessage `json:"messages"`
	Tools      []chatTool    `json:"tools,omitempty"`
	ToolChoice string        `json:"tool_choice,omitempty"`
	MaxTokens  int           `json:"max_tokens,omitempty"`
}

// chatRequestBody is the wire body of one call. choice is the tool_choice to send, empty for none.
func chatRequestBody(req ports.ModelRequest, choice ports.ModelToolChoice) ([]byte, error) {
	body := chatRequestBodyWire{
		Model:      req.Model,
		Messages:   make([]chatMessage, 0, len(req.Messages)),
		Tools:      make([]chatTool, 0, len(req.Tools)),
		ToolChoice: string(choice),
		MaxTokens:  req.MaxOutputTokens,
	}
	for _, m := range req.Messages {
		body.Messages = append(body.Messages, chatWireMessage(m))
	}
	for _, tool := range req.Tools {
		body.Tools = append(body.Tools, chatTool{
			Type: "function",
			Function: chatToolFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.Parameters,
			},
		})
	}
	// The encoder does not escape <, > and &, so reasoning_details goes back with the bytes the provider sent.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(body); err != nil {
		return nil, fmt.Errorf("encode model chat request: %w", err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

func chatWireMessage(m ports.ModelMessage) chatMessage {
	msg := chatMessage{Role: string(m.Role), Content: m.Content, ToolCallID: m.ToolCallID}
	if m.Role == ports.ModelRoleAssistant {
		setWireReasoning(&msg, m.Reasoning)
	}
	for _, call := range m.ToolCalls {
		msg.ToolCalls = append(msg.ToolCalls, chatToolCall{
			ID:       call.ID,
			Type:     "function",
			Function: chatToolFunction{Name: call.Name, Arguments: call.Arguments},
		})
	}
	return msg
}

// setWireReasoning puts the reasoning back in the field it came in, the empty text too. The text is never cut: a cut
// value may be refused.
func setWireReasoning(msg *chatMessage, r ports.ModelReasoning) {
	text := r.Text
	switch r.Field {
	case fieldReasoningContent:
		msg.ReasoningContent = &text
	case fieldReasoning:
		msg.Reasoning = &text
	}
	if len(r.Details) > 0 {
		msg.ReasoningDetails = r.Details
	}
}

// answerReasoning reads the reasoning of an answer. reasoning_content wins when an endpoint sends both text fields,
// since DeepSeek reads that one back. A null field is no field. reasoning_details is kept compact, which is the form
// the request encoder writes, so it goes back byte for byte.
func answerReasoning(content, reasoning *string, details json.RawMessage) ports.ModelReasoning {
	var out ports.ModelReasoning
	switch {
	case content != nil:
		out.Text, out.Field = *content, fieldReasoningContent
	case reasoning != nil:
		out.Text, out.Field = *reasoning, fieldReasoning
	}
	trimmed := bytes.TrimSpace(details)
	if len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) {
		var compact bytes.Buffer
		if json.Compact(&compact, trimmed) == nil {
			out.Details = compact.Bytes()
		}
	}
	return out
}

// readChatResponse reads at most maxChatResponseBytes and refuses anything longer.
func readChatResponse(r io.Reader) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(r, maxChatResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read model chat response: %w", err)
	}
	if len(raw) > maxChatResponseBytes {
		return nil, errors.New("model chat response is too large")
	}
	return raw, nil
}

// chatAnswerBody is the parts of the OpenAI-compatible answer this adapter uses.
type chatAnswerBody struct {
	Choices []struct {
		Message struct {
			Content          string          `json:"content"`
			ReasoningContent *string         `json:"reasoning_content"`
			Reasoning        *string         `json:"reasoning"`
			ReasoningDetails json.RawMessage `json:"reasoning_details"`
			ToolCalls        []struct {
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens        int64 `json:"prompt_tokens"`
		CompletionTokens    int64 `json:"completion_tokens"`
		PromptTokensDetails struct {
			CachedTokens int64 `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
}

func chatAnswer(raw []byte, req ports.ModelRequest, prices map[string]Price) (ports.ModelResponse, error) {
	var body chatAnswerBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return ports.ModelResponse{}, errors.New("model chat returned an invalid response")
	}
	if len(body.Choices) == 0 {
		return ports.ModelResponse{}, errors.New("model chat returned no choices")
	}
	choice := body.Choices[0]
	answer := ports.ModelResponse{
		Content:   strings.TrimSpace(choice.Message.Content),
		Reasoning: answerReasoning(choice.Message.ReasoningContent, choice.Message.Reasoning, choice.Message.ReasoningDetails),
		Finish:    chatFinish(choice.FinishReason),
		Usage: ports.ModelUsage{
			InputTokens:       body.Usage.PromptTokens,
			CachedInputTokens: body.Usage.PromptTokensDetails.CachedTokens,
			OutputTokens:      body.Usage.CompletionTokens,
		},
	}
	for _, call := range choice.Message.ToolCalls {
		answer.ToolCalls = append(answer.ToolCalls, ports.ModelToolCall{
			ID:        call.ID,
			Name:      call.Function.Name,
			Arguments: call.Function.Arguments,
		})
	}
	answer.CostUSD = chatCost(answer.Usage, prices[req.Model])
	return answer, nil
}

func chatFinish(reason string) ports.ModelFinish {
	switch finish := ports.ModelFinish(reason); finish {
	case ports.ModelFinishStop, ports.ModelFinishToolCalls, ports.ModelFinishLength, ports.ModelFinishContentFilter, ports.ModelFinishOther:
		return finish
	default:
		return ports.ModelFinishOther
	}
}

// chatCost bills a cache read at the cached price and the rest of the input at the input price. A provider that
// reports more cached tokens than input tokens is billed for the cached count only, never a negative amount.
func chatCost(usage ports.ModelUsage, price Price) float64 {
	fresh := usage.InputTokens - usage.CachedInputTokens
	if fresh < 0 {
		fresh = 0
	}
	total := float64(fresh)*price.Input + float64(usage.CachedInputTokens)*price.CachedInput + float64(usage.OutputTokens)*price.Output
	return total / 1e6
}

// chatErrorBody is the error envelope OpenAI-compatible endpoints share. Code is a string on most and a number on a
// few, so it is decoded as any and rendered as text.
type chatErrorBody struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    any    `json:"code"`
	} `json:"error"`
}

// chatStatusError maps a refused call to a typed error. It records the status, the content type, the size of the
// request body and its SHA-256, never the body itself or the key, so a refusal such as an HTML "400 Bad request"
// explains itself, and a refusal that repeats on the same body shows as one.
func chatStatusError(resp *http.Response, raw, request []byte) error {
	var body chatErrorBody
	decoded := json.Unmarshal(raw, &body) == nil
	message, code := "", ""
	if decoded {
		message, code = strings.TrimSpace(body.Error.Message), chatErrorCode(body.Error.Code)
	}
	if message == "" {
		message = chatErrorSnippet(raw)
	}
	sum := sha256.Sum256(request)
	modelErr := &ports.ModelError{Status: resp.StatusCode, ContentType: chatContentType(resp.Header), RequestBytes: len(request),
		RequestHash: hex.EncodeToString(sum[:]), Message: message}
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		modelErr.Kind = ports.ModelErrorRateLimited
		modelErr.RetryAfter = retryAfter(resp.Header)
	case resp.StatusCode >= 500:
		modelErr.Kind = ports.ModelErrorServer
	case chatTextSays(code, "context_length", "context length"):
		modelErr.Kind = ports.ModelErrorContextTooLong
	case chatTextSays(code, "content_policy", "content_filter"), chatTextSays(message, "content policy", "content filter", "flagged"):
		modelErr.Kind = ports.ModelErrorRefused
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden, resp.StatusCode == http.StatusPaymentRequired:
		modelErr.Kind = ports.ModelErrorAuth
	default:
		modelErr.Kind = ports.ModelErrorInvalid
	}
	return modelErr
}

func chatErrorCode(code any) string {
	switch typed := code.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return ""
	}
}

// chatContentType is the answer's content type, cut to a bounded length.
func chatContentType(header http.Header) string {
	const limit = 128
	text := strings.TrimSpace(header.Get("Content-Type"))
	if len(text) > limit {
		text = text[:limit]
	}
	return text
}

// chatErrorSnippet is the fallback message for a body that is not the JSON envelope.
func chatErrorSnippet(raw []byte) string {
	const limit = 512
	text := strings.TrimSpace(string(raw))
	if len(text) > limit {
		text = text[:limit] + "..."
	}
	return text
}

// chatTextSays reports whether the message holds one of the phrases. The message and the phrases compare lowercase.
func chatTextSays(message string, phrases ...string) bool {
	lower := strings.ToLower(message)
	for _, phrase := range phrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

// retryAfter reads the provider's hint: delta seconds, or an HTTP date. An HTTP date is measured from the answer's
// own Date header, so no clock is read here. A missing or malformed hint is zero.
func retryAfter(header http.Header) time.Duration {
	raw := strings.TrimSpace(header.Get("Retry-After"))
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(raw); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	at, err := http.ParseTime(raw)
	if err != nil {
		return 0
	}
	sent, err := http.ParseTime(header.Get("Date"))
	if err != nil {
		return 0
	}
	if wait := at.Sub(sent); wait > 0 {
		return wait
	}
	return 0
}
