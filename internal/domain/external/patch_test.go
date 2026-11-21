package external_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/external"
)

func TestParsePatch(t *testing.T) {
	patch := "@@ -3,3 +3,5 @@ func f() {\n a\n b\n+x\n+y\n c\n@@ -19,3 +21,2 @@\n d\n-gone\n e\n\\ No newline at end of file"
	got, err := external.ParsePatch(patch)
	require.NoError(t, err)
	require.Equal(t, []external.Hunk{
		{OldStart: 3, NewStart: 3, Ops: join(keep(2), add(2), keep(1))},
		{OldStart: 19, NewStart: 21, Ops: join(keep(1), del(1), keep(1))},
	}, got)
}

func TestParsePatchRefuses(t *testing.T) {
	cases := map[string]string{
		"header counts do not match the lines": "@@ -1,2 +1,2 @@\n a\n",
		"bad header":                           "@@ -x +y @@\n a\n",
		"a line with no prefix":                "@@ -1,1 +1,1 @@\nzzz\n",
		"too large":                            "@@ -1 +1 @@\n" + strings.Repeat("+a\n", external.MaxPatchBytes),
	}
	for name, p := range cases {
		_, err := external.ParsePatch(p)
		require.ErrorIs(t, err, external.ErrBadPatch, name)
	}
	got, err := external.ParsePatch("")
	require.NoError(t, err)
	require.Empty(t, got)
}
