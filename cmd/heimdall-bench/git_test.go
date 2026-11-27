package main

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/bench"
)

func TestRepoSetOpensEachCacheOnceForConcurrentCases(t *testing.T) {
	// Two cases of one repository start at once in a parallel run. They must share one cache, opened once: a second
	// git init and remote add on the same bare repository fails, and two caches would race on its writes.
	root := t.TempDir()
	set := newRepoSet()
	a := bench.Case{ID: "a", Repo: "owner/name"}
	b := bench.Case{ID: "b", Repo: "owner/name"}
	other := bench.Case{ID: "c", Repo: "owner/other"}
	var wg sync.WaitGroup
	got := make([]*repo, 8)
	errs := make([]error, 8)
	for i := range got {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := a
			if i%2 == 1 {
				c = b
			}
			got[i], errs[i] = set.open(context.Background(), execRunner{}, root, c)
		}()
	}
	wg.Wait()
	for i := range got {
		require.NoError(t, errs[i])
		require.Same(t, got[0], got[i], "every case of one repository gets the same cache")
	}
	rc, err := set.open(context.Background(), execRunner{}, root, other)
	require.NoError(t, err)
	require.NotSame(t, got[0], rc, "another repository has its own cache")
}
