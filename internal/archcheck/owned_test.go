package archcheck_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// The ownership guard is scripts/check-owned.sh (BUILD_PLAN M0 step 2b). These tests run it on fixed path lists and
// on a real git branch. They live here because the guard belongs to M0 and so does this package.

// pinRepo points a child's git at repo whatever the parent exports. A git hook (pre-commit runs make fast)
// exports GIT_DIR and GIT_INDEX_FILE, and a child git that inherits them acts on the real repository: a first run
// wiped a worktree's index and set core.bare on the shared config. Later entries win in exec.Cmd.Env.
func pinRepo(cmd *exec.Cmd, repo string) {
	gitDir := filepath.Join(repo, ".git")
	cmd.Env = append(cmd.Environ(), "GIT_DIR="+gitDir, "GIT_WORK_TREE="+repo, "GIT_INDEX_FILE="+filepath.Join(gitDir, "index"))
}

func runOwned(t *testing.T, dir string, args ...string) (string, int) {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "check-owned.sh"))
	require.NoError(t, err)
	cmd := exec.CommandContext(context.Background(), "bash", append([]string{script}, args...)...) // #nosec G204 -- fixed script, arguments from the test
	cmd.Dir = dir
	pinRepo(cmd, dir)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err = cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return out.String(), exit.ExitCode()
	}
	require.NoError(t, err)
	return out.String(), 0
}

func TestCheckOwnedPassesADiffInsideTheModule(t *testing.T) {
	out, code := runOwned(t, ".", "M3", "--paths", filepath.Join("testdata", "owned", "m3_inside.txt"))
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "9 changed paths, all inside M3")
}

func TestCheckOwnedFailsADiffOutsideTheModuleAndNamesEachPath(t *testing.T) {
	out, code := runOwned(t, ".", "M3", "--paths", filepath.Join("testdata", "owned", "m3_outside.txt"))
	require.Equal(t, 1, code, out)
	require.Contains(t, out, "outside M3: internal/transport/mcp/tools_tests.go")
	require.Contains(t, out, "outside M3: internal/app/reviewer/new.go", "a folder that only shares a prefix is not owned")
	require.NotContains(t, out, "outside M3: internal/app/review/doc.go")
	require.Contains(t, out, "2 of 4 changed paths are outside M3")
}

func TestCheckOwnedHonoursADenyInsideAnOwnedFolder(t *testing.T) {
	list := filepath.Join(t.TempDir(), "paths.txt")
	require.NoError(t, os.WriteFile(list, []byte("internal/app/engine/engine.go\ninternal/app/engine/steps_review.go\n"), 0o600))
	out, code := runOwned(t, ".", "M0", "--paths", list)
	require.Equal(t, 1, code, out)
	require.Contains(t, out, "outside M0: internal/app/engine/steps_review.go")
	require.NotContains(t, out, "engine.go\n")
}

func TestCheckOwnedRefusesAnUnknownModule(t *testing.T) {
	_, code := runOwned(t, ".", "M9", "--paths", filepath.Join("testdata", "owned", "m3_inside.txt"))
	require.Equal(t, 2, code)
}

// TestCheckOwnedReadsTheBranchDiffFromTheMergeBase builds a repository whose base branch moved on after the module
// branched: only the module's own commits count, and a rename out of another module's folder counts as a change there.
func TestCheckOwnedReadsTheBranchDiffFromTheMergeBase(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), "git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false"}, args...)...) // #nosec G204 -- fixed binary, arguments from the test
		cmd.Dir = repo
		pinRepo(cmd, repo)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	write := func(path string) {
		t.Helper()
		full := filepath.Join(repo, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o750))
		require.NoError(t, os.WriteFile(full, []byte(path), 0o600))
	}
	git("init", "-q", "-b", "base")
	write("internal/app/review/old.go")
	git("add", "-A")
	git("commit", "-q", "-m", "base")
	git("checkout", "-q", "-b", "m3")
	write("internal/domain/sarif/parse.go")
	git("add", "-A")
	git("commit", "-q", "-m", "inside")
	git("checkout", "-q", "base")
	write("internal/app/announce/later.go")
	git("add", "-A")
	git("commit", "-q", "-m", "base moves on")
	git("checkout", "-q", "m3")

	out, code := runOwned(t, repo, "M3", "base")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "1 changed paths, all inside M3")

	require.NoError(t, os.MkdirAll(filepath.Join(repo, "internal", "app", "announce"), 0o750))
	git("mv", "internal/app/review/old.go", "internal/app/announce/old.go")
	git("commit", "-q", "-m", "move out")
	out, code = runOwned(t, repo, "M3", "base")
	require.Equal(t, 1, code, out)
	require.Contains(t, out, "outside M3: internal/app/announce/old.go")
}
