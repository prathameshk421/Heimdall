package coverage

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseGoCoverGolden(t *testing.T) {
	cases := []struct {
		name string
		file string
		opt  Options
		want map[string]Lines
	}{
		{
			name: "count mode",
			file: "testdata/gocover/simple.out",
			opt:  Options{MaxBytes: 1 << 20, Prefix: "example.com/app"},
			want: map[string]Lines{
				"internal/domain/coverage/coverage.go": {41: 12, 42: 12, 43: 12, 45: 0, 46: 0, 47: 0},
				"internal/domain/coverage/lcov.go":     {5: 7, 6: 7, 7: 7},
			},
		},
		{
			name: "set mode",
			file: "testdata/gocover/set.out",
			opt:  Options{MaxBytes: 1 << 20, Prefix: "example.com/app"},
			want: map[string]Lines{
				"a.go": {1: 1, 2: 1},
				"b.go": {3: 0},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseGoCover(load(t, c.file), c.opt)
			require.NoError(t, err)
			require.Equal(t, c.want, got.Files)
		})
	}
}

func TestParseGoCoverRefusesBadReports(t *testing.T) {
	cases := []struct {
		name string
		in   string
		line int
	}{
		{"empty input", "", 1},
		{"no mode line", "example.com/app/a.go:1.1,2.2 1 1\n", 1},
		{"unknown mode", "mode: profile\n", 1},
		{"record without a colon", "mode: set\nexample.com/app/a.go 1 1\n", 2},
		{"record without fields", "mode: set\nexample.com/app/a.go:1.1,2.2\n", 2},
		{"record with four fields", "mode: set\nexample.com/app/a.go:1.1,2.2 1 1 9\n", 2},
		{"span without a comma", "mode: set\nexample.com/app/a.go:1.1 1 1\n", 2},
		{"span without a column", "mode: set\nexample.com/app/a.go:1,2 1 1\n", 2},
		{"span ends before it starts", "mode: set\nexample.com/app/a.go:2.1,1.5 1 1\n", 2},
		{"line zero", "mode: set\nexample.com/app/a.go:0.1,2.2 1 1\n", 2},
		{"count in set mode above one", "mode: set\nexample.com/app/a.go:1.1,2.2 1 2\n", 2},
		{"negative hits", "mode: count\nexample.com/app/a.go:1.1,2.2 1 -1\n", 2},
		{"negative statements", "mode: count\nexample.com/app/a.go:1.1,2.2 -1 1\n", 2},
		{"line above the cap", "mode: count\nexample.com/app/a.go:1.1,10000001.1 1 1\n", 2},
		{"path escapes the root", "mode: set\n../a.go:1.1,2.2 1 1\n", 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseGoCover([]byte(c.in), Options{MaxBytes: 1 << 20})
			require.ErrorIs(t, err, ErrMalformed)
			require.Contains(t, err.Error(), "line "+strconv.Itoa(c.line)+":")
		})
	}
}

func TestParseGoCoverAcceptsAModeOnlyProfile(t *testing.T) {
	got, err := ParseGoCover([]byte("mode: atomic\n"), Options{MaxBytes: 1 << 20})
	require.NoError(t, err)
	require.Empty(t, got.Files)
}

func TestParseGoCoverAcceptsCRLF(t *testing.T) {
	in := "mode: set\r\nexample.com/app/a.go:1.1,2.2 1 1\r\n"
	got, err := ParseGoCover([]byte(in), Options{MaxBytes: 1 << 20, Prefix: "example.com/app"})
	require.NoError(t, err)
	require.Equal(t, map[string]Lines{"a.go": {1: 1, 2: 1}}, got.Files)
}

func TestParseGoCoverHandlesWindowsPaths(t *testing.T) {
	in := "mode: set\nC:\\work\\app\\a.go:1.1,2.2 1 1\n"
	got, err := ParseGoCover([]byte(in), Options{MaxBytes: 1 << 20, Prefix: "C:\\work\\app"})
	require.NoError(t, err)
	require.Equal(t, map[string]Lines{"a.go": {1: 1, 2: 1}}, got.Files)
}
