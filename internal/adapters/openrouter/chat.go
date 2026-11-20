package openrouter

import (
	"bytes"
	"context"
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
type ChatClient struct {
	BaseURL string
	Secrets Secrets
	KeyName string
	Headers map[string]string
	HTTP    *http.Client
	Prices  map[string]Price
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
	body, err := chatRequestBody(req)
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
		return ports.ModelResponse{}, chatStatusError(resp, raw)
	}
	return chatAnswer(raw, req, c.Prices)
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

// chatMessage is the wire form of ports.ModelMessage. Content is always sent, empty for an assistant tool call.
type chatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
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
	Model     string        `json:"model"`
	Messages  []chatMessage `json:"messages"`
	Tools     []chatTool    `json:"tools,omitempty"`
	MaxTokens int           `json:"max_tokens,omitempty"`
}

func chatRequestBody(req ports.ModelRequest) ([]byte, error) {
	body := chatRequestBodyWire{
		Model:     req.Model,
		Messages:  make([]chatMessage, 0, len(req.Messages)),
		Tools:     make([]chatTool, 0, len(req.Tools)),
		MaxTokens: req.MaxOutputTokens,
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
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode model chat request: %w", err)
	}
	return raw, nil
}

func chatWireMessage(m ports.ModelMessage) chatMessage {
	msg := chatMessage{Role: string(m.Role), Content: m.Content, ToolCallID: m.ToolCallID}
	for _, call := range m.ToolCalls {
		msg.ToolCalls = append(msg.ToolCalls, chatToolCall{
			ID:       call.ID,
			Type:     "function",
			Function: chatToolFunction{Name: call.Name, Arguments: call.Arguments},
		})
	}
	return msg
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
			Content   string `json:"content"`
			ToolCalls []struct {
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
		Content: strings.TrimSpace(choice.Message.Content),
		Finish:  chatFinish(choice.FinishReason),
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

func chatStatusError(resp *http.Response, raw []byte) error {
	var body chatErrorBody
	decoded := json.Unmarshal(raw, &body) == nil
	message, code := "", ""
	if decoded {
		message, code = strings.TrimSpace(body.Error.Message), chatErrorCode(body.Error.Code)
	}
	if message == "" {
		message = chatErrorSnippet(raw)
	}
	modelErr := &ports.ModelError{Status: resp.StatusCode, Message: message}
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
