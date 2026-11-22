package main

import (
	"os"
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

func TestWriteReportAtomicLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.json")
	require.NoError(t, writeReportAtomic(out, reviewReport{Model: "m"}))
	_, err := os.Stat(out)
	require.NoError(t, err)
	_, err = os.Stat(out + ".tmp")
	require.ErrorIs(t, err, os.ErrNotExist)
}
