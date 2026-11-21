package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/bench"
)

// The shared cap rule of a parallel run: a case that the other workers' holds cut short is not scored and the run goes
// on; the run stops only when the cap cannot cover another case at all.

func sharedCapOptions() reviewOptions {
	return reviewOptions{perCase: 0.50, price: bench.Price{InputPerM: 0.5, OutputPerM: 2}}
}

func TestACaseStarvedByOtherWorkersIsNotScoredAndTheRunGoesOn(t *testing.T) {
	// Another worker's call holds the whole $1 cap, but nothing is spent: the cap covers a case once that call
	// settles. This case gets nothing now; it must say why and must not stop the run. It never opens a repository.
	costCap, err := bench.NewCostCap(1)
	require.NoError(t, err)
	_, err = costCap.Reserve(1)
	require.NoError(t, err)
	cr := &caseRun{o: sharedCapOptions(), r: execRunner{}, costCap: costCap, prog: newProgress(nil), repos: newRepoSet()}
	rc, stop := cr.review(context.Background(), bench.Case{ID: "late", Kind: bench.KindBug})
	require.False(t, stop, "the holds return to the cap, so the run goes on")
	require.False(t, rc.Scored)
	require.Contains(t, rc.Error, "other workers held the rest of the shared cost cap")
}

func TestACaseTheCapCannotCoverStopsTheRun(t *testing.T) {
	costCap, err := bench.NewCostCap(1)
	require.NoError(t, err)
	require.NoError(t, costCap.Add(1))
	cr := &caseRun{o: sharedCapOptions(), r: execRunner{}, costCap: costCap, prog: newProgress(nil), repos: newRepoSet()}
	rc, stop := cr.review(context.Background(), bench.Case{ID: "late", Kind: bench.KindBug})
	require.True(t, stop)
	require.False(t, rc.Scored)
	require.Contains(t, rc.Error, "not run: ")
}

func TestCapCutTellsAHeldCutFromAnEmptyCap(t *testing.T) {
	var held reviewCase
	require.False(t, capCut(&held, true, 0.2, "the agent loop used its budget"), "other workers held part of the cap")
	require.Contains(t, held.Error, "cut short by the shared cost cap: other workers held part of it ($0.2000 was free")
	require.Contains(t, held.Error, "not scored")
	require.False(t, held.Scored)

	var empty reviewCase
	require.True(t, capCut(&empty, false, 0.2, "the agent loop used its budget"), "the cap cannot cover another case")
	require.Contains(t, empty.Error, "stopped by the cost cap ($0.2000 left for this case)")
}

func TestRunCasesGoesOnPastAHeldCutAndStopsAtTheCap(t *testing.T) {
	// Three cases on one worker. The first is cut by holds and the run goes on; the second empties the cap and ends
	// the run; the third never starts and says why.
	out, stopped := runPool(1, 3, func(i int) (reviewCase, string) {
		rc := reviewCase{CaseScore: bench.CaseScore{ID: []string{"a", "b", "c"}[i]}}
		switch i {
		case 0:
			_ = capCut(&rc, true, 0.1, "x")
			return rc, ""
		case 1:
			return rc, stopIf(capCut(&rc, false, 0.1, "x"))
		}
		return rc, ""
	})
	require.Equal(t, stopCostCap, stopped)
	require.Contains(t, out[0].Error, "not scored")
	require.Contains(t, out[1].Error, "stopped by the cost cap")
	require.Empty(t, out[2].ID, "no case starts after the cap ended the run")
}
