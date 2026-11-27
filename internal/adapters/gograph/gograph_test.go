package gograph_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/gograph"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testselect"
)

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
