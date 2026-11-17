package questions

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

func TestDue(t *testing.T) {
	asked := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
	timeouts := policy.Timeouts{RemindAfter: policy.Duration(2 * time.Hour), Question: policy.Duration(4 * time.Hour),
		EscalateAfter: policy.Duration(8 * time.Hour), RejectAfter: policy.Duration(24 * time.Hour)}
	at := asked.Add
	tests := []struct {
		name string
		q    Question
		now  time.Time
		want []Action
	}{
		{"nothing yet", Question{AskedAt: asked}, at(time.Hour), nil},
		{"remind", Question{AskedAt: asked}, at(2 * time.Hour), []Action{Remind}},
		{"reminded once only", Question{AskedAt: asked, Done: map[Action]bool{Remind: true}}, at(3 * time.Hour), nil},
		{"assume after the question timeout", Question{AskedAt: asked, Done: map[Action]bool{Remind: true}}, at(4 * time.Hour), []Action{Assume}},
		{"late caller gets everything due, in order", Question{AskedAt: asked}, at(9 * time.Hour), []Action{Remind, Assume, Escalate}},
		{"answered needs nothing", Question{AskedAt: asked, Answered: true}, at(9 * time.Hour), nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, Due(tc.q, timeouts, tc.now))
		})
	}
}
