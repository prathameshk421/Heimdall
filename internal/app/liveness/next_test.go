package liveness

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Every row of AGENT_INTERACTION_ARCH 6.3.
func TestNext(t *testing.T) {
	t0 := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
	w := Windows{Ack: time.Minute, Stall: 10 * time.Minute}
	at := t0.Add
	tests := []struct {
		name string
		f    Facts
		now  time.Time
		want Action
	}{
		{"fresh task", Facts{Since: t0}, at(30 * time.Second), Wait},
		{"no ack in the ack window, no repeat yet", Facts{Since: t0}, at(2 * time.Minute), Repeat},
		{"repeated, still inside the stall window", Facts{Since: t0, Repeated: true}, at(5 * time.Minute), Wait},
		{"acked and working", Facts{Since: t0, Acked: true, LastCall: at(8 * time.Minute)}, at(12 * time.Minute), Wait},
		{"acked, silent for the stall window, provider not probed", Facts{Since: t0, Acked: true, LastCall: at(time.Minute)}, at(12 * time.Minute), Probe},
		{"provider refuses", Facts{Since: t0, Acked: true, Provider: Refusing}, at(12 * time.Minute), Blocked},
		{"provider refuses even before the ack window", Facts{Since: t0, Provider: Refusing}, at(2 * time.Minute), Blocked},
		{"provider fine, no reset yet", Facts{Since: t0, Acked: true, Provider: Healthy}, at(12 * time.Minute), Reset},
		{"reset, waiting", Facts{Since: t0, Acked: true, Provider: Healthy, ResetAt: at(12 * time.Minute)}, at(15 * time.Minute), Wait},
		{"still silent after the reset", Facts{Since: t0, Acked: true, Provider: Healthy, ResetAt: at(12 * time.Minute)}, at(23 * time.Minute), Fail},
		{"called after the reset", Facts{Since: t0, Acked: true, Provider: Healthy, ResetAt: at(12 * time.Minute), LastCall: at(20 * time.Minute)}, at(23 * time.Minute), Wait},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, Next(tc.f, w, tc.now))
		})
	}
}
