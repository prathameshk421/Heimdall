package main

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/bench"
)

func TestRunPoolKeepsTheManifestOrder(t *testing.T) {
	// A parallel run must write the results file in the manifest order, not in the order the cases happen to end. Each
	// case waits for the next one to end before it returns, so the cases end in the reverse of their slot order.
	const count = 5
	gates := make([]chan struct{}, count)
	for i := range gates {
		gates[i] = make(chan struct{})
	}
	out, capped := runPool(count, count, func(i int) (reviewCase, bool) {
		if i+1 < count {
			<-gates[i+1]
		}
		close(gates[i])
		return reviewCase{CaseScore: bench.CaseScore{ID: fmt.Sprintf("case-%d", i)}}, false
	})
	require.False(t, capped)
	require.Len(t, out, count)
	for i, rc := range out {
		require.Equal(t, fmt.Sprintf("case-%d", i), rc.ID, "slot %d keeps its case", i)
	}
}

func TestRunPoolRunsTheCasesAtTheSameTime(t *testing.T) {
	// The pool must use its workers: four cases with four workers each wait on the other three, so the pool only ends
	// when all four are in flight at once. One worker per case would deadlock.
	const count = 4
	var mu sync.Mutex
	var arrived []chan struct{}
	started := make([]chan struct{}, count)
	for i := range started {
		started[i] = make(chan struct{})
	}
	out, capped := runPool(count, count, func(i int) (reviewCase, bool) {
		close(started[i])
		for j := range count {
			<-started[j]
		}
		mu.Lock()
		arrived = append(arrived, started[i])
		mu.Unlock()
		return reviewCase{CaseScore: bench.CaseScore{ID: fmt.Sprintf("case-%d", i)}}, false
	})
	require.False(t, capped)
	require.Len(t, out, count)
	require.Len(t, arrived, count)
}

func TestRunPoolStartsNoCaseAfterTheCostCap(t *testing.T) {
	// A worker that reports the cost cap ends the run: no case after it starts, and the pool says it stopped.
	var mu sync.Mutex
	started := 0
	out, capped := runPool(1, 4, func(i int) (reviewCase, bool) {
		mu.Lock()
		started++
		mu.Unlock()
		return reviewCase{CaseScore: bench.CaseScore{ID: fmt.Sprintf("case-%d", i)}}, i == 1
	})
	require.True(t, capped)
	require.Equal(t, 2, started, "the case after the capped one never starts")
	require.Equal(t, "case-0", out[0].ID)
	require.Equal(t, "case-1", out[1].ID)
	require.Empty(t, out[2].ID, "a case that never ran has no result of its own")
}

func TestRunPoolWithMoreWorkersThanCases(t *testing.T) {
	// A worker count larger than the case count must not start a worker with no work, and each case still runs once.
	var mu sync.Mutex
	seen := map[int]int{}
	out, _ := runPool(maxParallel, 3, func(i int) (reviewCase, bool) {
		mu.Lock()
		seen[i]++
		mu.Unlock()
		return reviewCase{CaseScore: bench.CaseScore{ID: fmt.Sprintf("case-%d", i)}}, false
	})
	require.Len(t, out, 3)
	require.Equal(t, map[int]int{0: 1, 1: 1, 2: 1}, seen, "each case runs exactly once")
}

func TestRunPoolWithNoCase(t *testing.T) {
	require.NotPanics(t, func() {
		out, capped := runPool(4, 0, func(int) (reviewCase, bool) {
			t.Fatal("no worker may run")
			return reviewCase{}, false
		})
		require.Empty(t, out)
		require.False(t, capped)
	})
}
