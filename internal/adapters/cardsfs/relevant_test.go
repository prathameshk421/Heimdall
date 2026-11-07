package cardsfs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeCard(t *testing.T, dir, name, content string) {
	const person = "bob"
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, person), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, person, name), []byte(content), 0o600))
}

func TestParseReadsFrontMatterAndBody(t *testing.T) {
	card, err := Parse("inv.md", "---\napplies_to: [\"internal/payments/**\", \"api/*.yaml\"]\nkind: invariant\nsource: session 2026-10-30\n---\nSum partial refunds.\n")
	require.NoError(t, err)
	require.Equal(t, []string{"internal/payments/**", "api/*.yaml"}, card.AppliesTo)
	require.Equal(t, "invariant", card.Kind)
	require.Equal(t, "session 2026-10-30", card.Source)
	require.Equal(t, "Sum partial refunds.", card.Body)
}

func TestParseTreatsAFileWithoutFrontMatterAsAGeneralCard(t *testing.T) {
	card, err := Parse("conv.md", "Plain text only.\n")
	require.NoError(t, err)
	require.Empty(t, card.AppliesTo)
	require.Equal(t, "Plain text only.", card.Body)
}

func TestParseRefusesBrokenFrontMatterAndUnsafeGlobs(t *testing.T) {
	for _, bad := range []string{
		"---\napplies_to: [unclosed\n---\nbody",
		"---\napplies_to: [\"../../etc/**\"]\n---\nbody",
		"---\napplies_to: [\"/abs/path\"]\n---\nbody",
		"---\nkind: invariant\nbody without closing marker",
	} {
		_, err := Parse("x.md", bad)
		require.Error(t, err, bad)
	}
}

func TestRelevantReturnsCardsThatMatchTheChangedFilesThenGeneralOnes(t *testing.T) {
	dir := t.TempDir()
	writeCard(t, dir, "money.md", "---\napplies_to: [\"internal/payments/**\"]\n---\nMONEY")
	writeCard(t, dir, "api.md", "---\napplies_to: [\"api/openapi.yaml\"]\n---\nAPI")
	writeCard(t, dir, "general.md", "GENERAL")
	s, err := Open(dir)
	require.NoError(t, err)
	defer func() { _ = s.Close() }()

	got, err := s.Relevant("bob", []string{"internal/payments/service.go", "README.md"}, 10_000)
	require.NoError(t, err)
	var names []string
	for _, c := range got {
		names = append(names, c.Name)
	}
	require.Equal(t, []string{"money.md", "general.md"}, names, "a matching card comes first, a general card follows, an unrelated card is left out")
}

func TestRelevantStaysInsideTheByteBudget(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.md", "b.md", "c.md"} {
		writeCard(t, dir, name, "---\napplies_to: [\"**\"]\n---\n"+string(make([]byte, 400)))
	}
	s, err := Open(dir)
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	got, err := s.Relevant("bob", []string{"x.go"}, 900)
	require.NoError(t, err)
	require.Len(t, got, 2)
}

func TestRelevantSkipsABrokenCardInsteadOfFailingTheReview(t *testing.T) {
	dir := t.TempDir()
	writeCard(t, dir, "bad.md", "---\napplies_to: [oops\n---\nbody")
	writeCard(t, dir, "good.md", "GOOD")
	s, err := Open(dir)
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	got, err := s.Relevant("bob", []string{"x.go"}, 10_000)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "good.md", got[0].Name)
}
