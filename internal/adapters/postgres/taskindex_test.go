//go:build component

package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const (
	testBundleBlobRef = workflow.BlobRef("blob:sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	testSHA           = id.SHA("0123456789abcdef0123456789abcdef01234567")
	testCaseID        = id.CaseID("case_01HZX3K9QW7M2P5R8T0V4YB6CD")
)

func taskAssignedEvent(cid id.ChangeID, ver int64, taskID id.TaskID, agent id.AgentName, role workflow.Role, t time.Time) change.Event {
	raw, err := json.Marshal(change.TaskAssigned{
		TaskID:    taskID,
		Agent:     agent,
		Role:      role,
		Deadline:  t.Add(30 * time.Minute),
		BundleRef: testBundleBlobRef,
	})
	if err != nil {
		panic(err)
	}
	return change.Event{
		ID:            newEventID(),
		ChangeID:      cid,
		Version:       ver,
		Type:          change.EventTaskAssigned,
		Actor:         change.Actor{Kind: change.ActorSystem, ID: "engine"},
		OccurredAt:    t,
		CorrelationID: "corr-1",
		Payload:       raw,
	}
}

func taskTimedOutEvent(cid id.ChangeID, ver int64, taskID id.TaskID, agent id.AgentName, role workflow.Role, t time.Time) change.Event {
	raw, err := json.Marshal(change.TaskTimedOut{
		TaskID: taskID,
		Agent:  agent,
		Role:   role,
		Reason: "deadline exceeded",
	})
	if err != nil {
		panic(err)
	}
	return change.Event{
		ID:            newEventID(),
		ChangeID:      cid,
		Version:       ver,
		Type:          change.EventTaskTimedOut,
		Actor:         change.Actor{Kind: change.ActorSystem, ID: "engine"},
		OccurredAt:    t,
		CorrelationID: "corr-1",
		Payload:       raw,
	}
}

func taskFailedEvent(cid id.ChangeID, ver int64, taskID id.TaskID, agent id.AgentName, role workflow.Role, t time.Time) change.Event {
	raw, err := json.Marshal(change.TaskFailed{
		TaskID: taskID,
		Agent:  agent,
		Role:   role,
		Reason: "budget used up",
	})
	if err != nil {
		panic(err)
	}
	return change.Event{
		ID:            newEventID(),
		ChangeID:      cid,
		Version:       ver,
		Type:          change.EventTaskFailed,
		Actor:         change.Actor{Kind: change.ActorSystem, ID: "engine"},
		OccurredAt:    t,
		CorrelationID: "corr-1",
		Payload:       raw,
	}
}

func testsSubmittedEvent(cid id.ChangeID, ver int64, t time.Time) change.Event {
	raw, err := json.Marshal(change.TestsSubmitted{
		Refs:     []string{"tests/heimdall/PAY-100/suite_test.go"},
		ACMap:    map[string][]string{"AC1": {"tests/heimdall/PAY-100/suite_test.go"}},
		SHA:      testSHA,
		TreeHash: testSHA,
	})
	if err != nil {
		panic(err)
	}
	return change.Event{
		ID:            newEventID(),
		ChangeID:      cid,
		Version:       ver,
		Type:          change.EventTestsSubmitted,
		Actor:         change.Actor{Kind: change.ActorAgent, ID: "alice"},
		OccurredAt:    t,
		CorrelationID: "corr-1",
		Payload:       raw,
	}
}

func reviewSubmittedEvent(cid id.ChangeID, ver int64, reviewer id.AgentName, t time.Time) change.Event {
	fRef, err := id.ParseFindingRef("F1")
	if err != nil {
		panic(err)
	}
	raw, err := json.Marshal(change.ReviewSubmitted{
		Reviewer:    reviewer,
		FindingRefs: []id.FindingRef{fRef},
		PackDate:    "2026-11-05",
	})
	if err != nil {
		panic(err)
	}
	return change.Event{
		ID:            newEventID(),
		ChangeID:      cid,
		Version:       ver,
		Type:          change.EventReviewSubmitted,
		Actor:         change.Actor{Kind: change.ActorAgent, ID: change.ActorID(reviewer)},
		OccurredAt:    t,
		CorrelationID: "corr-1",
		Payload:       raw,
	}
}

func verdictSubmittedEvent(cid id.ChangeID, ver int64, t time.Time) change.Event {
	raw, err := json.Marshal(change.VerdictSubmitted{
		CaseID:    testCaseID,
		Verdict:   change.VerdictUphold,
		Citations: []string{"test citation"},
		Checks:    []string{},
	})
	if err != nil {
		panic(err)
	}
	return change.Event{
		ID:            newEventID(),
		ChangeID:      cid,
		Version:       ver,
		Type:          change.EventVerdictSubmitted,
		Actor:         change.Actor{Kind: change.ActorAgent, ID: "alice"},
		OccurredAt:    t,
		CorrelationID: "corr-1",
		Payload:       raw,
	}
}

func TestTaskIndex(t *testing.T) {
	ctx := context.Background()
	srv := startServer(t)

	cid1 := id.ChangeID("PAY-101")
	cid2 := id.ChangeID("PAY-102")

	alice := id.AgentName("alice")
	bob := id.AgentName("bob")

	task1 := id.TaskID("tsk_01HZX3K9QW7M2P5R8T0V4YB6AA")
	task2 := id.TaskID("tsk_01HZX3K9QW7M2P5R8T0V4YB6BB")
	task3 := id.TaskID("tsk_01HZX3K9QW7M2P5R8T0V4YB6CC")

	t.Run("no task returns ErrNotFound", func(t *testing.T) {
		db := srv.database(ctx, t)
		idx := NewTaskIndex(db.pool)

		_, err := idx.ActiveTask(ctx, alice, workflow.RoleReviewer)
		require.ErrorIs(t, err, ports.ErrNotFound)
	})

	t.Run("one open task is returned", func(t *testing.T) {
		db := srv.database(ctx, t)
		s := NewStore(db.pool)
		idx := NewTaskIndex(db.pool)

		evs := []change.Event{
			roomEvent(cid1, 1, "room-1"),
			taskAssignedEvent(cid1, 2, task1, alice, workflow.RoleReviewer, base.Add(2*time.Second)),
		}
		_, err := s.Append(ctx, cid1, 0, evs, nil)
		require.NoError(t, err)

		ref, err := idx.ActiveTask(ctx, alice, workflow.RoleReviewer)
		require.NoError(t, err)
		require.Equal(t, cid1, ref.ChangeID)
		require.Equal(t, task1, ref.TaskID)
	})

	t.Run("task ended by submission", func(t *testing.T) {
		t.Run("test-author ended by tests.submitted", func(t *testing.T) {
			db := srv.database(ctx, t)
			s := NewStore(db.pool)
			idx := NewTaskIndex(db.pool)

			evs := []change.Event{
				roomEvent(cid1, 1, "room-1"),
				taskAssignedEvent(cid1, 2, task1, alice, workflow.RoleTestAuthor, base.Add(2*time.Second)),
				testsSubmittedEvent(cid1, 3, base.Add(3*time.Second)),
			}
			_, err := s.Append(ctx, cid1, 0, evs, nil)
			require.NoError(t, err)

			_, err = idx.ActiveTask(ctx, alice, workflow.RoleTestAuthor)
			require.ErrorIs(t, err, ports.ErrNotFound)
		})

		t.Run("reviewer ended by review.submitted", func(t *testing.T) {
			db := srv.database(ctx, t)
			s := NewStore(db.pool)
			idx := NewTaskIndex(db.pool)

			evs := []change.Event{
				roomEvent(cid1, 1, "room-1"),
				taskAssignedEvent(cid1, 2, task1, alice, workflow.RoleReviewer, base.Add(2*time.Second)),
				reviewSubmittedEvent(cid1, 3, alice, base.Add(3*time.Second)),
			}
			_, err := s.Append(ctx, cid1, 0, evs, nil)
			require.NoError(t, err)

			_, err = idx.ActiveTask(ctx, alice, workflow.RoleReviewer)
			require.ErrorIs(t, err, ports.ErrNotFound)
		})

		t.Run("resolver ended by verdict.submitted", func(t *testing.T) {
			db := srv.database(ctx, t)
			s := NewStore(db.pool)
			idx := NewTaskIndex(db.pool)

			evs := []change.Event{
				roomEvent(cid1, 1, "room-1"),
				taskAssignedEvent(cid1, 2, task1, alice, workflow.RoleResolver, base.Add(2*time.Second)),
				verdictSubmittedEvent(cid1, 3, base.Add(3*time.Second)),
			}
			_, err := s.Append(ctx, cid1, 0, evs, nil)
			require.NoError(t, err)

			_, err = idx.ActiveTask(ctx, alice, workflow.RoleResolver)
			require.ErrorIs(t, err, ports.ErrNotFound)
		})
	})

	t.Run("ended by timeout or failure", func(t *testing.T) {
		t.Run("ended by timeout with same task id", func(t *testing.T) {
			db := srv.database(ctx, t)
			s := NewStore(db.pool)
			idx := NewTaskIndex(db.pool)

			evs := []change.Event{
				roomEvent(cid1, 1, "room-1"),
				taskAssignedEvent(cid1, 2, task1, alice, workflow.RoleReviewer, base.Add(2*time.Second)),
				taskTimedOutEvent(cid1, 3, task1, alice, workflow.RoleReviewer, base.Add(3*time.Second)),
			}
			_, err := s.Append(ctx, cid1, 0, evs, nil)
			require.NoError(t, err)

			_, err = idx.ActiveTask(ctx, alice, workflow.RoleReviewer)
			require.ErrorIs(t, err, ports.ErrNotFound)
		})

		t.Run("ended by task.failed with same task id", func(t *testing.T) {
			db := srv.database(ctx, t)
			s := NewStore(db.pool)
			idx := NewTaskIndex(db.pool)

			evs := []change.Event{
				roomEvent(cid1, 1, "room-1"),
				taskAssignedEvent(cid1, 2, task1, alice, workflow.RoleReviewer, base.Add(2*time.Second)),
				taskFailedEvent(cid1, 3, task1, alice, workflow.RoleReviewer, base.Add(3*time.Second)),
			}
			_, err := s.Append(ctx, cid1, 0, evs, nil)
			require.NoError(t, err)

			_, err = idx.ActiveTask(ctx, alice, workflow.RoleReviewer)
			require.ErrorIs(t, err, ports.ErrNotFound)
		})
	})

	t.Run("two agents do not see each others tasks", func(t *testing.T) {
		db := srv.database(ctx, t)
		s := NewStore(db.pool)
		idx := NewTaskIndex(db.pool)

		evs := []change.Event{
			roomEvent(cid1, 1, "room-1"),
			taskAssignedEvent(cid1, 2, task1, alice, workflow.RoleReviewer, base.Add(2*time.Second)),
		}
		_, err := s.Append(ctx, cid1, 0, evs, nil)
		require.NoError(t, err)

		_, err = idx.ActiveTask(ctx, bob, workflow.RoleReviewer)
		require.ErrorIs(t, err, ports.ErrNotFound)

		ref, err := idx.ActiveTask(ctx, alice, workflow.RoleReviewer)
		require.NoError(t, err)
		require.Equal(t, cid1, ref.ChangeID)
		require.Equal(t, task1, ref.TaskID)
	})

	t.Run("role mismatch returns ErrNotFound", func(t *testing.T) {
		db := srv.database(ctx, t)
		s := NewStore(db.pool)
		idx := NewTaskIndex(db.pool)

		evs := []change.Event{
			roomEvent(cid1, 1, "room-1"),
			taskAssignedEvent(cid1, 2, task1, alice, workflow.RoleReviewer, base.Add(2*time.Second)),
		}
		_, err := s.Append(ctx, cid1, 0, evs, nil)
		require.NoError(t, err)

		_, err = idx.ActiveTask(ctx, alice, workflow.RoleTestAuthor)
		require.ErrorIs(t, err, ports.ErrNotFound)
	})

	t.Run("two open tasks return the oldest", func(t *testing.T) {
		db := srv.database(ctx, t)
		s := NewStore(db.pool)
		idx := NewTaskIndex(db.pool)

		// Change 1 assigned at base + 10s (older)
		evs1 := []change.Event{
			roomEvent(cid1, 1, "room-1"),
			taskAssignedEvent(cid1, 2, task1, alice, workflow.RoleReviewer, base.Add(10*time.Second)),
		}
		_, err := s.Append(ctx, cid1, 0, evs1, nil)
		require.NoError(t, err)

		// Change 2 assigned at base + 20s (newer)
		evs2 := []change.Event{
			roomEvent(cid2, 1, "room-2"),
			taskAssignedEvent(cid2, 2, task2, alice, workflow.RoleReviewer, base.Add(20*time.Second)),
		}
		_, err = s.Append(ctx, cid2, 0, evs2, nil)
		require.NoError(t, err)

		// ActiveTask must return the older task (task1 on cid1)
		ref, err := idx.ActiveTask(ctx, alice, workflow.RoleReviewer)
		require.NoError(t, err)
		require.Equal(t, cid1, ref.ChangeID)
		require.Equal(t, task1, ref.TaskID)
	})

	t.Run("a later event of another task does not end this one", func(t *testing.T) {
		db := srv.database(ctx, t)
		s := NewStore(db.pool)
		idx := NewTaskIndex(db.pool)

		// On same change:
		// task1 assigned to alice (reviewer)
		// task2 assigned to bob (reviewer)
		// bob's task times out (task2)
		// bob submits review for another task or another event
		evs := []change.Event{
			roomEvent(cid1, 1, "room-1"),
			taskAssignedEvent(cid1, 2, task1, alice, workflow.RoleReviewer, base.Add(2*time.Second)),
			taskAssignedEvent(cid1, 3, task2, bob, workflow.RoleReviewer, base.Add(3*time.Second)),
			taskTimedOutEvent(cid1, 4, task2, bob, workflow.RoleReviewer, base.Add(4*time.Second)),
			reviewSubmittedEvent(cid1, 5, bob, base.Add(5*time.Second)),
		}
		_, err := s.Append(ctx, cid1, 0, evs, nil)
		require.NoError(t, err)

		// Alice's task1 must still be open!
		ref, err := idx.ActiveTask(ctx, alice, workflow.RoleReviewer)
		require.NoError(t, err)
		require.Equal(t, cid1, ref.ChangeID)
		require.Equal(t, task1, ref.TaskID)
	})

	t.Run("superseded task on same change returns latest open task", func(t *testing.T) {
		db := srv.database(ctx, t)
		s := NewStore(db.pool)
		idx := NewTaskIndex(db.pool)

		// task1 timed out, then task3 assigned to alice
		evs := []change.Event{
			roomEvent(cid1, 1, "room-1"),
			taskAssignedEvent(cid1, 2, task1, alice, workflow.RoleReviewer, base.Add(2*time.Second)),
			taskTimedOutEvent(cid1, 3, task1, alice, workflow.RoleReviewer, base.Add(3*time.Second)),
			taskAssignedEvent(cid1, 4, task3, alice, workflow.RoleReviewer, base.Add(4*time.Second)),
		}
		_, err := s.Append(ctx, cid1, 0, evs, nil)
		require.NoError(t, err)

		ref, err := idx.ActiveTask(ctx, alice, workflow.RoleReviewer)
		require.NoError(t, err)
		require.Equal(t, cid1, ref.ChangeID)
		require.Equal(t, task3, ref.TaskID)
	})

	t.Run("migration 0003 creates index and applies on top of 0001 and 0002", func(t *testing.T) {
		db := srv.emptyDatabase(ctx, t)
		// Apply only 0001
		require.NoError(t, migrateOnly(ctx, db.pool, "0001_init.sql"))

		// Check index does not exist yet
		require.Equal(t, int64(0), countRows(ctx, t, db.pool,
			"SELECT count(*) FROM pg_indexes WHERE tablename = 'event' AND indexname = 'event_type_agent'"))

		// Apply all migrations
		require.NoError(t, Migrate(ctx, db.pool))

		// Check index exists
		require.Equal(t, int64(1), countRows(ctx, t, db.pool,
			"SELECT count(*) FROM pg_indexes WHERE tablename = 'event' AND indexname = 'event_type_agent'"))
	})
}
