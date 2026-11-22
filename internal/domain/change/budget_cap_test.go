package change_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

// P4: over the daily spend cap, no new agent task is assigned and Missing names the cap.
func TestSpendCapPausesTaskAssignmentsAndNamesCapInMissing(t *testing.T) {
	v := fold(t,
		triggered(t),
		ev(t, 2, change.EventRoomOpened, change.RoomOpened{RoomID: "switch:r1"}),
		ev(t, 3, change.EventBranchEnsured, change.BranchEnsured{Repo: "acme/pay", Branch: "KAN-5-x", BaseSHA: fBase}),
	)
	in := inOf("KAN-5", v, t0)
	require.NotEmpty(t, actionsOf(in)["write_tests"], "under cap, write_tests assigns a task")

	in.SpendCapExceeded = true
	in.SpendSummary = "daily spend cap reached ($3.00 / $3.00); new agent tasks are paused"
	for _, ra := range change.Next(in) {
		require.Nil(t, ra.Task, "no new agent task is assigned while over the daily cap")
	}
	needs := change.Missing(in)
	var found bool
	for _, n := range needs {
		if n.Step == "budget" {
			found = true
			require.Contains(t, n.Text, "daily spend cap reached ($3.00 / $3.00)")
		}
	}
	require.True(t, found, "Missing names the spend cap")
}
