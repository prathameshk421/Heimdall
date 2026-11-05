package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// The protocol this agent speaks. The MCP edge answers every request with one JSON body (TECH_SPEC 14.1 and
// internal/transport/mcp/doc.go), so there is no stream to read.
const (
	protocolVersion = "2025-06-18"
	clientName      = "heimdall-testagent"
	clientVersion   = "0.1.0"
	// requestTimeout bounds one request, so a hung server cannot hold the agent forever.
	requestTimeout = 30 * time.Second
	// maxAnswerBytes matches the server's own body limit.
	maxAnswerBytes = 8 << 20
)

// client speaks JSON-RPC 2.0 to one MCP endpoint with one agent token.
type client struct {
	url    string
	token  string
	http   *http.Client
	nextID int
}

// newClient returns a client for the MCP endpoint at rawURL. The token stays in memory and is never put in an error
// or an output line.
func newClient(rawURL, token string) (*client, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse the server URL: %w", err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("the server URL %q is not an http or https URL", rawURL)
	}
	if token == "" {
		return nil, errors.New("the agent token must not be blank")
	}
	return &client{url: rawURL, token: token, http: &http.Client{}, nextID: 1}, nil
}

// rpcRequest is one JSON-RPC 2.0 request. A nil ID makes the message a notification.
type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int   `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// rpcReply is one JSON-RPC 2.0 answer: exactly one of Result and Error is set.
type rpcReply struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

// rpcError is the error object of an answer.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// toolResult is the result of tools/call. A refusal the agent can act on has IsError set and its text says why.
type toolResult struct {
	Content []textContent `json:"content"`
	IsError bool          `json:"isError"`
}

// textContent is one text part of a tool result.
type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// text joins the text parts of the result.
func (r toolResult) text() string {
	var b strings.Builder
	for _, part := range r.Content {
		b.WriteString(part.Text)
	}
	return b.String()
}

// connect performs the MCP handshake every client does before its first tool call: initialize, then the initialized
// notification.
func (c *client) connect(ctx context.Context) error {
	params := map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": clientName, "version": clientVersion},
	}
	if _, err := c.call(ctx, "initialize", params); err != nil {
		return fmt.Errorf("handshake: %w", err)
	}
	if err := c.notify(ctx, "notifications/initialized", map[string]any{}); err != nil {
		return fmt.Errorf("handshake: %w", err)
	}
	return nil
}

// call sends one request and returns its result. An answer for another id is refused, because acting on another
// request's reply would mean acting on another task.
func (c *client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID
	c.nextID++
	rep, err := c.exchange(ctx, rpcRequest{JSONRPC: "2.0", ID: &id, Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	if rep.Error != nil {
		return nil, fmt.Errorf("%s: the server answered error %d: %s", method, rep.Error.Code, rep.Error.Message)
	}
	var answered int
	if err := json.Unmarshal(rep.ID, &answered); err != nil || answered != id {
		return nil, fmt.Errorf("%s: the server answer is not for request %d", method, id)
	}
	if len(rep.Result) == 0 {
		return nil, fmt.Errorf("%s: the server answer has no result", method)
	}
	return rep.Result, nil
}

// notify sends one notification, which the server answers with 202 and no body.
func (c *client) notify(ctx context.Context, method string, params any) error {
	_, err := c.exchange(ctx, rpcRequest{JSONRPC: "2.0", Method: method, Params: params})
	return err
}

// callTool runs one MCP tool. A refusal is a result with IsError set, not an error: the agent keeps its budget and can
// correct the call.
func (c *client) callTool(ctx context.Context, name string, args any) (toolResult, error) {
	raw, err := c.call(ctx, "tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return toolResult{}, err
	}
	var res toolResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return toolResult{}, fmt.Errorf("%s: read the tool result: %w", name, err)
	}
	return res, nil
}

// exchange posts one JSON-RPC message and returns its answer. A failed HTTP status is an error that carries the
// status and never the body, because the body can hold untrusted text.
func (c *client) exchange(ctx context.Context, msg rpcRequest) (*rpcReply, error) {
	body, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("%s: encode the request: %w", msg.Method, err)
	}
	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%s: build the request: %w", msg.Method, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: reach the server: %w", msg.Method, err)
	}
	// The body is read in full below; a close error has nothing left to report.
	defer func() { _ = resp.Body.Close() }()
	if err := checkStatus(msg.Method, resp.StatusCode); err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusAccepted {
		return &rpcReply{}, nil
	}
	var rep rpcReply
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAnswerBytes)).Decode(&rep); err != nil {
		return nil, fmt.Errorf("%s: read the server answer: %w", msg.Method, err)
	}
	if rep.JSONRPC != "2.0" {
		return nil, fmt.Errorf("%s: the server answer is not JSON-RPC 2.0", msg.Method)
	}
	return &rep, nil
}

// checkStatus turns the HTTP status into an error. The token is never part of the message.
func checkStatus(method string, status int) error {
	switch status {
	case http.StatusOK, http.StatusAccepted:
		return nil
	case http.StatusUnauthorized:
		return fmt.Errorf("%s: the server refused the agent token", method)
	}
	return fmt.Errorf("%s: the server answered HTTP %d", method, status)
}
