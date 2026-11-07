package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/effects"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/postgres"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// nudgeAfter is how long an assigned task may wait for its ack before the agent is mentioned again. Each further nudge
// waits one more step, so a slow agent is not spammed.
const (
	nudgeAfter = 45 * time.Second
	nudgeMax   = 12
)

// nudgeAgent is a workflow agent whose open task can be nudged.
type nudgeAgent struct {
	name string
	role workflow.Role
}

// taskNudger mentions an agent again when its assigned task has gone unacknowledged. Switch wakes an agent only on a
// mention, and a mention can be lost while the agent's session is busy or cold, which left a retried task waiting.
type taskNudger struct {
	index       ports.TaskIndex
	events      ports.EventStore
	chat        ports.Chat
	cursors     ports.CursorTable
	clock       ports.Clock
	agents      []nudgeAgent
	reviewsRoom change.RoomID
}

// RunOnce nudges every agent whose open task is overdue for its ack and returns how many nudges it posted.
func (n *taskNudger) RunOnce(ctx context.Context) (int, error) {
	posted := 0
	for _, a := range n.agents {
		ref, err := n.index.ActiveTask(ctx, id.AgentName(a.name), a.role)
		if errors.Is(err, ports.ErrNotFound) {
			continue
		}
		if err != nil {
			return posted, fmt.Errorf("nudge: find the open task of %s: %w", a.name, err)
		}
		sent, err := n.nudge(ctx, a, ref)
		if err != nil {
			return posted, err
		}
		if sent {
			posted++
		}
	}
	return posted, nil
}

func (n *taskNudger) nudge(ctx context.Context, a nudgeAgent, ref ports.ActiveTaskRef) (bool, error) {
	events, err := n.events.Load(ctx, ref.ChangeID)
	if err != nil {
		return false, fmt.Errorf("nudge: load %s: %w", ref.ChangeID, err)
	}
	assignedAt, acked, room := taskFacts(events, string(ref.TaskID))
	if acked || assignedAt.IsZero() {
		return false, nil
	}
	room = n.roomFor(a, room)
	if room == "" {
		return false, nil
	}
	source := "nudge:" + string(ref.TaskID)
	count, err := n.nudgeCount(ctx, source)
	if err != nil {
		return false, err
	}
	if count >= nudgeMax || n.clock.Now().Sub(assignedAt) < nudgeAfter*time.Duration(count+1) {
		return false, nil
	}
	key, err := change.IdempotencyKey(ref.ChangeID, 1, change.EffectRoomPost, fmt.Sprintf("%s:%d", source, count))
	if err != nil {
		return false, fmt.Errorf("nudge: build key: %w", err)
	}
	text := fmt.Sprintf("@%s %s is waiting for you. Call heimdall_get_task, it has your full instructions, then heimdall_ack.", a.name, ref.ChangeID)
	if err := n.chat.Post(ctx, room, key, text); err != nil {
		return false, fmt.Errorf("nudge: post to %s: %w", room, err)
	}
	if err := n.cursors.SetCursor(ctx, source, strconv.Itoa(count+1)); err != nil {
		return false, fmt.Errorf("nudge: save count: %w", err)
	}
	if err := n.cursors.SetCursor(ctx, effects.FocusSource(a.name), string(ref.ChangeID)); err != nil {
		return false, fmt.Errorf("nudge: remember the agent's ticket: %w", err)
	}
	return true, nil
}

// roomFor is the room an agent is nudged in: the reviews channel for reviewers and the resolver, the change's own room
// for the test author.
func (n *taskNudger) roomFor(a nudgeAgent, changeRoom change.RoomID) change.RoomID {
	if a.role != workflow.RoleTestAuthor && n.reviewsRoom != "" {
		return n.reviewsRoom
	}
	return changeRoom
}

// nudgeCount is how many times the task was nudged so far.
func (n *taskNudger) nudgeCount(ctx context.Context, source string) (int, error) {
	raw, err := n.cursors.GetCursor(ctx, source)
	if errors.Is(err, ports.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("nudge: read count: %w", err)
	}
	count, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("nudge: count %q is not a number: %w", raw, err)
	}
	return count, nil
}

// taskFacts reads, from the change's events, when the task was assigned, whether it was acknowledged, and the room
// the change opened.
func taskFacts(events []change.Event, taskID string) (assignedAt time.Time, acked bool, room change.RoomID) {
	for _, ev := range events {
		var p struct {
			TaskID string        `json:"task_id"`
			RoomID change.RoomID `json:"room_id"`
		}
		if json.Unmarshal(ev.Payload, &p) != nil {
			continue
		}
		if ev.Type == change.EventTaskAssigned && p.TaskID == taskID {
			assignedAt = ev.OccurredAt
		}
		if ev.Type == change.EventTaskAcked && p.TaskID == taskID {
			acked = true
		}
		if ev.Type == change.EventRoomOpened {
			room = p.RoomID
		}
	}
	return assignedAt, acked, room
}

// newTaskNudger builds the nudger for the configured workflow agents.
func newTaskNudger(ad adapters, store ports.EventStore, cursors ports.CursorTable, pool *pgxpool.Pool, clk ports.Clock) *taskNudger {
	var agents []nudgeAgent
	if ad.agents.TestAuthor != "" {
		agents = append(agents, nudgeAgent{name: ad.agents.TestAuthor, role: workflow.RoleTestAuthor})
	}
	for _, r := range ad.agents.Reviewers {
		if r != "" {
			agents = append(agents, nudgeAgent{name: r, role: workflow.RoleReviewer})
		}
	}
	if ad.agents.Resolver != "" {
		agents = append(agents, nudgeAgent{name: ad.agents.Resolver, role: workflow.RoleResolver})
	}
	return &taskNudger{
		index: postgres.NewTaskIndex(pool), events: store, chat: ad.chat, cursors: cursors, clock: clk,
		agents: agents,
	}
}
