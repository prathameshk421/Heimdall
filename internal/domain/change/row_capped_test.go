package change_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

// RowCapped names the row whose seat used up its failures in the current round: the fact the resume command needs to
// know that a cap, and not only an escalation, is why nothing is running.
func TestRowCappedSeesTheRoundTheRowIsIn(t *testing.T) {
	t.Parallel()
	base := []change.Event{opened(t), ev(t, 2, change.EventSlopEvaluated, change.SlopEvaluated{Pass: true})}
	twice := append(append([]change.Event{}, base...), profileTask(t, 3, "tsk_01HZX3K9QW7M2P5R8T0V4YB6C1")...)
	twice = append(twice, profileTask(t, 5, "tsk_01HZX3K9QW7M2P5R8T0V4YB6C2")...)

	v := fold(t, twice...)
	require.True(t, change.RowCapped(v, "profile"), "two failed profile tasks on the round cap the row")
	require.False(t, change.RowCapped(v, "review"), "no review task failed")
	require.False(t, change.RowCapped(v, "write_tests"), "no tests task failed")

	one := fold(t, append(append([]change.Event{}, base...), profileTask(t, 3, "tsk_01HZX3K9QW7M2P5R8T0V4YB6C1")...)...)
	require.False(t, change.RowCapped(one, "profile"), "one failure does not cap")

	resumed := append(append([]change.Event{}, twice...), ev(t, 7, change.EventStepResumed, change.StepResumed{Step: "profile", By: "ann"}))
	require.False(t, change.RowCapped(fold(t, resumed...), "profile"), "a resume starts a new round and clears the cap")
}
