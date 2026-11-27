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

func TestParseLCOVRefusesBadReports(t *testing.T) {
	t.Parallel()
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
