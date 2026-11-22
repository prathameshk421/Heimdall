package change_test

import (
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/stretchr/testify/require"
)

func TestEveryEffectKindHasABudgetAndSandboxKindsCoverRunnerTime(t *testing.T) {
	t.Parallel()
	for _, k := range change.EffectKinds() {
		budget := k.Budget()
		require.Positive(t, budget, "effect kind %s must declare a positive budget", k)
		if k.IsSandbox() {
			require.GreaterOrEqual(t, budget, 5*time.Minute, "sandbox effect %s budget too short", k)
		}
	}
	require.GreaterOrEqual(t, change.EffectGitHubCommitTests.Budget(), 60*time.Second)
	require.GreaterOrEqual(t, change.EffectMemoryEpisode.Budget(), 60*time.Second)
	require.Zero(t, change.EffectKind("unknown_kind").Budget())
}
