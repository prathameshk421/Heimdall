package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/bench"
)

func caseList() []bench.Case {
	return []bench.Case{
		{ID: "bug-ok", Kind: bench.KindBug, Language: "go", Status: bench.StatusValid},
		{ID: "clean-ok", Kind: bench.KindClean, Language: "go", Status: bench.StatusValid},
		{ID: "bad", Kind: bench.KindBug, Language: "go", Status: bench.StatusInvalid},
		{ID: "python-ok", Kind: bench.KindBug, Language: "python", Status: bench.StatusValid},
		{ID: "ts-ok", Kind: bench.KindBug, Language: "typescript", Toolchain: bench.ToolchainVitest, Status: bench.StatusValid},
		{ID: "rust-ok", Kind: bench.KindBug, Language: "rust", Toolchain: bench.ToolchainCargo, Status: bench.StatusValid},
		{ID: "ts-pending", Kind: bench.KindBug, Language: "typescript", Status: bench.StatusPending},
	}
}

func TestPickCasesTakesOnlyValidCasesWithARunner(t *testing.T) {
	got, err := pickCases(caseList(), nil)
	require.NoError(t, err)
	require.Equal(t, []string{"bug-ok", "clean-ok", "ts-ok", "rust-ok"}, ids(got))
}

func TestPickCasesKeepsTheAskedOrderAndSet(t *testing.T) {
	got, err := pickCases(caseList(), []string{"clean-ok", "bug-ok"})
	require.NoError(t, err)
	require.Equal(t, []string{"bug-ok", "clean-ok"}, ids(got), "the run keeps the manifest order, not the flag order")
}

func TestPickCasesRefusesAnIDThatIsNotAValidCaseWithARunner(t *testing.T) {
	for _, id := range []string{"bad", "python-ok", "missing", "ts-pending"} {
		_, err := pickCases(caseList(), []string{id})
		require.ErrorContains(t, err, id, "a named case that cannot be scored is an error, not a silent skip")
	}
}

func TestPickCasesRefusesANamedInvalidCaseEvenWithAGoodOne(t *testing.T) {
	// The typo must not shrink the run to the case that happens to exist.
	_, err := pickCases(caseList(), []string{"bug-ok", "missing"})
	require.ErrorContains(t, err, "missing")
}

func ids(cs []bench.Case) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.ID
	}
	return out
}
