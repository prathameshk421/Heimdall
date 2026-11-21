package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
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
