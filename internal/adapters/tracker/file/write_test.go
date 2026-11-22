package file

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFileTrackerCommentAndTransition(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "KAN-5.md")
	require.NoError(t, os.WriteFile(path, []byte("# KAN-5: Demo\nAssignee: ann\nStatus: In Progress\n\n## Acceptance criteria\n- AC1\n"), 0o600))

	tr, err := New(dir, &fixedClock{at: now})
	require.NoError(t, err)

	require.NoError(t, tr.Comment(context.Background(), "KAN-5", "KAN-5/tracker_commit/abc/r0", "Tests committed."))
	require.NoError(t, tr.Comment(context.Background(), "KAN-5", "KAN-5/tracker_commit/abc/r0", "Tests committed."))
	require.NoError(t, tr.Transition(context.Background(), "KAN-5", "KAN-5/tracker_merge/abc/r0", "Done"))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	text := string(raw)
	require.Contains(t, text, "Status: Done")
	require.Equal(t, 1, strings.Count(text, "Tests committed."), "comment marker makes repeat a no-op")
}
