package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// RoomDigestClient summarizes already redacted Switch room messages using the extraction model in trusted policy.
type RoomDigestClient struct {
	BaseURL string
	Secrets Secrets
	KeyName string
	HTTP    *http.Client
}

// SummarizeRoom sends the redacted room messages to model and returns the trimmed summary text.
func (c RoomDigestClient) SummarizeRoom(ctx context.Context, model string, messages []ports.Message) (string, error) {
	if c.Secrets == nil || model == "" || len(messages) == 0 {
		return "", errors.New("room digest model configuration is incomplete")
	}
	key, err := c.Secrets.Get(c.KeyName)
	if err != nil || strings.TrimSpace(key) == "" {
		return "", errors.New("room digest OpenRouter credential is unavailable")
	}
	body, err := roomDigestRequestBody(model, messages)
	if err != nil {
		return "", err
	}
	raw, err := c.post(ctx, key, body)
	if err != nil {
		return "", err
	}
	return roomDigestText(raw)
}

func roomDigestRequestBody(model string, messages []ports.Message) ([]byte, error) {
	input, err := json.Marshal(messages)
	if err != nil {
		return nil, fmt.Errorf("encode redacted room messages: %w", err)
	}
	body, err := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": "Summarize the supplied change-room messages as concise engineering context. Include questions and answers, commands, and findings context. Do not invent facts, actors, or outcomes. Return only the summary text."},
			{"role": "user", "content": string(input)},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("encode room digest request: %w", err)
	}
	return body, nil
}

// post performs the chat completion call and returns the raw body of a successful (2xx) response.
func (c RoomDigestClient) post(ctx context.Context, key string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build room digest request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call room digest model: %w", ports.ErrUnavailable)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read room digest response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("room digest model returned HTTP %d", resp.StatusCode)
	}
	return raw, nil
}

func (c RoomDigestClient) endpoint() string {
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = "https://openrouter.ai/api/v1"
	}
	return base + "/chat/completions"
}

func roomDigestText(raw []byte) (string, error) {
	var response struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &response); err != nil || len(response.Choices) != 1 {
		return "", errors.New("room digest model returned an invalid response")
	}
	text := strings.TrimSpace(response.Choices[0].Message.Content)
	if text == "" {
		return "", errors.New("room digest model returned an empty summary")
	}
	return text, nil
}
