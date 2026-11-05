package mcp

import (
	"encoding/json"
	"errors"
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

// expectedTools is the tool table of TECH_SPEC 14.1 for the tools this package builds. "all" in the spec means every
// LLM role, so profiler gets the same tools as reviewer.
func expectedTools(role workflow.Role) []string {
	all := []string{
		"heimdall_get_task", "heimdall_ack", "heimdall_get_change",
		"heimdall_read_untrusted", "heimdall_read_code", "heimdall_ask_knowledge", "heimdall_report_progress",
	}
	switch role {
	case workflow.RoleTestAuthor:
		return append(all, "heimdall_submit_tests", "heimdall_ask_humans")
	case workflow.RoleResolver:
		return append(all, "heimdall_ask_humans", "heimdall_submit_verdict")
	case workflow.RoleReviewer:
		return append(all, "heimdall_submit_review", "heimdall_read_pack", "heimdall_stage_knowledge", "heimdall_approve_knowledge", "heimdall_forget_knowledge")
	case workflow.RoleProfiler:
		return all
	}
	return nil
}

func allLLMRoles() []workflow.Role {
	return []workflow.Role{workflow.RoleTestAuthor, workflow.RoleReviewer, workflow.RoleResolver, workflow.RoleProfiler}
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

func TestInitializeNegotiatesTheVersionAndAdvertisesTools(t *testing.T) {
	r := newRig(t, workflow.RoleReviewer)
	rep := r.rpc("initialize", `{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"opencode","version":"1.0.0"}}`)
	require.Nil(t, rep.Error)
	var res struct {
		ProtocolVersion string                     `json:"protocolVersion"`
		Capabilities    map[string]json.RawMessage `json:"capabilities"`
		ServerInfo      struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
	}
	require.NoError(t, decodeInto(rep.Result, &res))
	require.Equal(t, "2025-06-18", res.ProtocolVersion)
	require.Contains(t, res.Capabilities, "tools")
	require.Equal(t, "heimdall", res.ServerInfo.Name)
}

func TestInitializeAnswersAnUnknownVersionWithTheLatestItSupports(t *testing.T) {
	r := newRig(t, workflow.RoleReviewer)
	rep := r.rpc("initialize", `{"protocolVersion":"1999-01-01","capabilities":{}}`)
	require.Nil(t, rep.Error)
	var res struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	require.NoError(t, decodeInto(rep.Result, &res))
	require.Equal(t, "2025-06-18", res.ProtocolVersion)
}

func TestPingReturnsAnEmptyResult(t *testing.T) {
	r := newRig(t, workflow.RoleReviewer)
	rep := r.rpc("ping", "{}")
	require.Nil(t, rep.Error)
	require.JSONEq(t, "{}", string(rep.Result))
}

func TestAnUnknownMethodIsMethodNotFoundAndKeepsTheID(t *testing.T) {
	r := newRig(t, workflow.RoleReviewer)
	rep := r.rpc("resources/list", "{}")
	require.NotNil(t, rep.Error)
	require.Equal(t, codeMethodNotFound, rep.Error.Code)
	require.JSONEq(t, "1", string(rep.ID))
}

func TestAStringIDIsEchoedBackUnchanged(t *testing.T) {
	r := newRig(t, workflow.RoleReviewer)
	rep := decodeReply(t, r.post("Bearer "+testToken, `{"jsonrpc":"2.0","id":"req-7","method":"ping"}`))
	require.JSONEq(t, `"req-7"`, string(rep.ID))
	require.Nil(t, rep.Error)
}

func TestANotificationGetsNoBodyAndStatus202(t *testing.T) {
	r := newRig(t, workflow.RoleReviewer)
	rep := r.post("Bearer "+testToken, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	require.Equal(t, http.StatusAccepted, rep.status)
	require.Empty(t, rep.body)
}

func TestMalformedJSONIsAParseErrorWithANullID(t *testing.T) {
	r := newRig(t, workflow.RoleReviewer)
	rep := r.post("Bearer "+testToken, `{not json`)
	require.Equal(t, http.StatusBadRequest, rep.status)
	out := decodeReply(t, rep)
	require.NotNil(t, out.Error)
	require.Equal(t, codeParseError, out.Error.Code)
	require.JSONEq(t, "null", string(out.ID))
}

func TestABatchArrayIsRefusedAsAnInvalidRequest(t *testing.T) {
	r := newRig(t, workflow.RoleReviewer)
	rep := r.post("Bearer "+testToken, `[{"jsonrpc":"2.0","id":1,"method":"ping"}]`)
	require.Equal(t, http.StatusBadRequest, rep.status)
	out := decodeReply(t, rep)
	require.NotNil(t, out.Error)
	require.Equal(t, codeInvalidRequest, out.Error.Code)
}

func TestAWrongJSONRPCVersionIsAnInvalidRequest(t *testing.T) {
	r := newRig(t, workflow.RoleReviewer)
	rep := r.post("Bearer "+testToken, `{"jsonrpc":"1.0","id":1,"method":"ping"}`)
	out := decodeReply(t, rep)
	require.NotNil(t, out.Error)
	require.Equal(t, codeInvalidRequest, out.Error.Code)
}

func TestToolsListShowsExactlyTheToolsOfEachRole(t *testing.T) {
	for _, role := range allLLMRoles() {
		t.Run(string(role), func(t *testing.T) {
			r := newRig(t, role)
			require.ElementsMatch(t, expectedTools(role), toolNames(listTools(t, r)))
		})
	}
}

func TestEveryListedToolHasADescriptionAndAClosedObjectSchema(t *testing.T) {
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
	r := newRig(t, workflow.RoleReviewer)
	hidden := r.rpc("tools/call", `{"name":"heimdall_submit_tests","arguments":{}}`)
	unknown := r.rpc("tools/call", `{"name":"heimdall_no_such_tool","arguments":{}}`)
	require.NotNil(t, hidden.Error)
	require.NotNil(t, unknown.Error)
	require.Equal(t, codeInvalidParams, hidden.Error.Code)
	require.Equal(t, *unknown.Error, *hidden.Error, "a hidden tool and an unknown tool must look the same")
	require.Equal(t, Principal{}, r.tasks.currentFor, "the role check runs before any task is read")
}

func TestToolsCallWithoutANameIsInvalidParams(t *testing.T) {
	r := newRig(t, workflow.RoleReviewer)
	rep := r.rpc("tools/call", `{"arguments":{}}`)
	require.NotNil(t, rep.Error)
	require.Equal(t, codeInvalidParams, rep.Error.Code)
}

func TestToolsCallWithArrayParamsIsInvalidParams(t *testing.T) {
	r := newRig(t, workflow.RoleReviewer)
	rep := r.rpc("tools/call", `["heimdall_get_task"]`)
	require.NotNil(t, rep.Error)
	require.Equal(t, codeInvalidParams, rep.Error.Code)
}

func TestTheTaskEnvelopeMustBeForTheTokensRole(t *testing.T) {
	r := newRig(t, workflow.RoleReviewer)
	r.tasks.current.Task = testEnvelope(workflow.RoleTestAuthor)

	got := r.call("heimdall_get_task", `{}`)
	require.True(t, got.IsError, "a mismatched envelope must be refused")
	require.NotContains(t, got.text(), "tsk_01J8", "the other role's envelope must not leak")

	progress := r.call("heimdall_report_progress", `{"text":"hello"}`)
	require.True(t, progress.IsError)
	require.Empty(t, r.tasks.progress, "no effect may run for a mismatched envelope")
}

func TestNoActiveTaskIsARefusalNotAProtocolError(t *testing.T) {
	r := newRig(t, workflow.RoleTestAuthor)
	r.tasks.currentErr = ErrNoTask
	rep := r.rpc("tools/call", `{"name":"heimdall_get_task","arguments":{}}`)
	require.Nil(t, rep.Error, "no active task is an answer the agent can act on, not a protocol error")
	var res toolResult
	require.NoError(t, decodeInto(rep.Result, &res))
	require.True(t, res.IsError)
	require.Contains(t, res.text(), "no active task")
}

func TestAnUnexpectedServerFailureIsGenericAndItsCauseIsLogged(t *testing.T) {
	r := newRig(t, workflow.RoleTestAuthor)
	r.tasks.currentErr = errors.New("postgres://heimdall:hunter2-cause@db refused")
	res := r.call("heimdall_get_task", `{}`)
	require.True(t, res.IsError)
	require.Equal(t, "the server could not complete this call", res.text())
	require.NotContains(t, res.text(), "hunter2")
	require.Contains(t, r.logs.String(), "hunter2-cause", "the cause belongs in the log")
}

func TestReviewerCannotCallSubmitVerdictAndResolverCannotCallSubmitReview(t *testing.T) {
	rev := newRig(t, workflow.RoleReviewer)
	rep := rev.rpc("tools/call", `{"name":"heimdall_submit_verdict","arguments":{}}`)
	require.NotNil(t, rep.Error)
	require.Equal(t, codeInvalidParams, rep.Error.Code)

	res := newRig(t, workflow.RoleResolver)
	rep = res.rpc("tools/call", `{"name":"heimdall_submit_review","arguments":{}}`)
	require.NotNil(t, rep.Error)
	require.Equal(t, codeInvalidParams, rep.Error.Code)
}

func TestWrongRoleSeesNoSuchToolForSubmitReviewAndSubmitVerdict(t *testing.T) {
	author := newRig(t, workflow.RoleTestAuthor)
	rep := author.rpc("tools/call", `{"name":"heimdall_submit_review","arguments":{}}`)
	require.NotNil(t, rep.Error)
	require.Equal(t, codeInvalidParams, rep.Error.Code)

	rep = author.rpc("tools/call", `{"name":"heimdall_submit_verdict","arguments":{}}`)
	require.NotNil(t, rep.Error)
	require.Equal(t, codeInvalidParams, rep.Error.Code)
}
