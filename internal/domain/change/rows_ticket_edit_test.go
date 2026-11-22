package change_test

import (
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/stretchr/testify/require"
)

func TestTicketEditAddingCriterionReopensWriteTestsRow(t *testing.T) {
	t.Parallel()
	in := change.In{
		Change: "KAN-9",
		View: change.View{
			Trigger: &change.TriggerInfo{
				Ticket:         "KAN-9",
				Repo:           "org/repo",
				Criteria:       []string{"AC1", "AC2"},
				CriteriaEdited: true,
			},
			Room:   "room-9",
			Branch: &change.BranchInfo{Repo: "org/repo", Branch: "KAN-9-tests"},
			Tests: &change.TestsInfo{
				Attempts: 1,
				ACMap:    map[string][]string{"AC1": {"test_ac1"}},
			},
			Tasks: map[id.TaskID]change.TaskInfo{},
		},
	}
	row := findRowByID(t, change.Rows(), "write_tests")
	require.False(t, row.Done(in), "write_tests must re-open when AC2 is uncovered after ticket.changed")
	actions := row.Next(in)
	require.Len(t, actions, 1)
	require.NotNil(t, actions[0].Task, "write_tests must emit a tests task for uncovered criteria")
	require.Equal(t, "tests", actions[0].Task.Kind)
	require.Contains(t, actions[0].Task.Input, "AC2")
}

func findRowByID(t *testing.T, rows []change.Row, rowID string) change.Row {
	t.Helper()
	for _, r := range rows {
		if r.ID == rowID {
			return r
		}
	}
	t.Fatalf("row %q not found", rowID)
	return change.Row{}
}
