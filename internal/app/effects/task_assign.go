package effects

import (
	"context"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// TaskAssignDeps are the dependencies of TaskAssign.
type TaskAssignDeps struct {
	Chat    ports.Chat
	Inviter ports.RoomInviter
	// RoleRooms sends the assignment of a role to a standing channel (the reviews channel for reviewers and resolvers)
	// instead of the room the change opened. A role not listed uses the change's room.
	RoleRooms map[workflow.Role]change.RoomID
	// Focus, when set, remembers which ticket each agent was last woken for, so get_task without a ticket returns it.
	Focus ports.CursorTable
}

// TaskAssign performs task_assign effects: it invites the agent and posts the line that wakes it.
type TaskAssign struct {
	deps TaskAssignDeps
}

// NewTaskAssign builds the executor, or refuses when a dependency is missing.
func NewTaskAssign(d TaskAssignDeps) (TaskAssign, error) {
	if d.Chat == nil {
		return TaskAssign{}, fmt.Errorf("task_assign executor: missing chat")
	}
	if d.Inviter == nil {
		return TaskAssign{}, fmt.Errorf("task_assign executor: missing inviter")
	}
	return TaskAssign{deps: d}, nil
}

// Execute invites the agent into the room and posts the wake line.
func (t TaskAssign) Execute(ctx context.Context, fx change.Effect) error {
	if fx.Kind != change.EffectTaskAssign {
		return failf(nil, "task_assign executor: effect %s has kind %s", fx.Key, fx.Kind)
	}
	p, err := change.DecodeTaskAssignPayload(fx.Payload)
	if err != nil {
		return failf(err, "task_assign effect %s: payload refused", fx.Key)
	}

	room := p.RoomID
	if standing, ok := t.deps.RoleRooms[p.Role]; ok {
		room = standing
	}
	if err := t.deps.Inviter.InviteAgent(ctx, room, string(p.Agent)); err != nil {
		return failf(err, "task_assign %s: invite %s into the room", fx.Key, p.Agent)
	}

	ticket, err := changeOfEffect(p.Change, fx.Key)
	if err != nil {
		return failf(err, "task_assign %s: read the ticket from the effect key", fx.Key)
	}
	text := TaskAssignText(string(p.Agent), string(ticket), p.Role)
	err = t.deps.Chat.Post(ctx, room, fx.Key, text)
	if err != nil {
		return failf(err, "task_assign %s: post room message", fx.Key)
	}
	if t.deps.Focus != nil {
		if err := t.deps.Focus.SetCursor(ctx, FocusSource(string(p.Agent)), string(ticket)); err != nil {
			return failf(err, "task_assign %s: remember the agent's ticket", fx.Key)
		}
	}

	return nil
}

// FocusSource names where the ticket an agent was last woken for is kept.
func FocusSource(agent string) string { return "focus:" + agent }

// TaskAssignText is the room line that wakes an agent. People read the room too (Discord, Telegram), so it says what
// the agent will do before the cue the agent acts on.
func TaskAssignText(agent, ticket string, role workflow.Role) string {
	return fmt.Sprintf("@%s %s is yours: %s. Start with heimdall_get_task, it has your full instructions.", agent, ticket, roleWork(role))
}

// roleWork names the work of a role in a few plain words.
func roleWork(role workflow.Role) string {
	if k, ok := workflow.KindFor(role); ok {
		return k.Work
	}
	return "see the task"
}
