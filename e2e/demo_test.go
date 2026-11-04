package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/e2e"
)

func TestDemoBeatsAreTheNineBeatsOfTheVideoInOrder(t *testing.T) {
	beats := e2e.DemoBeats()
	var at []string
	for _, b := range beats {
		at = append(at, b.At)
		require.NotEmpty(t, b.Shows)
		require.NotEmpty(t, b.Steps)
	}
	require.Equal(t, []string{"1:10", "1:20", "1:35", "1:45", "1:55", "2:00", "2:05", "2:15", "2:25"}, at)
	require.Equal(t, []string{"P1.1", "P1.2"}, beats[0].Steps)
	require.Equal(t, []string{"P2.1", "P2.2"}, beats[3].Steps)
	require.Equal(t, []string{"P4.1", "P4.3"}, beats[6].Steps)
	require.Equal(t, []string{"P5.2"}, beats[8].Steps)
}

func TestDemoCheckAbeatIsGreenOnlyWhenEveryOneOfItsStepsPassed(t *testing.T) {
	results := []e2e.ScenarioResult{
		{ID: "P1", Results: []e2e.Result{
			{Step: "P1.1", Status: e2e.StatusPass},
			{Step: "P1.2", Status: e2e.StatusPass},
			{Step: "P1.3", Status: e2e.StatusRed, Why: "expected one question, saw two"},
		}},
		{ID: "P2", Results: []e2e.Result{
			{Step: "P2.1", Status: e2e.StatusPass},
			{Step: "P2.2", Status: e2e.StatusBlocked, BlockedOn: e2e.NeedServer, Why: "no server"},
		}},
	}
	got := e2e.DemoCheck(results)
	require.Len(t, got, 9)
	require.Equal(t, e2e.StatusPass, got[0].Status, "1:10 needs P1.1 and P1.2")
	require.Equal(t, e2e.StatusRed, got[1].Status, "1:20 needs P1.3, which failed")
	require.Contains(t, got[1].Why, "P1.3")
	require.Equal(t, e2e.StatusBlocked, got[3].Status, "1:45 needs P2.2, which is blocked; blocked is not green")
	require.Equal(t, e2e.StatusRed, got[2].Status, "1:35 needs P1.4, which is not on the board at all, so it cannot be claimed")
	require.Contains(t, got[2].Why, "P1.4")
}

func TestDemoLineSaysHowManyBeatsAreReal(t *testing.T) {
	require.Equal(t, "demo beats 1/2 real", e2e.DemoLine([]e2e.BeatResult{{Status: e2e.StatusPass}, {Status: e2e.StatusBlocked}}))
}
