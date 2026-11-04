package scenarios_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/e2e"
	"github.com/prathameshk421/flip_the_switch_hackathon/e2e/scenarios"
)

var specIDs = []string{
	"P1.1", "P1.2", "P1.3", "P1.4", "P1.5", "P1.6",
	"P2.1", "P2.2", "P2.3", "P2.4",
	"P3.1", "P3.2", "P3.3", "P3.4", "P3.5", "P3.6",
	"P4.1", "P4.2", "P4.3", "P4.4",
	"P5.1", "P5.2", "P5.3", "P5.4", "P5.5", "P5.6", "P5.7",
	"P6.1", "P6.2",
	"P7.1", "P7.2", "P7.3", "P7.4", "P7.5",
	"P8.1", "P8.2",
	"P9.1", "P9.2", "P9.3", "P9.4",
}

func hasTag(st e2e.Step, tag string) bool {
	for _, t := range st.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

func TestAllStepsMatchTheSpec(t *testing.T) {
	var ids []string
	seen := map[string]bool{}
	for _, sc := range scenarios.All() {
		position := map[string]int{}
		for i, st := range sc.Steps {
			ids = append(ids, st.ID)
			require.False(t, seen[st.ID], "%s appears twice", st.ID)
			seen[st.ID] = true
			position[st.ID] = i
			require.NotEmpty(t, st.Does, st.ID)
			require.NotEmpty(t, st.Sees, st.ID)
			require.NotEmpty(t, st.Tags, st.ID)
			require.NotNil(t, st.Run, st.ID)
			require.NotEmpty(t, st.Needs, "%s needs at least one prerequisite", st.ID)
			if st.After != "" {
				at, ok := position[st.After]
				require.True(t, ok && at < i, "%s: After %q is not an earlier step of the same scenario", st.ID, st.After)
			}
			require.Equal(t, hasTag(st, "manual"), hasNeed(st, e2e.NeedManual), "%s: manual tag and NeedManual go together", st.ID)
			if hasTag(st, "live") {
				require.True(t, hasNeed(st, e2e.NeedLive), "%s is live", st.ID)
			}
			if hasTag(st, "scripted") {
				require.True(t, hasNeed(st, e2e.NeedScripted), "%s is scripted", st.ID)
			}
		}
	}
	require.Equal(t, specIDs, ids)
}

func hasNeed(st e2e.Step, n e2e.Need) bool {
	for _, x := range st.Needs {
		if x == n {
			return true
		}
	}
	return false
}

func TestEveryScenarioIsNamedAfterItsSteps(t *testing.T) {
	for _, sc := range scenarios.All() {
		require.NotEmpty(t, sc.Title, sc.ID)
		for _, st := range sc.Steps {
			require.Equal(t, sc.ID+".", st.ID[:len(sc.ID)+1], "%s is in scenario %s", st.ID, sc.ID)
		}
	}
}

func fixture(t *testing.T) *e2e.Story {
	t.Helper()
	st, err := e2e.LoadStory("../stories/fixture-pay.yaml")
	require.NoError(t, err)
	st.Pending = ""
	return st
}

func TestWithNothingBuiltEveryStepIsBlockedAndNoneIsGreen(t *testing.T) {
	st := fixture(t)
	w := e2e.NewLocalWorld(e2e.LocalConfig{Story: st, Agents: "scripted"})
	now := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	b := &e2e.Board{World: w, Story: st, Now: func() time.Time { return now }, Scripted: true}
	steps := 0
	for _, sc := range b.Run(context.Background(), scenarios.All()) {
		for _, r := range sc.Results {
			steps++
			require.Equal(t, e2e.StatusBlocked, r.Status, "%s: %s", r.Step, r.Why)
			require.NotEmpty(t, r.Why, r.Step)
			require.NotEmpty(t, r.BlockedOn, r.Step)
		}
	}
	require.Equal(t, len(specIDs), steps)
}

// With no prerequisite configured every step stops at its first need, so this proves the other half: a step that
// is run directly reaches a driver that no task has built yet, and that is "blocked on a driver", never red.
func TestAStepThatStartsAScenarioMeetsAnUnbuiltDriverNotAFailure(t *testing.T) {
	st := fixture(t)
	w := e2e.NewLocalWorld(e2e.LocalConfig{Story: st, Agents: "scripted"})
	starts := map[string]bool{}
	for _, id := range []string{
		"P1.1", "P1.6", "P2.1", "P2.4", "P3.1", "P3.6", "P4.1", "P4.4", "P5.1", "P5.6", "P6.1", "P6.2",
		"P7.1", "P7.2", "P7.3", "P7.4", "P7.5", "P8.1", "P8.2",
	} {
		starts[id] = true
	}
	for _, sc := range scenarios.All() {
		for _, step := range sc.Steps {
			if !starts[step.ID] {
				continue
			}
			env := &e2e.Env{World: w, Story: st, Capture: &e2e.Capture{}, Vars: map[string]any{}}
			err := step.Run(context.Background(), env)
			task, ok := e2e.IsNotBuilt(err)
			require.True(t, ok, "%s should be blocked on an unbuilt driver, got %v", step.ID, err)
			require.NotEmpty(t, task, step.ID)
			delete(starts, step.ID)
		}
	}
	require.Empty(t, starts)
}
