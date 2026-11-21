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

// DecodeTaskAssignPayload strictly decodes and validates a task_assign payload.
func DecodeTaskAssignPayload(raw json.RawMessage) (TaskAssignPayload, error) {
	var p TaskAssignPayload
	if err := decodeEffectPayload(raw, []string{"agent", "role", "task_id", "room_id"}, &p); err != nil {
		return TaskAssignPayload{}, fmt.Errorf("task_assign payload: %w", err)
	}
	return p, nil
}
