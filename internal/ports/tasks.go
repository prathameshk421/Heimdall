package ports

import (
	"context"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// ActiveTaskRef names the change and task of an agent's active task.
type ActiveTaskRef struct {
	ChangeID id.ChangeID `json:"change_id"`
	TaskID   id.TaskID   `json:"task_id"`
}

// Validate returns an error if ChangeID or TaskID is invalid.
func (r ActiveTaskRef) Validate() error {
	if _, err := id.ParseChangeID(string(r.ChangeID)); err != nil {
		return fmt.Errorf("active task ref change_id: %w", err)
	}
	if _, err := id.ParseTaskID(string(r.TaskID)); err != nil {
		return fmt.Errorf("active task ref task_id: %w", err)
	}
	return nil
}

// TaskIndex finds the active task of an agent in a role (TECH_SPEC 6.2).
// Consumer: internal/app/agenttools.
type TaskIndex interface {
	ActiveTask(ctx context.Context, agent id.AgentName, role workflow.Role) (ActiveTaskRef, error)
}
