package coverage

import (
	"errors"
	"path"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParsersRefuseInputOverTheLimit(t *testing.T) {
	t.Parallel()
	parsers := []struct {
		name  string
		parse func([]byte, Options) (Result, error)
		in    []byte
	}{
		{"lcov", ParseLCOV, []byte("SF:a.go\nDA:1,1\nend_of_record\n")},
		{"go cover", ParseGoCover, []byte("mode: set\nexample.com/app/a.go:1.1,2.2 1 1\n")},
	}
	for _, c := range parsers {
		t.Run(c.name, func(t *testing.T) {
			atLimit, err := c.parse(c.in, Options{MaxBytes: len(c.in)})
			require.NoError(t, err)
			require.NotEmpty(t, atLimit.Files)

			_, err = c.parse(c.in, Options{MaxBytes: len(c.in) - 1})
			require.ErrorIs(t, err, ErrTooLarge)
		})
	}
}

func TestParsersRefuseANonPositiveLimit(t *testing.T) {
	t.Parallel()
	parsers := []struct {
		name  string
		parse func([]byte, Options) (Result, error)
	}{
		{"lcov", ParseLCOV},
		{"go cover", ParseGoCover},
	}
	for _, c := range parsers {
		t.Run(c.name, func(t *testing.T) {
			_, err := c.parse(nil, Options{})
			require.Error(t, err)
			require.NotErrorIs(t, err, ErrMalformed)
		})
	}
}

func TestParsersRefuseABadPrefix(t *testing.T) {
	t.Parallel()
	parsers := []struct {
		name  string
		parse func([]byte, Options) (Result, error)
	}{
		{"lcov", ParseLCOV},
		{"go cover", ParseGoCover},
	}
	for _, c := range parsers {
		t.Run(c.name, func(t *testing.T) {
			_, err := c.parse(nil, Options{MaxBytes: 1 << 20, Prefix: "../app"})
			require.Error(t, err)
			require.Contains(t, err.Error(), "prefix")
		})
	}
}

func FuzzParseLCOV(f *testing.F) {
	f.Add([]byte("TN:\nSF:a.go\nDA:1,1\nend_of_record\n"))
	f.Add([]byte(""))
	f.Add([]byte("SF:a.go\nDA:0,-1\nend_of_record\n"))
	f.Add([]byte("SF:C:\\work\\app\\a.go\nDA:1,1\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		res, err := ParseLCOV(data, Options{MaxBytes: 1 << 20})
		checkFuzzed(t, res, err)
	})
}

func FuzzParseGoCover(f *testing.F) {
	f.Add([]byte("mode: set\nexample.com/app/a.go:1.1,2.2 1 1\n"))
	f.Add([]byte(""))
	f.Add([]byte("mode: count\nexample.com/app/a.go:5.1,5.9 0 0\n"))
	f.Add([]byte("mode: set\na.go:0.1,1.1 1 2\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		res, err := ParseGoCover(data, Options{MaxBytes: 1 << 20})
		checkFuzzed(t, res, err)
	})
}

// checkFuzzed accepts only the two error kinds a parser returns for untrusted bytes, and checks every
// value it does return against the package invariants.
func checkFuzzed(t *testing.T, res Result, err error) {
	t.Helper()
	if err != nil {
		require.True(t, errors.Is(err, ErrMalformed) || errors.Is(err, ErrTooLarge), "unexpected error: %v", err)
		return
	}
	require.NotNil(t, res.Files)
	for file, lines := range res.Files {
		require.NotEmpty(t, file)
		require.NotContains(t, file, "\\")
		require.False(t, strings.HasPrefix(file, "/"))
		require.Equal(t, path.Clean(file), file)
		require.False(t, file == ".." || strings.HasPrefix(file, "../"))
		require.NotEmpty(t, lines)
		for number, hits := range lines {
			require.GreaterOrEqual(t, number, 1)
			require.LessOrEqual(t, number, MaxLine)
			require.GreaterOrEqual(t, hits, 0)
		}
	}
}
