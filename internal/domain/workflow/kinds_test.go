package workflow

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEveryRoleHoldsOneKindAndKindNamesAreUnique(t *testing.T) {
	names := map[string]bool{}
	for _, k := range Kinds() {
		require.False(t, names[k.Name], "kind %s twice", k.Name)
		names[k.Name] = true
		require.NotEmpty(t, k.Output, k.Name)
		require.NotEmpty(t, k.Work, k.Name)
		require.NotEmpty(t, k.Label, k.Name)
	}
	for _, r := range []Role{RoleTestAuthor, RoleReviewer, RoleResolver, RoleProfiler} {
		k, ok := KindFor(r)
		require.True(t, ok, r)
		require.Equal(t, r, k.Holder)
	}
	_, ok := KindFor("nobody")
	require.False(t, ok)
}

func TestKindToolsAreSharedWithNoOtherKindOnlyWhereTheTableSaysSo(t *testing.T) {
	for _, k := range Kinds() {
		require.NotEmpty(t, k.Inputs, k.Name)
		require.NotEmpty(t, k.Tools, k.Name)
		require.NotEmpty(t, k.Skill, k.Name)
		require.Positive(t, k.Timeout, k.Name)
		for _, shared := range SharedTools() {
			require.NotContains(t, k.Tools, shared, "shared tools are not repeated per kind")
		}
	}
}
