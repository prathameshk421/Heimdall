package agenttools_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/agenttools"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// makeProfileStream is a change whose one event assigns the profile task to testAgent as the profiler.
func makeProfileStream(t *testing.T, h *testHarness) {
	t.Helper()
	payload, err := json.Marshal(change.TaskAssigned{TaskID: testTaskID, Agent: testAgent, Role: workflow.RoleProfiler,
		Deadline: t0.Add(time.Hour), Kind: "profile", Subject: "acme/pay", Input: "base-" + strings.Repeat("a", 40)})
	require.NoError(t, err)
	first := h.ids.NewEvent()
	e := change.Event{ID: first, ChangeID: testChangeID, Version: 1, Type: change.EventTaskAssigned,
		Actor: change.Actor{Kind: change.ActorSystem, ID: "heimdall"}, OccurredAt: t0,
		CorrelationID: change.CorrelationID(first.String()), Payload: payload}
	_, err = h.events.Append(context.Background(), testChangeID, 0, []change.Event{e}, nil)
	require.NoError(t, err)
}

func profilerAssignment() agenttools.Assignment {
	return agenttools.Assignment{ChangeID: testChangeID, TaskID: testTaskID, Agent: testAgent, Role: workflow.RoleProfiler,
		Deadline: t0.Add(time.Hour)}
}

// A proposal is recorded on the change and ends the profiler's task (D175).
func TestARecordedProfileProposalEndsTheTask(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeProfileStream(t, h)
	proposal := strings.Repeat("ab", 32)

	require.NoError(t, h.svc.RecordProfileProposed(context.Background(), profilerAssignment(), proposal, "acme/pay"))

	evs, err := h.events.Load(context.Background(), testChangeID)
	require.NoError(t, err)
	require.Len(t, evs, 2)
	require.Equal(t, change.EventProfileProposed, evs[1].Type)
	v, err := change.Fold(evs)
	require.NoError(t, err)
	require.Equal(t, change.TaskDoneStatus, v.Tasks[testTaskID].Status)

	err = h.svc.RecordProfileProposed(context.Background(), profilerAssignment(), proposal, "acme/pay")
	require.ErrorIs(t, err, agenttools.ErrNoTask, "a done task takes no second proposal")
}

func TestOnlyTheProfilerRecordsAProposal(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	makeProfileStream(t, h)
	a := profilerAssignment()
	a.Role = workflow.RoleReviewer
	err := h.svc.RecordProfileProposed(context.Background(), a, strings.Repeat("ab", 32), "acme/pay")
	require.ErrorIs(t, err, agenttools.ErrWrongRole)
}
