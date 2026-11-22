package gograph_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/gograph"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testselect"
)

const mod = "github.com/prathameshk421/flip_the_switch_hackathon/"

// golist.json is the real output of
//
//	go list -deps -test -json ./internal/domain/finding ./internal/domain/testset ./internal/domain/id ./internal/archcheck
//
// run in this repository (Go 1.27.4). Only the main-module records, two standard library records and one
// dependency are kept, and the absolute paths are rewritten to /work/repo. The test variants and the generated
// test mains stay in, so the parser must skip them.
func realOutput(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/golist.json")
	require.NoError(t, err)
	return string(b)
}

func TestParseRealOutput(t *testing.T) {
	g, err := gograph.Parse(strings.NewReader(realOutput(t)), gograph.Options{Root: "/work/repo"})
	require.NoError(t, err)

	tests := []struct {
		name    string
		changed []string
		want    []string
	}{
		{
			name:    "the id package selects every package that imports it, tests included",
			changed: []string{"internal/domain/id/ulid.go"},
			want:    []string{mod + "internal/domain/finding", mod + "internal/domain/id", mod + "internal/domain/testset"},
		},
		{
			name:    "the finding package selects itself and testset",
			changed: []string{"internal/domain/finding/check.go"},
			want:    []string{mod + "internal/domain/finding", mod + "internal/domain/testset"},
		},
		{
			name:    "a leaf selects itself",
			changed: []string{"internal/archcheck/archcheck.go"},
			want:    []string{mod + "internal/archcheck"},
		},
		{
			name:    "a test file selects its own package only",
			changed: []string{"internal/domain/id/id_test.go"},
			want:    []string{mod + "internal/domain/id"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := testselect.Select(g, tt.changed, testselect.Policy{})
			require.NoError(t, err)
			require.False(t, got.Full, got.FullReason)
			require.ElementsMatch(t, tt.want, got.Paths())
		})
	}

	// The standard library, a dependency, a test variant and a generated test main are not packages of the graph.
	for _, f := range []string{"errors/errors.go", "github.com/stretchr/testify/require/require.go"} {
		got, err := testselect.Select(g, []string{f}, testselect.Policy{})
		require.NoError(t, err)
		require.True(t, got.Full, f)
	}
}

func TestParseRefusals(t *testing.T) {
	record := func(path, dir string, extra string) string {
		return `{"ImportPath":"` + path + `","Dir":"` + dir + `","Module":{"Path":"m","Main":true}` + extra + `}`
	}
	tests := []struct {
		name string
		in   string
		opt  gograph.Options
		want error
	}{
		{"not JSON", "package main", gograph.Options{Root: "/r"}, gograph.ErrMalformed},
		{"cut off", record("m/a", "/r/a", "")[:20], gograph.Options{Root: "/r"}, gograph.ErrMalformed},
		{"no main-module package", `{"ImportPath":"fmt","Standard":true}`, gograph.Options{Root: "/r"}, gograph.ErrMalformed},
		{"empty input", "", gograph.Options{Root: "/r"}, gograph.ErrMalformed},
		{"package outside the root", record("m/a", "/elsewhere/a", ""), gograph.Options{Root: "/r"}, gograph.ErrMalformed},
		{"package with a load error", record("m/a", "/r/a", `,"Error":{"Err":"no Go files"}`), gograph.Options{Root: "/r"}, gograph.ErrMalformed},
		{"same package twice", record("m/a", "/r/a", "") + record("m/a", "/r/a", ""), gograph.Options{Root: "/r"}, gograph.ErrMalformed},
		{"too large", record("m/a", "/r/a", "") + strings.Repeat(" ", 100), gograph.Options{Root: "/r", MaxBytes: 64}, gograph.ErrTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := gograph.Parse(strings.NewReader(tt.in), tt.opt)
			require.ErrorIs(t, err, tt.want)
		})
	}
}

func TestParseRefusesAMissingRoot(t *testing.T) {
	_, err := gograph.Parse(strings.NewReader(`{}`), gograph.Options{})
	require.ErrorContains(t, err, "root")
}

func TestParseDropsSelfImportsAndForeignModulePackages(t *testing.T) {
	in := `{"ImportPath":"m/a","Dir":"/r/a","Module":{"Main":true},"Imports":["m/b"],"XTestImports":["m/a"],"XTestGoFiles":["a_test.go"]}
{"ImportPath":"m/b","Dir":"/r/b","Module":{"Main":true},"Imports":["x/dep"]}
{"ImportPath":"x/dep","Dir":"/mod/dep","Module":{"Main":false},"Imports":["m/a"]}`
	g, err := gograph.Parse(strings.NewReader(in), gograph.Options{Root: "/r"})
	require.NoError(t, err)
	got, err := testselect.Select(g, []string{"b/b.go"}, testselect.Policy{})
	require.NoError(t, err)
	// m/b has no tests; m/a imports it and has tests (an xtest).
	require.Equal(t, []string{"m/a"}, got.Paths())
}
