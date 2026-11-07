//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTask_T06(t *testing.T) {
	endpoint := os.Getenv("HEIMDALL_GRAPHITI_URL")
	if endpoint == "" {
		endpoint = "http://127.0.0.1:18000/mcp"
	}
	client := &http.Client{Timeout: 2 * time.Minute}
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
	defer cancel()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	groupA, groupB := "heimdall-t06-a-"+suffix, "heimdall-t06-b-"+suffix
	var session string
	// Graphiti's clear_graph tool accepts one or more group_ids. The groups are unique to
	// this run, so cleanup can only affect data created by this test.
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		for _, group := range []string{groupA, groupB} {
			result, _, err := graphitiRequest(cleanupCtx, client, endpoint, session, "tools/call", map[string]any{
				"name": "clear_graph", "arguments": map[string]any{"group_ids": group},
			})
			if err != nil {
				t.Errorf("clear Graphiti group %s: %v", group, err)
				continue
			}
			if err := graphitiToolError(result); err != nil {
				t.Errorf("clear Graphiti group %s: %v", group, err)
			}
		}
	})

	// Streamable HTTP permits a JSON response for a single request. We use
	// protocol version 2025-03-26 and retain the session header, if supplied,
	// for subsequent requests; no long-lived SSE stream is required here.
	_, session, err := graphitiRequest(ctx, client, endpoint, session, "initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "heimdall-t06", "version": "1"},
	})
	require.NoError(t, err)
	_, session, err = graphitiRequest(ctx, client, endpoint, session, "notifications/initialized", nil)
	require.NoError(t, err)

	fact := "The " + suffix + " payments review established that refunds must reuse the original payment's idempotency key."
	added, updatedSession, err := graphitiRequest(ctx, client, endpoint, session, "tools/call", map[string]any{
		"name": "add_memory",
		"arguments": map[string]any{
			"name": fact, "episode_body": fact, "group_id": groupA,
			"source": "message", "source_description": "T06 real extraction isolation check",
		},
	})
	require.NoError(t, err, "add_memory must extract the episode through Graphiti and its configured structured-output model")
	session = updatedSession
	require.NoError(t, graphitiToolError(added))

	query := "What idempotency key should refunds reuse?"
	var a json.RawMessage
	deadline := time.Now().Add(2 * time.Minute)
	for {
		a, session, err = graphitiRequest(ctx, client, endpoint, session, "tools/call", map[string]any{
			"name": "search_memory_facts", "arguments": map[string]any{"query": query, "group_ids": groupA},
		})
		require.NoError(t, err)
		require.NoError(t, graphitiToolError(a))
		if strings.Contains(graphitiText(a), suffix) || time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for extracted Graphiti fact: %v", ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}
	require.Contains(t, graphitiText(a), suffix, "the extracted fact should be returned for group A")

	b, _, err := graphitiRequest(ctx, client, endpoint, session, "tools/call", map[string]any{
		"name": "search_memory_facts", "arguments": map[string]any{"query": query, "group_ids": groupB},
	})
	require.NoError(t, err)
	require.NoError(t, graphitiToolError(b))
	require.NotContains(t, graphitiText(b), suffix, "group B must not see group A's fact")
}

func graphitiRequest(ctx context.Context, client *http.Client, endpoint, session, method string, params any) (json.RawMessage, string, error) {
	body, err := graphitiRequestBody(method, params)
	if err != nil {
		return nil, session, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, session, fmt.Errorf("create MCP %s request: %w", method, err)
	}
	setGraphitiHeaders(req, session)
	resp, err := client.Do(req)
	if err != nil {
		return nil, session, fmt.Errorf("send MCP %s: %w", method, err)
	}
	return graphitiResponse(resp, session, method)
}

func graphitiRequestBody(method string, params any) ([]byte, error) {
	requestBody := map[string]any{"jsonrpc": "2.0", "method": method}
	if !strings.HasPrefix(method, "notifications/") {
		requestBody["id"] = 1
	}
	if params != nil {
		requestBody["params"] = params
	}
	body, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("marshal MCP %s: %w", method, err)
	}
	return body, nil
}

func setGraphitiHeaders(req *http.Request, session string) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
	}
}

func graphitiResponse(resp *http.Response, session, method string) (json.RawMessage, string, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		closeErr := resp.Body.Close()
		if closeErr != nil {
			return nil, session, fmt.Errorf("close MCP %s response after HTTP %s: %w", method, resp.Status, closeErr)
		}
		return nil, session, fmt.Errorf("MCP %s returned HTTP %s", method, resp.Status)
	}
	if value := resp.Header.Get("Mcp-Session-Id"); value != "" {
		session = value
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	closeErr := resp.Body.Close()
	if err != nil {
		return nil, session, fmt.Errorf("read MCP %s: %w", method, err)
	}
	if closeErr != nil {
		return nil, session, fmt.Errorf("close MCP %s response: %w", method, closeErr)
	}
	if strings.HasPrefix(method, "notifications/") && resp.StatusCode == http.StatusAccepted && len(data) == 0 {
		return nil, session, nil
	}
	return graphitiDecodeResponse(data, resp.Header.Get("Content-Type"), session, method)
}

func graphitiDecodeResponse(data []byte, contentType, session, method string) (json.RawMessage, string, error) {
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	// Streamable HTTP may return one SSE event instead of a JSON document.
	if strings.HasPrefix(contentType, "text/event-stream") {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "data: ") {
				data = []byte(strings.TrimPrefix(line, "data: "))
				break
			}
		}
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, session, fmt.Errorf("decode MCP %s response: %w", method, err)
	}
	if envelope.Error != nil {
		return nil, session, fmt.Errorf("MCP %s error: %s", method, envelope.Error.Message)
	}
	return envelope.Result, session, nil
}

func graphitiText(result json.RawMessage) string {
	var parsed any
	if json.Unmarshal(result, &parsed) != nil {
		return string(result)
	}
	return fmt.Sprint(parsed)
}

func graphitiToolError(result json.RawMessage) error {
	var response struct {
		IsError bool `json:"isError"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		StructuredContent struct {
			Result struct {
				Error string `json:"error"`
			} `json:"result"`
		} `json:"structuredContent"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return fmt.Errorf("decode Graphiti tool result: %w", err)
	}
	if response.IsError {
		return fmt.Errorf("Graphiti tool error: %s", graphitiText(result))
	}
	if response.StructuredContent.Result.Error != "" {
		return fmt.Errorf("Graphiti tool error: %s", response.StructuredContent.Result.Error)
	}
	for _, item := range response.Content {
		if strings.Contains(strings.ToLower(item.Text), "error") {
			return fmt.Errorf("Graphiti tool error: %s", item.Text)
		}
	}
	return nil
}
