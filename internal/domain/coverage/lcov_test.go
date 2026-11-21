package coverage

import (
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func load(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(name)
	require.NoError(t, err)
	return data
}

func TestParseLCOVGolden(t *testing.T) {
	cases := []struct {
		name string
		file string
		opt  Options
		want map[string]Lines
	}{
		{
			name: "simple",
			file: "testdata/lcov/simple.info",
			opt:  Options{MaxBytes: 1 << 20},
			want: map[string]Lines{
				"internal/domain/coverage/coverage.go": {41: 12, 42: 12, 43: 11, 44: 0},
				"internal/domain/coverage/lcov.go":     {1: 1, 2: 0},
			},
		},
		{
			name: "two records for one file are merged",
			file: "testdata/lcov/merged.info",
			opt:  Options{MaxBytes: 1 << 20},
			want: map[string]Lines{"src/app.js": {1: 3, 2: 0, 3: 1}},
		},
		{
			name: "the prefix is removed from an absolute path",
			file: "testdata/lcov/absolute.info",
			opt:  Options{MaxBytes: 1 << 20, Prefix: "/home/runner/work/app"},
			want: map[string]Lines{"src/report.js": {1: 2, 2: 0}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseLCOV(load(t, c.file), c.opt)
			require.NoError(t, err)
			require.Equal(t, c.want, got.Files)
		})
	}
}

func TestParseLCOVRefusesBadReports(t *testing.T) {
	cases := []struct {
		name string
		in   string
		line int
	}{
		{"DA before SF", "DA:1,1\n", 1},
		{"end_of_record with no open record", "end_of_record\n", 1},
		{"unknown record", "SF:a.go\nXZ:1\nend_of_record\n", 2},
		{"record without a value", "SF:a.go\nDA\nend_of_record\n", 2},
		{"DA with one field", "SF:a.go\nDA:1\nend_of_record\n", 2},
		{"DA with four fields", "SF:a.go\nDA:1,1,x,y\nend_of_record\n", 2},
		{"line is not a number", "SF:a.go\nDA:x,1\nend_of_record\n", 2},
		{"hits are not a number", "SF:a.go\nDA:1,x\nend_of_record\n", 2},
		{"line zero", "SF:a.go\nDA:0,1\nend_of_record\n", 2},
		{"negative hits", "SF:a.go\nDA:1,-1\nend_of_record\n", 2},
		{"hits above the cap", "SF:a.go\nDA:1,1099511627777\nend_of_record\n", 2},
		{"line above the cap", "SF:a.go\nDA:10000001,1\nend_of_record\n", 2},
		{"record not closed", "SF:a.go\nDA:1,1\n", 1},
		{"SF inside an open record", "SF:a.go\nSF:b.go\nend_of_record\n", 2},
		{"path escapes the root", "SF:../a.go\nDA:1,1\nend_of_record\n", 1},
		{"empty path", "SF:\nDA:1,1\nend_of_record\n", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseLCOV([]byte(c.in), Options{MaxBytes: 1 << 20})
			require.ErrorIs(t, err, ErrMalformed)
			require.Contains(t, err.Error(), "line "+strconv.Itoa(c.line)+":")
		})
	}
}

func TestParseLCOVAcceptsAnEmptyReport(t *testing.T) {
	got, err := ParseLCOV(nil, Options{MaxBytes: 1 << 20})
	require.NoError(t, err)
	require.Empty(t, got.Files)
}

func TestParseLCOVAcceptsCRLF(t *testing.T) {
	in := "SF:a.go\r\nDA:1,1\r\nDA:2,0\r\nend_of_record\r\n"
	got, err := ParseLCOV([]byte(in), Options{MaxBytes: 1 << 20})
	require.NoError(t, err)
	require.Equal(t, map[string]Lines{"a.go": {1: 1, 2: 0}}, got.Files)
}

func TestParseLCOVAcceptsEveryInformationalRecord(t *testing.T) {
	in := "TN:unit\nSF:a.go\nFN:1,f\nFNDA:2,f\nFNF:1\nFNH:1\nBRDA:1,0,0,2\nBRDA:1,0,1,-\nBRF:2\nBRH:1\nLF:1\nLH:1\nDA:1,2,checksum\nend_of_record\n"
	got, err := ParseLCOV([]byte(in), Options{MaxBytes: 1 << 20})
	require.NoError(t, err)
	require.Equal(t, map[string]Lines{"a.go": {1: 2}}, got.Files)
}
