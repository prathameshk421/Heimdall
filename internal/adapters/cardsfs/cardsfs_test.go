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

func TestWriteCreatesReplacesAndDeletesACardInsideThePersonsFolder(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	require.NoError(t, s.Write("bob", "rules.md", []byte("v1")))
	require.NoError(t, s.Write("bob", "rules.md", []byte("v2")))
	got, err := s.Read("bob", "rules.md")
	require.NoError(t, err)
	require.Equal(t, "v2", got)
	require.NoError(t, s.Delete("bob", "rules.md"))
	_, err = s.Read("bob", "rules.md")
	require.ErrorIs(t, err, ErrNotFound)
	require.ErrorIs(t, s.Delete("bob", "rules.md"), ErrNotFound)
}

func TestWriteRefusesUnsafeNamesPeopleAndSizes(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	for _, name := range []string{"../x.md", "a/b.md", "x.sh", "", ".md.exe"} {
		require.Error(t, s.Write("bob", name, []byte("x")), name)
	}
	for _, person := range []string{"..", "a/b", "", "."} {
		require.Error(t, s.Write(person, "x.md", []byte("x")), person)
	}
	require.Error(t, s.Write("bob", "big.md", make([]byte, MaxCardBytes+1)))
	_, statErr := os.Stat(filepath.Join(dir, "x.md"))
	require.Error(t, statErr, "nothing may be written outside the person's folder")
}

func TestWriteDoesNotFollowASymlinkOutOfTheFolder(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "target.md")
	require.NoError(t, os.WriteFile(outside, []byte("ORIGINAL"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "bob"), 0o700))
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, "bob", "link.md")))
	s, err := Open(dir)
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	_ = s.Write("bob", "link.md", []byte("OVERWRITTEN"))
	got, err := os.ReadFile(outside)
	require.NoError(t, err)
	require.Equal(t, "ORIGINAL", string(got))
}
