package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/collaboration"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/commands"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/ids"
)

func recoveryEvent(t *testing.T, typ change.EventType, p change.Payload) change.Event {
	t.Helper()
	raw, err := json.Marshal(p)
	require.NoError(t, err)
	return change.Event{Type: typ, Payload: raw}
}

// A resume is refused unless the step is escalated now: a retry or a resume after the escalation releases it, and an
// escalation of another step does not count (A10 item 5).
func TestStepEscalatedNowFollowsTheLatestRecoveryEvent(t *testing.T) {
	esc := recoveryEvent(t, change.EventStepEscalated, change.StepEscalated{Step: "test_qa", Attempts: 2, Reason: "rejected"})
	retry := recoveryEvent(t, change.EventStepRetried, change.StepRetried{Step: "test_qa", Attempt: 3, Reasons: []string{"x"}})
	resumed := recoveryEvent(t, change.EventStepResumed, change.StepResumed{Step: "test_qa", By: "ana"})
	otherEsc := recoveryEvent(t, change.EventStepEscalated, change.StepEscalated{Step: "other", Attempts: 1, Reason: "x"})

	cases := []struct {
		name  string
		state []change.Event
		want  bool
	}{
		{"escalated", []change.Event{esc}, true},
		{"resumed after the escalation", []change.Event{esc, resumed}, false},
		{"retried after the escalation", []change.Event{esc, retry}, false},
		{"another step escalated", []change.Event{otherEsc}, false},
		{"never escalated", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := stepEscalatedNow(c.state, "test_qa")
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

// resumeState is a pull request whose profiler seat failed two tasks on the same input, so the row is capped although
// no step.escalated was ever written (D173). The cap asks people to run `heimdall resume <change> profile`.
func resumeState(t *testing.T) []change.Event {
	t.Helper()
	profileTask := func(task string) []change.Event {
		return []change.Event{
			recoveryEvent(t, change.EventTaskAssigned, change.TaskAssigned{TaskID: id.TaskID(task), Agent: "profiler",
				Role: workflow.RoleProfiler, Deadline: time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC), Kind: "profile",
				Subject: "acme/pay", Input: "first"}),
			recoveryEvent(t, change.EventTaskFailed, change.TaskFailed{TaskID: id.TaskID(task), Agent: "profiler",
				Role: workflow.RoleProfiler, Reason: "sandbox down"}),
		}
	}
	evs := []change.Event{
		recoveryEvent(t, change.EventPROpened, change.PROpened{Repo: "acme/pay", Number: 7, HeadSHA: "9f1c2ab", Author: "ann", BaseSHA: "7d3e9a1"}),
		recoveryEvent(t, change.EventSlopEvaluated, change.SlopEvaluated{Pass: true}),
	}
	evs = append(evs, profileTask("tsk_01HZX3K9QW7M2P5R8T0V4YB6C1")...)
	return append(evs, profileTask("tsk_01HZX3K9QW7M2P5R8T0V4YB6C2")...)
}

// A capped row is resumed: the cap never writes step.escalated, so the command accepts the row that RowCapped names
// and writes exactly one step.resumed (D173).
func TestResumeOfACappedProfileRowWritesOneStepResumed(t *testing.T) {
	runtime := &commandRuntime{ids: ids.New(), clock: clock.System{}}
	state := resumeState(t)
	require.True(t, change.RowCapped(mustFold(t, state), "profile"), "the fixture caps the profile row")
	got, err := runtime.resumeEvents(id.ChangeID("pr-acme-pay-7"), state, "ana", commands.SourceChat, "message-9",
		change.Actor{Kind: change.ActorHuman, ID: "ana"}, commands.Resume{Step: "profile"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, change.EventStepResumed, got[0].Type)
	resumed, ok := mustPayload[change.StepResumed](got[0])
	require.True(t, ok)
	require.Equal(t, change.StepResumed{Step: "profile", By: "ana"}, resumed)
}

// A row that is neither escalated nor capped is still refused, and the refusal is a refusal: the poller must not retry
// the comment (D173).
func TestResumeOfARowThatIsNeitherEscalatedNorCappedIsRefused(t *testing.T) {
	runtime := &commandRuntime{ids: ids.New(), clock: clock.System{}}
	_, err := runtime.resumeEvents(id.ChangeID("pr-acme-pay-7"), resumeState(t), "ana", commands.SourceChat, "message-10",
		change.Actor{Kind: change.ActorHuman, ID: "ana"}, commands.Resume{Step: "review"})
	require.Error(t, err)
	require.ErrorIs(t, err, collaboration.ErrRefused)
	require.Contains(t, err.Error(), "not escalated")
}

func mustFold(t *testing.T, state []change.Event) change.View {
	t.Helper()
	v, err := change.Fold(state)
	require.NoError(t, err)
	return v
}
