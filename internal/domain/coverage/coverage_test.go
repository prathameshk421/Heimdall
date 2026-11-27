package coverage

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiffRefusesBadChangedLines(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		changed map[string][]int
	}{
		{"line zero", map[string][]int{"a.go": {0}}},
		{"negative line", map[string][]int{"a.go": {-1}}},
		{"line above the cap", map[string][]int{"a.go": {MaxLine + 1}}},
		{"path escapes the root", map[string][]int{"../a.go": {1}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Diff(Result{}, c.changed)
			require.Error(t, err)
		})
	}
}

func TestBuilderRefusesBadValues(t *testing.T) {
	t.Parallel()
	b, err := NewBuilder(Options{MaxBytes: 1 << 20})
	require.NoError(t, err)
	cases := []struct {
		name string
		line int
		hits int
	}{
		{"line zero", 0, 1},
		{"line above the cap", MaxLine + 1, 1},
		{"negative hits", 1, -1},
		{"hits above the cap", 1, maxHits + 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.ErrorIs(t, b.Add(1, "a.go", c.line, c.hits), ErrMalformed)
		})
	}
}

func TestBuilderRefusesBadPaths(t *testing.T) {
	t.Parallel()
	b, err := NewBuilder(Options{MaxBytes: 1 << 20})
	require.NoError(t, err)
	for _, raw := range []string{"../a.go", "", "."} {
		require.ErrorIs(t, b.Add(4, raw, 1, 1), ErrMalformed, raw)
		err := b.CheckFile(4, raw)
		require.ErrorIs(t, err, ErrMalformed, raw)
		require.Contains(t, err.Error(), "line 4:")
	}
	require.NoError(t, b.CheckFile(1, "a.go"))
	require.Empty(t, b.Result().Files)
}

func TestBuilderRefusesBadOptions(t *testing.T) {
	t.Parallel()
	_, err := NewBuilder(Options{})
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrMalformed)
	_, err = NewBuilder(Options{MaxBytes: 1, Prefix: "../app"})
	require.ErrorContains(t, err, "prefix")
}

func TestBuilderStopsAtMaxLines(t *testing.T) {
	t.Parallel()
	b, err := NewBuilder(Options{MaxBytes: 1})
	require.NoError(t, err)
	b.lines = MaxLines
	require.ErrorIs(t, b.Add(1, "a.go", 1, 1), ErrTooLarge)
}
