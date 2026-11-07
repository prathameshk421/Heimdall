package rootlessrunner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// runRootScript returns the module root the generated shell script selects for a prepared tree.
func runRootScript(t *testing.T, root string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "sh", "-c", sourceRootScript(root)+`; printf %s "$root"`)
	out, err := cmd.Output()
	require.NoError(t, err)
	return string(out)
}

func TestSourceRootScriptEntersTheGitHubTarballDirectory(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "owner-repo-abc1234")
	require.NoError(t, os.Mkdir(repo, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module fixture\n"), 0o600))

	require.Equal(t, repo, runRootScript(t, dir))
}

func TestSourceRootScriptKeepsAFlatArchiveRoot(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fixture\n"), 0o600))

	require.Equal(t, dir, runRootScript(t, dir))
}

func TestSourceRootScriptStopsAtSeveralTopLevelDirectories(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "one"), 0o750))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "two"), 0o750))

	require.Equal(t, dir, runRootScript(t, dir))
}

func TestSourceRootScriptKeepsAnEmptyArchiveRoot(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, dir, runRootScript(t, dir))
}
