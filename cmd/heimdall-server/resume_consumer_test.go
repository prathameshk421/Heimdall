package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
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
