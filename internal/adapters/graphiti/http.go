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
	"sync"
	"sync/atomic"
	"time"
)

// sessionHeader carries the MCP session id that the streamable HTTP server issues on initialize.
const sessionHeader = "Mcp-Session-Id"

// errSessionLost marks a reply that refused the session, so the caller opens a new one and tries once more.
var errSessionLost = errors.New("graphiti session is not accepted")

const maxGraphitiResponse = 8 << 20

// HTTPCaller speaks MCP streamable HTTP to the internal Graphiti endpoint.
type HTTPCaller struct {
	endpoint string
	client   *http.Client
	next     atomic.Uint64

	mu      sync.Mutex
	opened  bool
	session string
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

// Call performs one JSON-RPC tools/call without logging query text or server response bodies. It opens an MCP
// session first (initialize, then notifications/initialized), because the Graphiti server refuses a call without one
// ("Missing session ID", seen live). A refused session is opened again once.
func (c *HTTPCaller) Call(ctx context.Context, name string, args map[string]any) (json.RawMessage, error) {
	for attempt := 0; ; attempt++ {
		session, err := c.ensureSession(ctx)
		if err != nil {
			return nil, err
		}
		id := c.next.Add(1)
		raw, err := c.post(ctx, session, rpc{JSONRPC: "2.0", ID: &id, Method: "tools/call", Params: map[string]any{"name": name, "arguments": args}})
		if errors.Is(err, errSessionLost) && attempt == 0 {
			c.dropSession(session)
			continue
		}
		if err != nil {
			return nil, err
		}
		return decodeToolReply(raw, id)
	}
}

// rpc is one JSON-RPC message. A notification has no id.
type rpc struct {
	JSONRPC string  `json:"jsonrpc"`
	ID      *uint64 `json:"id,omitempty"`
	Method  string  `json:"method"`
	Params  any     `json:"params,omitempty"`
}

// ensureSession returns the open session id, opening a session when there is none.
func (c *HTTPCaller) ensureSession(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.opened {
		return c.session, nil
	}
	id := c.next.Add(1)
	init := rpc{JSONRPC: "2.0", ID: &id, Method: "initialize", Params: map[string]any{
		"protocolVersion": "2025-03-26", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "heimdall", "version": "1"},
	}}
	session, _, err := c.send(ctx, "", init)
	if err != nil {
		return "", fmt.Errorf("open graphiti session: %w", err)
	}
	// A server without sessions names none; calls then go without the header.
	if _, _, err := c.send(ctx, session, rpc{JSONRPC: "2.0", Method: "notifications/initialized"}); err != nil {
		return "", fmt.Errorf("confirm graphiti session: %w", err)
	}
	c.opened, c.session = true, session
	return session, nil
}

// dropSession forgets a session the server refused, unless another call already replaced it.
func (c *HTTPCaller) dropSession(session string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.opened && c.session == session {
		c.opened, c.session = false, ""
	}
}

// post sends one request inside a session and returns the JSON-RPC reply body.
func (c *HTTPCaller) post(ctx context.Context, session string, msg rpc) (json.RawMessage, error) {
	_, raw, err := c.send(ctx, session, msg)
	return raw, err
}

// send posts one message and returns the session id the server named and the reply body, unwrapped from an event
// stream when the server streams it. A 400 or 404 inside a session means the session is gone.
func (c *HTTPCaller) send(ctx context.Context, session string, msg rpc) (string, json.RawMessage, error) {
	body, err := json.Marshal(msg)
	if err != nil {
		return "", nil, fmt.Errorf("encode graphiti request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", nil, fmt.Errorf("create graphiti request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if session != "" {
		req.Header.Set(sessionHeader, session)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return "", nil, errors.New("graphiti endpoint is unavailable")
	}
	defer func() { _ = resp.Body.Close() }()
	if session != "" && (resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusNotFound) {
		return "", nil, errSessionLost
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", nil, fmt.Errorf("graphiti HTTP status %d", resp.StatusCode)
	}
	raw, err := readReply(resp, msg.ID != nil)
	if err != nil {
		return "", nil, err
	}
	return resp.Header.Get(sessionHeader), raw, nil
}

// readReply reads a bounded reply body, unwrapped from an event stream when a request with an id gets one.
func readReply(resp *http.Response, wantsReply bool) (json.RawMessage, error) {
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxGraphitiResponse+1))
	if err != nil {
		return nil, errors.New("read graphiti response")
	}
	if len(raw) > maxGraphitiResponse {
		return nil, errors.New("graphiti response exceeds 8 MiB")
	}
	if wantsReply && strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		return lastSSEData(raw)
	}
	return raw, nil
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
