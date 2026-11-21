package testselect_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testselect"
)

// graph builds this synthetic tree (an arrow means "imports"):
//
//	svc/c -> lib/b -> lib/a
//	svc/e -> mid (no tests) -> lib/a
//	svc/d -> internal/testutil (a test import only)
//	lib/b -> internal/testutil (a test import only)
//	cyc/x <-> cyc/y
//	tools/gen has no tests
func graph(t *testing.T) testselect.Graph {
	t.Helper()
	g, err := testselect.NewGraph([]testselect.Package{
		{Path: "m/lib/a", Dir: "lib/a", HasTests: true},
		{Path: "m/lib/b", Dir: "lib/b", HasTests: true, Imports: []string{"m/lib/a", "m/internal/testutil", "fmt"}},
		{Path: "m/svc/c", Dir: "svc/c", HasTests: true, Imports: []string{"m/lib/b"}},
		{Path: "m/mid", Dir: "mid", Imports: []string{"m/lib/a"}},
		{Path: "m/svc/e", Dir: "svc/e", HasTests: true, Imports: []string{"m/mid"}},
		{Path: "m/svc/d", Dir: "svc/d", HasTests: true, Imports: []string{"m/internal/testutil"}},
		{Path: "m/internal/testutil", Dir: "internal/testutil", HasTests: true},
		{Path: "m/cyc/x", Dir: "cyc/x", HasTests: true, Imports: []string{"m/cyc/y"}},
		{Path: "m/cyc/y", Dir: "cyc/y", HasTests: true, Imports: []string{"m/cyc/x"}},
		{Path: "m/tools/gen", Dir: "tools/gen"},
	})
	require.NoError(t, err)
	return g
}

func TestSelect(t *testing.T) {
	tests := []struct {
		name    string
		changed []string
		pol     testselect.Policy
		want    []string
	}{
		{name: "a leaf change selects the leaf only", changed: []string{"svc/d/d.go"}, want: []string{"m/svc/d"}},
		{
			name:    "a shared library selects its dependents through a package without tests",
			changed: []string{"lib/a/a.go"},
			want:    []string{"m/lib/a", "m/lib/b", "m/svc/c", "m/svc/e"},
		},
		{
			name:    "a test import counts as a dependency",
			changed: []string{"internal/testutil/util.go"},
			want:    []string{"m/internal/testutil", "m/lib/b", "m/svc/c", "m/svc/d"},
		},
		{name: "a test-only change selects its own package", changed: []string{"lib/a/a_test.go"}, want: []string{"m/lib/a"}},
		{
			name:    "a test file selects its package even when the graph says it has no tests",
			changed: []string{"tools/gen/gen_test.go"},
			want:    []string{"m/tools/gen"},
		},
		{name: "a package without tests is not selected", changed: []string{"tools/gen/gen.go"}, want: nil},
		{name: "a non-Go file maps to the nearest package", changed: []string{"lib/a/testdata/in/x.json"}, want: []string{"m/lib/a", "m/lib/b", "m/svc/c", "m/svc/e"}},
		{name: "a Go file under testdata is not a package file", changed: []string{"svc/d/testdata/fixture/x.go"}, want: []string{"m/svc/d"}},
		{name: "an import cycle ends", changed: []string{"cyc/x/x.go"}, want: []string{"m/cyc/x", "m/cyc/y"}},
		{name: "two changes merge", changed: []string{"svc/d/d.go", "svc/c/c.go", "svc/d/d.go"}, want: []string{"m/svc/c", "m/svc/d"}},
		{name: "nothing changed selects nothing", changed: nil, want: nil},
		{
			name:    "always-run packages join the selection",
			changed: []string{"svc/d/d.go"},
			pol:     testselect.Policy{AlwaysRun: []string{"cyc/**"}},
			want:    []string{"m/cyc/x", "m/cyc/y", "m/svc/d"},
		},
		{
			name:    "ignore drops a file no test reads",
			changed: []string{"docs/guide.md", "svc/d/d.go"},
			pol:     testselect.Policy{Ignore: []string{"docs/**", "*.md"}},
			want:    []string{"m/svc/d"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := testselect.Select(graph(t), tt.changed, tt.pol)
			require.NoError(t, err)
			require.False(t, got.Full, got.FullReason)
			require.ElementsMatch(t, tt.want, got.Paths())
		})
	}
}

func TestSelectFull(t *testing.T) {
	tests := []struct {
		name    string
		g       testselect.Graph
		changed []string
		pol     testselect.Policy
		reason  string
	}{
		{name: "go.mod", changed: []string{"go.mod"}, reason: "changed file go.mod is a build file (go.mod)"},
		{name: "go.sum below the root", changed: []string{"svc/d/go.sum"}, reason: "changed file svc/d/go.sum is a build file (go.sum)"},
		{name: "vendor tree", changed: []string{"vendor/x/y.go"}, reason: "changed file vendor/x/y.go is a build file (vendor/**)"},
		{
			name:    "policy trigger",
			changed: []string{"build/ci.mk"},
			pol:     testselect.Policy{FullTriggers: []string{"*.mk"}},
			reason:  "changed file build/ci.mk matches the run-everything trigger *.mk",
		},
		{
			name:    "pinned test",
			changed: []string{"lib/a/a_test.go"},
			pol:     testselect.Policy{Pinned: []string{"lib/a/*_test.go"}},
			reason:  "changed file lib/a/a_test.go is a Heimdall-pinned test (lib/a/*_test.go)",
		},
		{
			name:    "a trigger wins over ignore",
			changed: []string{"docs/go.mod"},
			pol:     testselect.Policy{Ignore: []string{"docs/**"}},
			reason:  "changed file docs/go.mod is a build file (go.mod)",
		},
		{name: "unknown file", changed: []string{"scripts/run.sh", "svc/d/d.go"}, reason: "changed file scripts/run.sh belongs to no known package"},
		{name: "new Go file in an unknown directory", changed: []string{"svc/new/new.go"}, reason: "changed file svc/new/new.go belongs to no known package"},
		{name: "path escapes the repository", changed: []string{"../x.go"}, reason: `changed path "../x.go" is not a clean relative path`},
		{name: "unknown graph", g: testselect.Graph{}, changed: []string{"svc/d/d.go"}, reason: "the package graph is unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := tt.g
			if tt.name != "unknown graph" {
				g = graph(t)
			}
			got, err := testselect.Select(g, tt.changed, tt.pol)
			require.NoError(t, err)
			require.True(t, got.Full)
			require.Equal(t, tt.reason, got.FullReason)
			require.Empty(t, got.Packages)
		})
	}
}

func TestSelectReasons(t *testing.T) {
	got, err := testselect.Select(graph(t), []string{"lib/a/a.go", "svc/d/d_test.go"}, testselect.Policy{AlwaysRun: []string{"cyc/x"}})
	require.NoError(t, err)
	reasons := map[string]string{}
	for _, p := range got.Packages {
		reasons[p.Path] = p.Reason.String()
	}
	require.Equal(t, map[string]string{
		"m/lib/a": "changed file lib/a/a.go",
		"m/lib/b": "imports lib/a, reached from changed file lib/a/a.go",
		"m/svc/c": "imports lib/b, reached from changed file lib/a/a.go",
		"m/svc/e": "imports mid, reached from changed file lib/a/a.go",
		"m/svc/d": "changed test file svc/d/d_test.go",
		"m/cyc/x": "always run by policy glob cyc/x",
	}, reasons)
}

func TestSelectRefusesABadPolicyGlob(t *testing.T) {
	_, err := testselect.Select(graph(t), []string{"svc/d/d.go"}, testselect.Policy{FullTriggers: []string{"[bad"}})
	require.ErrorContains(t, err, "full_triggers")
}

func TestNewGraphRefusals(t *testing.T) {
	tests := []struct {
		name string
		pkgs []testselect.Package
	}{
		{"no path", []testselect.Package{{Dir: "a"}}},
		{"absolute directory", []testselect.Package{{Path: "m/a", Dir: "/a"}}},
		{"directory leaves the root", []testselect.Package{{Path: "m/a", Dir: "../a"}}},
		{"unclean directory", []testselect.Package{{Path: "m/a", Dir: "a/"}}},
		{"empty directory", []testselect.Package{{Path: "m/a"}}},
		{"duplicate path", []testselect.Package{{Path: "m/a", Dir: "a"}, {Path: "m/a", Dir: "b"}}},
		{"shared directory", []testselect.Package{{Path: "m/a", Dir: "a"}, {Path: "m/b", Dir: "a"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := testselect.NewGraph(tt.pkgs)
			require.ErrorIs(t, err, testselect.ErrInvalidGraph)
		})
	}
}

func TestRootPackageOwnsNonGoFilesWithoutAnotherOwner(t *testing.T) {
	g, err := testselect.NewGraph([]testselect.Package{
		{Path: "m", Dir: ".", HasTests: true},
		{Path: "m/sub", Dir: "sub", HasTests: true, Imports: []string{"m"}},
	})
	require.NoError(t, err)
	got, err := testselect.Select(g, []string{"README.md"}, testselect.Policy{})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"m", "m/sub"}, got.Paths())
	got, err = testselect.Select(g, []string{"x.go"}, testselect.Policy{})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"m", "m/sub"}, got.Paths())
	got, err = testselect.Select(g, []string{"sub/y.go"}, testselect.Policy{})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"m/sub"}, got.Paths())
}
