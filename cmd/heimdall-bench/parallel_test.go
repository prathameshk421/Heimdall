package main

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/bench"
)

func TestRunPoolStartsNoCaseAfterTheCostCap(t *testing.T) {
	// A worker that reports the cost cap ends the run: no case after it starts, and the pool says it stopped.
	var mu sync.Mutex
	started := 0
	out, stopped := runPool(1, 4, func(i int) (reviewCase, string) {
		mu.Lock()
		started++
		mu.Unlock()
		return reviewCase{CaseScore: bench.CaseScore{ID: fmt.Sprintf("case-%d", i)}}, stopIf(i == 1)
	})
	require.Equal(t, stopCostCap, stopped)
	require.Equal(t, 2, started, "the case after the capped one never starts")
	require.Equal(t, "case-0", out[0].ID)
	require.Equal(t, "case-1", out[1].ID)
	require.Empty(t, out[2].ID, "a case that never ran has no result of its own")
}

// stopIf is the stop reason a fake case returns: the cost cap when stop is true.
func stopIf(stop bool) string {
	if stop {
		return stopCostCap
	}
	return ""
}
