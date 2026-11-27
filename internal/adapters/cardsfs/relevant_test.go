package cardsfs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/rulecard"
)

func writeCard(t *testing.T, dir, name, content string) {
	const person = "bob"
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, person), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, person, name), []byte(content), 0o600))
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

func TestParseReadsRuleFieldsAndRefusesABadStatus(t *testing.T) {
	card, err := Parse("r.md", "---\nsymbols: [\"RefundService.Create\"]\ntriggers: [\"refund(\"]\ncheck: \"Does the cap count past refunds?\"\nproof: \"Two refunds of 60.\"\nstatus: proposed\n---\nbody")
	require.NoError(t, err)
	require.Equal(t, []string{"RefundService.Create"}, card.Symbols)
	require.Equal(t, []string{"refund("}, card.Triggers)
	require.Equal(t, "Does the cap count past refunds?", card.Check)
	require.Equal(t, "Two refunds of 60.", card.Proof)
	require.Equal(t, rulecard.StatusProposed, card.Status)

	plain, err := Parse("p.md", "---\nkind: invariant\n---\nbody")
	require.NoError(t, err)
	require.Equal(t, rulecard.StatusActive, plain.Status)

	_, err = Parse("b.md", "---\nstatus: maybe\n---\nbody")
	require.Error(t, err)
}
