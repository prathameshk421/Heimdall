//go:build live

package main

// TestMCPSmokeAgainstRunningServer is the cheapest real check that a running heimdall-server speaks the agent
// protocol. It is not part of make fast and not part of the release binary.
//
// What it proves: the MCP edge at the endpoint answers initialize, notifications/initialized and tools/list with the
// expected HTTP statuses (never 401), the JSON-RPC 2.0 envelope is well formed, tools/list returns at least one tool
// and it contains get_task.
// What it does not prove: any tool call, any task, any database row, any GitHub or Switch call.
//
// The token comes from the file named by HEIMDALL_AGENT_TOKEN_FILE. Only the path is read from the environment; the
// token's content is never printed, logged, or put in an error message or a failure line. A missing or unreadable
// file fails the test; a refused token fails the test at the HTTP 401 assertion.
//
// How to run (the server must already listen on 127.0.0.1:8080; see docs/RUNBOOK.md "Live run of heimdall-server"):
//
//	HEIMDALL_AGENT_TOKEN_FILE="$HOME/heimdall/secrets/agent_token_test-author" \
//	    go test -tags live -count=1 -run TestMCPSmokeAgainstRunningServer -v ./cmd/heimdall-testagent/
//
// HEIMDALL_MCP_URL overrides the endpoint (default http://127.0.0.1:8080/mcp).

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const defaultMCPURL = "http://127.0.0.1:8080/mcp"

func TestMCPSmokeAgainstRunningServer(t *testing.T) {
	tokenFile := os.Getenv("HEIMDALL_AGENT_TOKEN_FILE")
	if tokenFile == "" {
		t.Skip("not verified: set HEIMDALL_AGENT_TOKEN_FILE to the path of an agent token file (never its content)")
	}
	endpoint := os.Getenv("HEIMDALL_MCP_URL")
	if endpoint == "" {
		endpoint = defaultMCPURL
	}
	token, err := readToken(tokenFile)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	initRaw := smokePost(t, ctx, endpoint, token, 1, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": clientName, "version": clientVersion},
	}, http.StatusOK)
	var initOut struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
	}
	require.NoError(t, json.Unmarshal(initRaw, &initOut), "initialize: the result is not an initialize result")
	require.Equal(t, protocolVersion, initOut.ProtocolVersion, "initialize: unexpected protocol revision")
	require.Equal(t, "heimdall", initOut.ServerInfo.Name, "initialize: unexpected server name")
	t.Logf("initialize: HTTP 200, protocol %s, server %q", initOut.ProtocolVersion, initOut.ServerInfo.Name)

	smokePost(t, ctx, endpoint, token, 0, "notifications/initialized", map[string]any{}, http.StatusAccepted)
	t.Log("notifications/initialized: HTTP 202")

	list := smokePost(t, ctx, endpoint, token, 2, "tools/list", map[string]any{}, http.StatusOK)
	var listing struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(list, &listing), "tools/list: the result is not a tool listing")
	require.NotEmpty(t, listing.Tools, "tools/list: the server listed no tools for this agent's role")
	var names []string
	foundGetTask := false
	for _, tool := range listing.Tools {
		names = append(names, tool.Name)
		if tool.Name == toolGetTask {
			foundGetTask = true
		}
	}
	require.True(t, foundGetTask, "tools/list: %s is missing (got %s)", toolGetTask, strings.Join(names, ", "))
	t.Logf("tools/list: HTTP 200, %d tools, %s present", len(listing.Tools), toolGetTask)
}

// smokePost posts one JSON-RPC message and returns its result. An id of 0 marks a notification, which the server
// answers with 202 and no body. The token is sent as a bearer header and never appears in a failure message; bodies
// of error answers are not printed, because they can hold untrusted text.
func smokePost(t *testing.T, ctx context.Context, endpoint, token string, id int, method string, params any, wantStatus int) json.RawMessage {
	t.Helper()
	msg := rpcRequest{JSONRPC: "2.0", Method: method, Params: params}
	if id != 0 {
		msg.ID = &id
	}
	body, err := json.Marshal(msg)
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: requestTimeout}
	resp, err := client.Do(req)
	require.NoError(t, err, "%s: cannot reach the server", method)
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswerBytes))
	require.NoError(t, err, "%s: cannot read the server answer", method)
	require.NotEqual(t, http.StatusUnauthorized, resp.StatusCode,
		"%s: HTTP 401 — the token file was refused", method)
	require.Equal(t, wantStatus, resp.StatusCode, "%s: unexpected HTTP status", method)
	if wantStatus != http.StatusOK {
		return nil
	}
	var rep rpcReply
	require.NoError(t, json.Unmarshal(raw, &rep), "%s: the answer is not JSON-RPC 2.0", method)
	require.Equal(t, "2.0", rep.JSONRPC, "%s: the answer is not JSON-RPC 2.0", method)
	require.Nil(t, rep.Error, "%s: the server answered with a JSON-RPC error", method)
	var answered int
	require.NoError(t, json.Unmarshal(rep.ID, &answered), "%s: the answer id is not a number", method)
	require.Equal(t, id, answered, "%s: the answer is not for request %d", method, id)
	require.NotEmpty(t, rep.Result, "%s: the answer has no result", method)
	return rep.Result
}
