package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// TaskIndex queries the event log to find active tasks for agents without requiring a separate table (TECH_SPEC 6.2).
type TaskIndex struct {
	pool *pgxpool.Pool
}

var _ ports.TaskIndex = (*TaskIndex)(nil)

// NewTaskIndex wraps a pgxpool.Pool for querying active tasks.
func NewTaskIndex(pool *pgxpool.Pool) *TaskIndex {
	return &TaskIndex{pool: pool}
}

const activeTaskQuery = `
WITH latest_assigned AS (
	SELECT DISTINCT ON (change_id)
		change_id,
		payload->>'task_id' AS task_id,
		version,
		occurred_at,
		seq
	FROM event
	WHERE type = 'task.assigned'
	  AND payload->>'agent' = $1
	  AND payload->>'role' = $2
	ORDER BY change_id, version DESC
)
SELECT a.change_id, a.task_id
FROM latest_assigned a
WHERE NOT EXISTS (
	SELECT 1 FROM event e
	WHERE e.change_id = a.change_id
	  AND e.version > a.version
	  AND (
		  (e.type IN ('task.timed_out', 'task.failed') AND e.payload->>'task_id' = a.task_id)
		  OR ($2 = 'test-author' AND e.type = 'tests.submitted')
		  OR ($2 = 'reviewer' AND e.type = 'review.submitted' AND (e.payload->>'reviewer' = $1 OR e.actor_id = $1))
		  OR ($2 = 'resolver' AND e.type = 'verdict.submitted')
	  )
)
ORDER BY
	-- A task whose change has a question no person has answered yet comes after every task that is not waiting.
	((SELECT count(*) FROM event q WHERE q.change_id = a.change_id AND q.version > a.version AND q.type = 'question.asked')
	 > (SELECT count(*) FROM event r WHERE r.change_id = a.change_id AND r.version > a.version AND r.type = 'question.answered')) ASC,
	a.occurred_at ASC, a.seq ASC
LIMIT 1;
`

// ActiveTask finds the active task of an agent in a role.
//
// The open task of an agent is the latest task.assigned event whose payload has that agent and role,
// on a change whose stream has no later event that ends that task (task.timed_out or task.failed with
// the same task id, tests.submitted for the test-author task, review.submitted by that agent after the
// assignment, verdict.submitted for the resolver).
//
// If the agent has more than one open task, ActiveTask returns the oldest one (by occurred_at, seq) and
// lets the caller finish it first, except that a task waiting on an unanswered human question comes after any
// task that is not waiting.
// If no open task exists, it returns ports.ErrNotFound.
func (idx *TaskIndex) ActiveTask(ctx context.Context, agent id.AgentName, role workflow.Role) (ports.ActiveTaskRef, error) {
	var cid, tid string
	err := idx.pool.QueryRow(ctx, activeTaskQuery, string(agent), string(role)).Scan(&cid, &tid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ports.ActiveTaskRef{}, ports.ErrNotFound
		}
		return ports.ActiveTaskRef{}, fmt.Errorf("lookup active task for agent %q role %q: %w", agent, role, err)
	}

	ref := ports.ActiveTaskRef{
		ChangeID: id.ChangeID(cid),
		TaskID:   id.TaskID(tid),
	}
	if err := ref.Validate(); err != nil {
		return ports.ActiveTaskRef{}, fmt.Errorf("active task ref: %w", err)
	}
	return ref, nil
}
