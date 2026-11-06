package pack

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDraftSaveLoadKeepsOnlyReviewablePackMaterial(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".heimdall", "pack-draft.json")
	want := Draft{Person: "alice", Version: 2, CreatedAt: time.Date(2026, 11, 6, 1, 0, 0, 0, time.UTC), Canary: "canary-token", Sections: []Section{{Name: "invariants", Content: "safe text"}}, Removed: []Redaction{{Kind: "email", Count: 1}}}
	require.NoError(t, SaveDraft(path, want))
	got, err := LoadDraft(path)
	require.NoError(t, err)
	require.Equal(t, want, got)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestDraftLoadRefusesBroadPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pack-draft.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"person":"alice"}`), 0o644))
	_, err := LoadDraft(path)
	require.ErrorContains(t, err, "permissions")
}
