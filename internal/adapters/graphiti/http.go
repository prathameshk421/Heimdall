package graphiti

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

const maxGraphitiResponse = 8 << 20

// HTTPCaller speaks MCP streamable HTTP to the internal Graphiti endpoint.
type HTTPCaller struct {
	endpoint string
	client   *http.Client
	next     atomic.Uint64
}

// NewHTTPCaller validates the internal Graphiti endpoint and applies a bounded default client.
func NewHTTPCaller(endpoint string, client *http.Client) (*HTTPCaller, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("graphiti endpoint must be an absolute HTTP URL")
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &HTTPCaller{endpoint: endpoint, client: client}, nil
}

// Call performs one JSON-RPC tools/call without logging query text or server response bodies.
func (c *HTTPCaller) Call(ctx context.Context, name string, args map[string]any) (json.RawMessage, error) {
	id := c.next.Add(1)
	request := struct {
		JSONRPC string `json:"jsonrpc"`
		ID      uint64 `json:"id"`
		Method  string `json:"method"`
		Params  any    `json:"params"`
	}{"2.0", id, "tools/call", map[string]any{"name": name, "arguments": args}}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encode graphiti request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create graphiti request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, errors.New("graphiti endpoint is unavailable")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("graphiti HTTP status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxGraphitiResponse+1))
	if err != nil {
		return nil, errors.New("read graphiti response")
	}
	if len(raw) > maxGraphitiResponse {
		return nil, errors.New("graphiti response exceeds 8 MiB")
	}
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		raw, err = lastSSEData(raw)
		if err != nil {
			return nil, err
		}
	}
	return decodeToolReply(raw, id)
}

func lastSSEData(raw []byte) ([]byte, error) {
	var result []byte
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 1024), maxGraphitiResponse)
	for scanner.Scan() {
		line := scanner.Text()
		if data, ok := strings.CutPrefix(line, "data: "); ok {
			result = []byte(data)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, errors.New("parse graphiti event stream")
	}
	if len(result) == 0 {
		return nil, errors.New("graphiti event stream had no data")
	}
	return result, nil
}

func decodeToolReply(raw []byte, wantID uint64) (json.RawMessage, error) {
	var reply struct {
		JSONRPC string `json:"jsonrpc"`
		ID      uint64 `json:"id"`
		Error   *struct {
			Message string `json:"message"`
		} `json:"error"`
		Result struct {
			IsError    bool            `json:"isError"`
			Structured json.RawMessage `json:"structuredContent"`
			Content    []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return nil, errors.New("decode graphiti response")
	}
	if reply.ID != wantID {
		return nil, errors.New("graphiti response id does not match request")
	}
	if reply.Error != nil || reply.Result.IsError {
		return nil, errors.New("graphiti tool call failed")
	}
	if len(reply.Result.Structured) > 0 {
		return reply.Result.Structured, nil
	}
	if len(reply.Result.Content) > 0 {
		for _, item := range reply.Result.Content {
			if item.Text != "" {
				return json.RawMessage(item.Text), nil
			}
		}
	}
	return nil, errors.New("graphiti response has no result content")
}
