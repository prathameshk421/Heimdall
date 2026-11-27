package main

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/bench"
)

func TestLoadPriorScoredKeepsOnlyScoredCases(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "review.json")
	prior := reviewReport{
		Cases: []reviewCase{
			{CaseScore: bench.CaseScore{ID: "case-a", Kind: bench.KindBug, Scored: true, CostUSD: 0.12}},
			{CaseScore: bench.CaseScore{ID: "case-b", Kind: bench.KindBug, Scored: false}, Error: "HTTP 500"},
			{CaseScore: bench.CaseScore{ID: "case-c", Kind: bench.KindClean, Scored: true, CostUSD: 0.05}},
		},
	}
	require.NoError(t, writeReportAtomic(out, prior))

	got, err := loadPriorScored(out)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Contains(t, got, "case-a")
	require.Contains(t, got, "case-c")
	require.NotContains(t, got, "case-b")
	require.InDelta(t, 0.12, got["case-a"].CostUSD, 1e-9)
}

func TestLoadPriorScoredMissingFileReturnsNil(t *testing.T) {
	got, err := loadPriorScored(filepath.Join(t.TempDir(), "does-not-exist.json"))
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestRunPoolWithHookCheckpointsEveryCase(t *testing.T) {
	var snapshots int
	out, stopped := runPoolWithHook(2, 3, func(i int) (reviewCase, string) {
		return reviewCase{CaseScore: bench.CaseScore{ID: string(rune('a' + i)), Scored: true}}, ""
	}, func(snap []reviewCase) {
		require.Len(t, snap, 3)
		snapshots++
	})
	require.Empty(t, stopped)
	require.Len(t, out, 3)
	require.Equal(t, 3, snapshots)
}

// The hook writes the run's checkpoint file, so its calls must not overlap and must arrive in completion order:
// each snapshot holds at least as many finished cases as the one before (an overlapping, unordered call could put an
// older snapshot on disk after a newer one). CI's -race run also checks the calls do not overlap.
func TestRunPoolWithHookSerializesSnapshotsInCompletionOrder(t *testing.T) {
	var inHook, maxInHook, last int
	var calls int
	_, stopped := runPoolWithHook(4, 40, func(i int) (reviewCase, string) {
		return reviewCase{CaseScore: bench.CaseScore{ID: string(rune('a' + i%26)), Scored: true}}, ""
	}, func(snap []reviewCase) {
		inHook++
		if inHook > maxInHook {
			maxInHook = inHook
		}
		done := 0
		for _, c := range snap {
			if c.Scored {
				done++
			}
		}
		require.GreaterOrEqual(t, done, last, "a snapshot never has fewer finished cases than the one before")
		last = done
		calls++
		inHook--
	})
	require.Empty(t, stopped)
	require.Equal(t, 40, calls)
	require.Equal(t, 1, maxInHook, "hook calls never overlap")
	require.Equal(t, 40, last)
}
