package switchchat

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

func TestCreateManagedAgentLeavesLaunchToSwitchAndDefersStartForHostConfig(t *testing.T) {
	f := newFakeSwitch(t)
	var got map[string]any
	f.ops["create_agent"] = func(args map[string]any) (int, any) {
		got = args
		return http.StatusOK, map[string]any{"agent_id": "managed-1", "name": "test-author"}
	}
	c, _ := newTestClient(t, f)
	spec := ports.ManagedAgentSpec{
		Agent:       ports.AgentRef{Name: id.AgentName("test-author"), Role: workflow.RoleTestAuthor},
		Description: "Heimdall test author", Machine: "machine-1", Provider: "opencode",
		Model: "openrouter/deepseek/deepseek-v4-flash", Instructions: "Use Heimdall tools.",
	}

	gotAgent, err := c.CreateManagedAgent(context.Background(), spec)
	require.NoError(t, err)
	require.Equal(t, ports.ManagedAgent{ID: "managed-1", Name: "test-author"}, gotAgent)
	require.Equal(t, "machine-1", got["machine"])
	require.Equal(t, "opencode", got["provider"])
	require.Equal(t, "openrouter/deepseek/deepseek-v4-flash", got["model"])
	require.Equal(t, true, got["auto_mode"])
	require.Equal(t, false, got["auto_approve"])
	require.Equal(t, false, got["start"], "Switch should start the process only after host MCP configuration is installed")
}

func TestSetManagedAgentStateRequestsControllerStateAndChecksIdentity(t *testing.T) {
	f := newFakeSwitch(t)
	var got map[string]any
	f.ops["update_agent_detail"] = func(args map[string]any) (int, any) {
		got = args
		return http.StatusOK, map[string]any{"id": "managed-1"}
	}
	c, _ := newTestClient(t, f)
	err := c.SetManagedAgentState(context.Background(), "managed-1", ports.ManagedAgentRunning)
	require.NoError(t, err)
	require.Equal(t, "managed-1", got["agent_id"])
	require.Equal(t, "running", got["desired_state"])
}

func TestCreateManagedAgentRejectsInvalidRoleBeforeCallingSwitch(t *testing.T) {
	f := newFakeSwitch(t)
	c, _ := newTestClient(t, f)
	_, err := c.CreateManagedAgent(context.Background(), ports.ManagedAgentSpec{
		Agent:       ports.AgentRef{Name: id.AgentName("test-author"), Role: "wizard"},
		Description: "agent", Machine: "machine-1", Provider: "opencode", Model: "model",
	})
	require.Error(t, err)
	require.Empty(t, f.requests())
}

func TestCreateManagedAgentReplayAdoptsExistingMatchingControllerDefinition(t *testing.T) {
	f := newFakeSwitch(t)
	c, _ := newTestClient(t, f)
	spec := ports.ManagedAgentSpec{
		Agent:       ports.AgentRef{Name: id.AgentName("test-author"), Role: workflow.RoleTestAuthor},
		Description: "Heimdall test author", Machine: "machine-1", Provider: "opencode",
		Model: "openrouter/deepseek/deepseek-v4-flash", Instructions: "Use Heimdall tools.",
	}
	f.ops["list_managed_agents"] = func(map[string]any) (int, any) {
		return http.StatusOK, []any{map[string]any{
			"agent_id": "managed-1", "name": "test-author", "description": "Heimdall test author",
			"provider": "opencode", "model": "openrouter/deepseek/deepseek-v4-flash",
			"auto_mode": true, "auto_approve": false, "machine": map[string]any{"id": "machine-1", "name": "VM"},
			"desired_state": "stopped",
		}}
	}
	f.ops["create_agent"] = func(map[string]any) (int, any) {
		return http.StatusOK, map[string]any{"agent_id": "managed-1", "name": "test-author"}
	}
	got, err := c.CreateManagedAgent(context.Background(), spec)
	require.NoError(t, err)
	require.Equal(t, ports.ManagedAgent{ID: "managed-1", Name: "test-author"}, got)
	require.Empty(t, f.opCalls("create_agent"), "replay must adopt the existing matching definition")
}

func TestEnsureMovesAnExistingAgentToTheConfiguredModel(t *testing.T) {
	f := newFakeSwitch(t)
	c, _ := newTestClient(t, f)
	spec := ports.ManagedAgentSpec{
		Agent:       ports.AgentRef{Name: id.AgentName("test-author"), Role: workflow.RoleTestAuthor},
		Description: "Heimdall test author", Machine: "machine-1", Provider: "opencode",
		Model: "deploy/deepseek-ai/deepseek-v4.1-flash-1017", Instructions: "Use Heimdall tools.",
	}
	f.ops["list_managed_agents"] = func(map[string]any) (int, any) {
		return http.StatusOK, []any{map[string]any{
			"agent_id": "managed-1", "name": "test-author", "description": "Heimdall test author",
			"provider": "opencode", "model": "openrouter/deepseek/deepseek-v4-flash",
			"auto_mode": true, "auto_approve": false, "machine": map[string]any{"id": "machine-1", "name": "VM"},
		}}
	}
	f.ops["update_agent_detail"] = func(map[string]any) (int, any) { return http.StatusOK, map[string]any{} }
	got, err := c.CreateManagedAgent(context.Background(), spec)
	require.NoError(t, err)
	require.Equal(t, ports.ManagedAgent{ID: "managed-1", Name: "test-author"}, got)
	calls := f.opCalls("update_agent_detail")
	require.Len(t, calls, 1)
	require.Equal(t, "managed-1", calls[0]["agent_id"])
	require.Equal(t, "deploy/deepseek-ai/deepseek-v4.1-flash-1017", calls[0]["model"])
	require.Empty(t, f.opCalls("create_agent"))
}

func TestEnsureDoesNotTouchTheModelWhenItAlreadyMatches(t *testing.T) {
	f := newFakeSwitch(t)
	c, _ := newTestClient(t, f)
	spec := ports.ManagedAgentSpec{
		Agent:       ports.AgentRef{Name: id.AgentName("test-author"), Role: workflow.RoleTestAuthor},
		Description: "Heimdall test author", Machine: "machine-1", Provider: "opencode",
		Model: "deploy/deepseek-ai/deepseek-v4.1-flash-1017", Instructions: "Use Heimdall tools.",
	}
	f.ops["list_managed_agents"] = func(map[string]any) (int, any) {
		return http.StatusOK, []any{map[string]any{
			"agent_id": "managed-1", "name": "test-author", "description": "Heimdall test author",
			"provider": "opencode", "model": "deploy/deepseek-ai/deepseek-v4.1-flash-1017",
			"auto_mode": true, "auto_approve": false, "machine": map[string]any{"id": "machine-1", "name": "VM"},
		}}
	}
	_, err := c.CreateManagedAgent(context.Background(), spec)
	require.NoError(t, err)
	require.Empty(t, f.opCalls("update_agent_detail"))
}

func TestSetModelReachesAnAgentMadeByHandAndChangesOnlyItsModel(t *testing.T) {
	f := newFakeSwitch(t)
	c, _ := newTestClient(t, f)
	f.ops["list_managed_agents"] = func(map[string]any) (int, any) {
		return http.StatusOK, []any{map[string]any{
			"agent_id": "managed-9", "name": "reviewer-1", "description": "made by hand", "directory": "/somewhere",
			"provider": "opencode", "model": "openrouter/deepseek/deepseek-v4-flash",
			"auto_mode": true, "auto_approve": false, "machine": map[string]any{"id": "machine-1", "name": "VM"},
		}}
	}
	f.ops["update_agent_detail"] = func(map[string]any) (int, any) { return http.StatusOK, map[string]any{} }
	spec := ports.ManagedAgentSpec{
		Agent: ports.AgentRef{Name: id.AgentName("reviewer-1"), Role: workflow.RoleReviewer}, Description: "Heimdall reviewer-1",
		Machine: "machine-1", Provider: "opencode", Model: "deploy/deepseek-ai/deepseek-v4.1-flash-1017",
	}
	require.NoError(t, c.SetManagedAgentModel(context.Background(), spec))
	calls := f.opCalls("update_agent_detail")
	require.Len(t, calls, 1)
	require.Equal(t, map[string]any{"agent_id": "managed-9", "model": "deploy/deepseek-ai/deepseek-v4.1-flash-1017"}, calls[0])

	spec.Machine = "other-machine"
	require.ErrorIs(t, c.SetManagedAgentModel(context.Background(), spec), ports.ErrConflict, "never move an agent on another machine")
	spec.Agent.Name = "reviewer-9"
	require.ErrorIs(t, c.SetManagedAgentModel(context.Background(), spec), ports.ErrNotFound)
}
