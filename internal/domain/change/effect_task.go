package change

import (
	"encoding/json"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// MaxBundleBytes is the maximum allowed size of the bundle in bytes (1 MiB).
const MaxBundleBytes = 1024 * 1024

// TaskAssignPayload is the payload of a task_assign effect.
type TaskAssignPayload struct {
	Agent  id.AgentName  `json:"agent"`
	Role   workflow.Role `json:"role"`
	TaskID id.TaskID     `json:"task_id"`
	RoomID RoomID        `json:"room_id"`
	// Bundle is the old context bundle. New tasks may leave it empty: inputs are rendered when the agent calls get_task.
	Bundle []byte `json:"bundle,omitempty"`
}

// Validate returns every problem with the payload.
func (p TaskAssignPayload) Validate() error {
	var v problems
	v.add("agent", checkAgent(p.Agent))
	v.add("role", checkRole(p.Role))
	v.add("task_id", checkTask(p.TaskID))
	_, err := ParseRoomID(string(p.RoomID))
	v.add("room_id", err)
	if len(p.Bundle) > MaxBundleBytes {
		v.add("bundle", fmt.Errorf("length %d exceeds maximum of %d bytes", len(p.Bundle), MaxBundleBytes))
	}
	return v.join()
}

// AgentResetPayload is the payload of an agent_reset effect: whose session to reset, in which room.
type AgentResetPayload struct {
	Agent  id.AgentName `json:"agent"`
	RoomID RoomID       `json:"room_id"`
}

// Validate returns every problem with the payload.
func (p AgentResetPayload) Validate() error {
	var v problems
	v.add("agent", checkAgent(p.Agent))
	_, err := ParseRoomID(string(p.RoomID))
	v.add("room_id", err)
	return v.join()
}

// DecodeAgentResetPayload strictly decodes and validates an agent_reset payload.
func DecodeAgentResetPayload(raw json.RawMessage) (AgentResetPayload, error) {
	var p AgentResetPayload
	if err := decodeEffectPayload(raw, []string{"agent", "room_id"}, &p); err != nil {
		return AgentResetPayload{}, fmt.Errorf("agent_reset payload: %w", err)
	}
	return p, nil
}

// DecodeTaskAssignPayload strictly decodes and validates a task_assign payload.
func DecodeTaskAssignPayload(raw json.RawMessage) (TaskAssignPayload, error) {
	var p TaskAssignPayload
	if err := decodeEffectPayload(raw, []string{"agent", "role", "task_id", "room_id"}, &p); err != nil {
		return TaskAssignPayload{}, fmt.Errorf("task_assign payload: %w", err)
	}
	return p, nil
}
