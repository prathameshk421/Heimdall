package switchchat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

var _ ports.AgentProvisioner = (*Client)(nil)

// EnsureManagedAgent adopts an existing matching definition or creates it stopped.
func (c *Client) EnsureManagedAgent(ctx context.Context, spec ports.ManagedAgentSpec) (ports.ManagedAgent, error) {
	return c.CreateManagedAgent(ctx, spec)
}

// CreateManagedAgent asks Switch to create a stopped managed-agent definition. The
// controller applies and launches the definition after the host has installed the
// per-agent MCP configuration. Heimdall never starts or supervises its process.
func (c *Client) CreateManagedAgent(ctx context.Context, spec ports.ManagedAgentSpec) (ports.ManagedAgent, error) {
	if err := spec.Validate(); err != nil {
		return ports.ManagedAgent{}, fmt.Errorf("switch create managed agent: %w", err)
	}
	var result ports.ManagedAgent
	err := c.guarded(ctx, "create managed agent", func(ctx context.Context) error {
		adopted, found, err := c.findManagedAgent(ctx, spec)
		if err != nil {
			return err
		}
		if found {
			result = adopted
			return nil
		}
		var reply struct {
			AgentID string `json:"agent_id"`
			Name    string `json:"name"`
		}
		args := map[string]any{
			"name":         string(spec.Agent.Name),
			"description":  spec.Description,
			"machine":      spec.Machine,
			"provider":     spec.Provider,
			"model":        spec.Model,
			"instructions": spec.Instructions,
			"directory":    nilIfEmpty(spec.Directory),
			"auto_mode":    true,
			"auto_approve": false,
			"start":        false,
		}
		if err := c.operation(ctx, "create_agent", args, &reply); err != nil {
			if status, ok := StatusOf(err); ok && status == http.StatusConflict {
				adopted, found, lookupErr := c.findManagedAgent(ctx, spec)
				if lookupErr != nil {
					return errors.Join(err, lookupErr)
				}
				if found {
					result = adopted
					return nil
				}
			}
			return err
		}
		if reply.AgentID == "" || reply.Name != string(spec.Agent.Name) {
			return errors.New("switch create_agent: reply does not match requested agent")
		}
		result = ports.ManagedAgent{ID: reply.AgentID, Name: reply.Name}
		return nil
	})
	if err != nil {
		return ports.ManagedAgent{}, fmt.Errorf("switch create managed agent: %w", err)
	}
	return result, nil
}

type managedAgentSummary struct {
	ID          string `json:"agent_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Provider    string `json:"provider"`
	Model       string `json:"model"`
	AutoMode    bool   `json:"auto_mode"`
	AutoApprove bool   `json:"auto_approve"`
	Machine     struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"machine"`
	Directory string `json:"directory"`
}

func (c *Client) findManagedAgent(ctx context.Context, spec ports.ManagedAgentSpec) (ports.ManagedAgent, bool, error) {
	var agents []managedAgentSummary
	if err := c.operation(ctx, "list_managed_agents", map[string]any{}, &agents); err != nil {
		return ports.ManagedAgent{}, false, err
	}
	for _, current := range agents {
		if current.Name != string(spec.Agent.Name) {
			continue
		}
		if !matchesManagedAgent(spec, current) {
			return ports.ManagedAgent{}, false, fmt.Errorf("switch managed agent %q: %w", current.Name, ports.ErrConflict)
		}
		if current.ID == "" {
			return ports.ManagedAgent{}, false, errors.New("switch list_managed_agents: matching agent has no id")
		}
		if current.Model != spec.Model {
			if err := c.setManagedAgentModel(ctx, current.ID, spec.Model); err != nil {
				return ports.ManagedAgent{}, false, err
			}
		}
		return ports.ManagedAgent{ID: current.ID, Name: current.Name}, true, nil
	}
	return ports.ManagedAgent{}, false, nil
}

func matchesManagedAgent(spec ports.ManagedAgentSpec, current managedAgentSummary) bool {
	if current.Machine.ID != spec.Machine && current.Machine.Name != spec.Machine {
		return false
	}
	// The model may differ: Heimdall moves an existing agent to the configured model instead of refusing it, so a
	// model or provider change in heimdall.toml reaches agents that already exist.
	if current.Description != spec.Description || current.Provider != spec.Provider {
		return false
	}
	if !current.AutoMode || current.AutoApprove || current.Directory != spec.Directory {
		return false
	}
	return true
}

// SetManagedAgentState changes the desired state in Switch. The controller
// performs the actual start or stop and remains the only process supervisor.
func (c *Client) SetManagedAgentState(ctx context.Context, agentID string, state ports.ManagedAgentState) error {
	if agentID == "" {
		return errors.New("switch set managed agent state: agent id is required")
	}
	if !state.Valid() {
		return fmt.Errorf("switch set managed agent state: invalid desired state %q", state)
	}
	return c.guarded(ctx, "set managed agent state", func(ctx context.Context) error {
		var reply struct {
			AgentID string `json:"id"`
		}
		if err := c.operation(ctx, "update_agent_detail", map[string]any{
			"agent_id": agentID, "desired_state": string(state),
		}, &reply); err != nil {
			return err
		}
		if reply.AgentID != agentID {
			return errors.New("switch update_agent_detail: reply does not match requested agent")
		}
		return nil
	})
}

func nilIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// setManagedAgentModel moves an existing managed agent to model. Switch stores the model with the agent and pins it on
// every session it starts, so the agent's own OpenCode config cannot change it.
func (c *Client) setManagedAgentModel(ctx context.Context, agentID, model string) error {
	if agentID == "" || model == "" {
		return errors.New("switch update_agent_detail: agent id and model are required")
	}
	var reply json.RawMessage
	if err := c.operation(ctx, "update_agent_detail", map[string]any{"agent_id": agentID, "model": model}, &reply); err != nil {
		return fmt.Errorf("switch update_agent_detail %s: %w", agentID, err)
	}
	return nil
}
