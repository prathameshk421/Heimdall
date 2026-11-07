package cardsfs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func seed(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "bob"), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "eli"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bob", "invariants.md"), []byte("sum partial refunds"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bob", "ownership.yaml"), []byte("owns: payments"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bob", "notes.bin"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "eli", "invariants.md"), []byte("ELI SECRET"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "outside.md"), []byte("OUTSIDE"), 0o600))
	s, err := Open(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestListShowsOnlyThePersonsCardFiles(t *testing.T) {
	names, err := seed(t).List("bob")
	require.NoError(t, err)
	require.Equal(t, []string{"invariants.md", "ownership.yaml"}, names)
}

func TestReadReturnsTheCard(t *testing.T) {
	got, err := seed(t).Read("bob", "invariants.md")
	require.NoError(t, err)
	require.Equal(t, "sum partial refunds", got)
}

func TestReadRefusesAnythingOutsideThePersonsFolder(t *testing.T) {
	s := seed(t)
	for _, name := range []string{"../eli/invariants.md", "../outside.md", "/etc/passwd", "a/../../outside.md", "..", ".", "", "notes.bin", "sub/dir.md"} {
		_, err := s.Read("bob", name)
		require.Error(t, err, name)
	}
	for _, person := range []string{"../eli", "..", "", "bob/../eli", "/", "."} {
		_, err := s.Read(person, "invariants.md")
		require.Error(t, err, person)
		_, err = s.List(person)
		require.Error(t, err, person)
	}
}

func TestReadRefusesASymlinkOutTheFolder(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "bob"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "secret.md"), []byte("OUTSIDE"), 0o600))
	require.NoError(t, os.Symlink("../secret.md", filepath.Join(dir, "bob", "leak.md")))
	s, err := Open(dir)
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	_, err = s.Read("bob", "leak.md")
	require.Error(t, err)
}

func TestAPersonWithNoCardsListsNothing(t *testing.T) {
	names, err := seed(t).List("nobody")
	require.NoError(t, err)
	require.Empty(t, names)
}

func TestAnOversizedCardIsRefused(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "bob"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bob", "big.md"), make([]byte, MaxCardBytes+1), 0o600))
	s, err := Open(dir)
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	_, err = s.Read("bob", "big.md")
	require.Error(t, err)
}
