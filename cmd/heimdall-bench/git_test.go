package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/bench"
)

func TestRemoveStaysInsideTheWorktreeParent(t *testing.T) {
	// The bench deletes a worktree after every run. A path that escapes the repository's own worktree parent must be
	// refused, so a label built from a case id or a SHA can never delete a directory the bench does not own.
	parent := t.TempDir()
	outside := filepath.Join(t.TempDir(), "keep")
	require.NoError(t, os.MkdirAll(outside, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "file"), []byte("x"), 0o600))

	rp := &repo{r: execRunner{}, work: parent, gitDir: filepath.Join(parent, ".git")}
	require.Error(t, rp.remove(context.Background(), outside), "a path outside the parent is refused")
	require.FileExists(t, filepath.Join(outside, "file"), "the outside directory is untouched")
	require.Error(t, rp.remove(context.Background(), parent), "the parent itself is refused")

	inside := filepath.Join(parent, "sbx-1")
	require.NoError(t, os.MkdirAll(inside, 0o750))
	// A bare git dir, so the prune that follows a removal is a real git call.
	rp.gitDir = filepath.Join(parent, "cache.git")
	_, err := rp.git(context.Background(), "", "init", "--quiet", "--bare", rp.gitDir)
	require.NoError(t, err)
	require.NoError(t, rp.remove(context.Background(), inside))
	require.NoDirExists(t, inside)
}

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
