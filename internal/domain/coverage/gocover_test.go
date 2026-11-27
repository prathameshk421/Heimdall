package coverage

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseGoCoverRefusesBadReports(t *testing.T) {
	t.Parallel()
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

func TestParseGoCoverHandlesWindowsPaths(t *testing.T) {
	t.Parallel()
	in := "mode: set\nC:\\work\\app\\a.go:1.1,2.2 1 1\n"
	got, err := ParseGoCover([]byte(in), Options{MaxBytes: 1 << 20, Prefix: "C:\\work\\app"})
	require.NoError(t, err)
	require.Equal(t, map[string]Lines{"a.go": {1: 1, 2: 1}}, got.Files)
}
