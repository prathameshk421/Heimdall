package mcp

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// wireTool is one entry of tools/list as a client reads it.
type wireTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

func TestKnowledgeLifecycleToolsAreListedOnlyForAuthenticatedHumanPrincipals(t *testing.T) {
	t.Parallel()
	r := newRig(t, workflow.RoleReviewer)
	r.tokens.byHash[HashToken(testToken)] = Principal{Person: "alice", Human: true}
	require.ElementsMatch(t, []string{"approve_knowledge", "forget_knowledge"}, toolNames(listTools(t, r)))
	approved := r.call("approve_knowledge", `{"ref":"knowledge:pending"}`)
	require.False(t, approved.IsError, approved.text())

	r.tokens.byHash[HashToken(testToken)] = Principal{Agent: testAgent, Role: workflow.RoleReviewer, Person: "alice", Human: true}
	forged := r.rpc("tools/call", `{"name":"approve_knowledge","arguments":{"ref":"knowledge:pending"}}`)
	require.NotNil(t, forged.Error, "a human flag cannot be combined with an agent principal")
}

func listTools(t *testing.T, r *rig) []wireTool {
	t.Helper()
	rep := r.rpc("tools/list", "{}")
	require.Nil(t, rep.Error, "tools/list must not be a protocol error")
	var out struct {
		Tools []wireTool `json:"tools"`
	}
	require.NoError(t, decodeInto(rep.Result, &out))
	return out.Tools
}

func toolNames(tools []wireTool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

const initBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}`

func TestInitializeAnswersAnUnknownVersionWithTheLatestItSupports(t *testing.T) {
	t.Parallel()
	r := newRig(t, workflow.RoleReviewer)
	rep := r.rpc("initialize", `{"protocolVersion":"1999-01-01","capabilities":{}}`)
	require.Nil(t, rep.Error)
	var res struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	require.NoError(t, decodeInto(rep.Result, &res))
	require.Equal(t, "2025-06-18", res.ProtocolVersion)
}

func TestAnUnknownMethodIsMethodNotFoundAndKeepsTheID(t *testing.T) {
	t.Parallel()
	r := newRig(t, workflow.RoleReviewer)
	rep := r.rpc("resources/list", "{}")
	require.NotNil(t, rep.Error)
	require.Equal(t, codeMethodNotFound, rep.Error.Code)
	require.JSONEq(t, "1", string(rep.ID))
}

func TestABatchArrayIsRefusedAsAnInvalidRequest(t *testing.T) {
	t.Parallel()
	r := newRig(t, workflow.RoleReviewer)
	rep := r.post("Bearer "+testToken, `[{"jsonrpc":"2.0","id":1,"method":"ping"}]`)
	require.Equal(t, http.StatusBadRequest, rep.status)
	out := decodeReply(t, rep)
	require.NotNil(t, out.Error)
	require.Equal(t, codeInvalidRequest, out.Error.Code)
}

func TestAWrongJSONRPCVersionIsAnInvalidRequest(t *testing.T) {
	t.Parallel()
	r := newRig(t, workflow.RoleReviewer)
	rep := r.post("Bearer "+testToken, `{"jsonrpc":"1.0","id":1,"method":"ping"}`)
	out := decodeReply(t, rep)
	require.NotNil(t, out.Error)
	require.Equal(t, codeInvalidRequest, out.Error.Code)
}

func TestEveryListedToolHasADescriptionAndAClosedObjectSchema(t *testing.T) {
	t.Parallel()
	r := newRig(t, workflow.RoleTestAuthor)
	for _, tool := range listTools(t, r) {
		t.Run(tool.Name, func(t *testing.T) {
			require.NotEmpty(t, strings.TrimSpace(tool.Description))
			var schema struct {
				Type                 string          `json:"type"`
				Properties           json.RawMessage `json:"properties"`
				AdditionalProperties *bool           `json:"additionalProperties"`
			}
			require.NoError(t, json.Unmarshal(tool.InputSchema, &schema))
			require.Equal(t, "object", schema.Type)
			require.NotNil(t, schema.Properties, "properties must be declared, even when empty")
			require.NotNil(t, schema.AdditionalProperties, "additionalProperties must be declared")
			require.False(t, *schema.AdditionalProperties, "unknown arguments must be refused by schema")
		})
	}
}

func TestACallToAToolTheRoleLacksAnswersLikeAnUnknownTool(t *testing.T) {
	t.Parallel()
	r := newRig(t, workflow.RoleReviewer)
	hidden := r.rpc("tools/call", `{"name":"submit_tests","arguments":{}}`)
	unknown := r.rpc("tools/call", `{"name":"no_such_tool","arguments":{}}`)
	require.NotNil(t, hidden.Error)
	require.NotNil(t, unknown.Error)
	require.Equal(t, codeInvalidParams, hidden.Error.Code)
	require.Equal(t, *unknown.Error, *hidden.Error, "a hidden tool and an unknown tool must look the same")
	require.Equal(t, Principal{}, r.tasks.currentFor, "the role check runs before any task is read")
}

func TestToolsCallWithArrayParamsIsInvalidParams(t *testing.T) {
	t.Parallel()
	r := newRig(t, workflow.RoleReviewer)
	rep := r.rpc("tools/call", `["get_task"]`)
	require.NotNil(t, rep.Error)
	require.Equal(t, codeInvalidParams, rep.Error.Code)
}

func TestTheTaskEnvelopeMustBeForTheTokensRole(t *testing.T) {
	t.Parallel()
	r := newRig(t, workflow.RoleReviewer)
	r.tasks.current.Task = testEnvelope(workflow.RoleTestAuthor)

	got := r.call("get_task", `{}`)
	require.True(t, got.IsError, "a mismatched envelope must be refused")
	require.NotContains(t, got.text(), "tsk_01J8", "the other role's envelope must not leak")

	progress := r.call("report_progress", `{"text":"hello"}`)
	require.True(t, progress.IsError)
	require.Empty(t, r.tasks.progress, "no effect may run for a mismatched envelope")
}

func TestNoActiveTaskIsAPlainAnswerNotAProtocolError(t *testing.T) {
	t.Parallel()
	r := newRig(t, workflow.RoleTestAuthor)
	r.tasks.currentErr = ErrNoTask
	rep := r.rpc("tools/call", `{"name":"get_task","arguments":{}}`)
	require.Nil(t, rep.Error, "no active task is an answer the agent can act on, not a protocol error")
	var res toolResult
	require.NoError(t, decodeInto(rep.Result, &res))
	require.False(t, res.IsError, "get_task with no task answers plainly")
	require.Contains(t, res.text(), "no active task")
}
