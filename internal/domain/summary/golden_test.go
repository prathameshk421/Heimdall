package summary_test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/summary"
)

var update = flag.Bool("update", false, "rewrite the golden files from the current output")

// TestGoldens: each realistic page renders to exactly the committed file. The files are reviewed by eye when they
// change: run go test ./internal/domain/summary -update, then read the diff.
func TestGoldens(t *testing.T) {
	t.Parallel()
	cases := goldenCases()
	require.GreaterOrEqual(t, len(cases), 6)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := summary.Render(c.in)
			path := filepath.Join("testdata", c.name+".golden.md")
			if *update {
				require.NoError(t, os.WriteFile(path, []byte(got), 0o600))
			}
			want, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, string(want), got)
			require.Equal(t, got, summary.Render(c.in), "the same input must give the same page")
		})
	}
}
