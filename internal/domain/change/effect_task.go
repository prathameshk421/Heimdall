package change

import (
	"encoding/json"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// TaskAssignPayload is the payload of a task_assign effect.
type TaskAssignPayload struct {
	Agent  id.AgentName  `json:"agent"`
	Role   workflow.Role `json:"role"`
	TaskID id.TaskID     `json:"task_id"`
	// Change is the change the task belongs to. Effects made before it existed carry it in their key only.
	Change id.ChangeID `json:"change,omitempty"`
	RoomID RoomID      `json:"room_id"`
}

// Validate returns every problem with the payload.
func (p TaskAssignPayload) Validate() error {
	var v problems
	v.add("agent", checkAgent(p.Agent))
	v.add("role", checkRole(p.Role))
	v.add("task_id", checkTask(p.TaskID))
	_, err := ParseRoomID(string(p.RoomID))
	v.add("room_id", err)
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
