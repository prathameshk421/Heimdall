package sessions_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/sessions"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

// linkedWorktree builds what `git worktree add` leaves on disk: a .git file in the worktree that points at
// <common>/worktrees/<name>, which holds a commondir file.
func linkedWorktree(t *testing.T, common, wt, name string) {
	t.Helper()
	admin := filepath.Join(common, "worktrees", name)
	write(t, filepath.Join(wt, ".git"), "gitdir: "+admin+"\n")
	write(t, filepath.Join(admin, "commondir"), "../..\n")
	write(t, filepath.Join(admin, "gitdir"), filepath.Join(wt, ".git")+"\n")
}

func TestMatcher(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o750))
	sibling := filepath.Join(base, "elsewhere", "wt-feature")
	linkedWorktree(t, filepath.Join(root, ".git"), sibling, "wt-feature")
	other := filepath.Join(base, "other")
	require.NoError(t, os.MkdirAll(filepath.Join(other, ".git"), 0o750))
	otherWT := filepath.Join(base, "other-wt")
	linkedWorktree(t, filepath.Join(other, ".git"), otherWT, "other-wt")
	// A relative gitdir pointer is legal in .git files.
	relWT := filepath.Join(base, "rel-wt")
	write(t, filepath.Join(relWT, ".git"), "gitdir: ../repo/.git/worktrees/rel-wt\n")
	write(t, filepath.Join(root, ".git", "worktrees", "rel-wt", "commondir"), "../..\n")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg", "deep"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(sibling, "sub", "dir"), 0o750))
	plain := filepath.Join(base, "plain")
	require.NoError(t, os.MkdirAll(plain, 0o750))

	m := sessions.NewMatcher(root)
	tests := []struct {
		name string
		dir  string
		want bool
	}{
		{"root itself", root, true},
		{"subfolder of root", filepath.Join(root, "pkg", "deep"), true},
		{"linked worktree outside root", sibling, true},
		{"subfolder of linked worktree", filepath.Join(sibling, "sub", "dir"), true},
		{"relative gitdir pointer", relWT, true},
		{"other repo", other, false},
		{"worktree of other repo", otherWT, false},
		{"folder that is no repo", plain, false},
		{"empty", "", false},
		{"deleted worktree outside root", filepath.Join(base, "gone"), false},
		{"deleted folder under root", filepath.Join(root, "removed-worktree"), true},
		{"sibling with the same name prefix", root + "-copy", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, m.Match(tc.dir))
		})
	}
}

func TestMatcherRootIsLinkedWorktree(t *testing.T) {
	base := t.TempDir()
	main := filepath.Join(base, "main")
	require.NoError(t, os.MkdirAll(filepath.Join(main, ".git"), 0o750))
	wtA := filepath.Join(base, "wt-a")
	wtB := filepath.Join(base, "wt-b")
	linkedWorktree(t, filepath.Join(main, ".git"), wtA, "wt-a")
	linkedWorktree(t, filepath.Join(main, ".git"), wtB, "wt-b")

	m := sessions.NewMatcher(wtA)
	require.True(t, m.Match(main), "the main checkout shares the common git dir")
	require.True(t, m.Match(wtB), "a sibling worktree shares the common git dir")
}

func TestMatcherIgnoresBrokenGitFile(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o750))
	bad := filepath.Join(base, "bad")
	write(t, filepath.Join(bad, ".git"), "this is not a pointer")
	missing := filepath.Join(base, "missing")
	write(t, filepath.Join(missing, ".git"), "gitdir: "+filepath.Join(base, "nowhere")+"\n")

	m := sessions.NewMatcher(root)
	require.False(t, m.Match(bad))
	require.False(t, m.Match(missing))
}

func TestRepoRelative(t *testing.T) {
	root := "/work/repo"
	require.Equal(t, "internal/a.go", sessions.RepoRelative("/work/repo/internal/a.go", "/work/repo", root))
	require.Equal(t, "a.go", sessions.RepoRelative("a.go", "/work/repo", root))
	require.Equal(t, "a.go", sessions.RepoRelative("/work/wt/a.go", "/work/wt", root), "a worktree path is relative to its own cwd")
	require.Empty(t, sessions.RepoRelative("/etc/passwd", "/work/repo", root))
	require.Empty(t, sessions.RepoRelative("", "/work/repo", root))
}

func TestCapText(t *testing.T) {
	long := ""
	for range sessions.MaxTurnChars + 10 {
		long += "é"
	}
	got := sessions.CapText(long)
	require.LessOrEqual(t, len(got), sessions.MaxTurnChars)
	require.Equal(t, got, string([]rune(got)), "the cap never splits a character")
	require.Equal(t, "short", sessions.CapText("short"))
}
