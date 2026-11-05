package archcheck_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/archcheck"
)

func TestDecode(t *testing.T) {
	stream := `{"ImportPath":"example.com/m/internal/domain/a","Imports":["fmt","example.com/m/internal/ports"],"Deps":["fmt","errors"],"Module":{"Path":"example.com/m"}}
{"ImportPath":"example.com/m/cmd/x"}`
	got, err := archcheck.Decode(strings.NewReader(stream))
	require.NoError(t, err)
	want := []archcheck.Package{
		{ImportPath: "example.com/m/internal/domain/a", Imports: []string{"fmt", "example.com/m/internal/ports"}, Deps: []string{"fmt", "errors"}},
		{ImportPath: "example.com/m/cmd/x"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("packages mismatch (-want +got):\n%s", diff)
	}
}

func TestDecodeRejectsAPackageThatDidNotLoad(t *testing.T) {
	for name, stream := range map[string]string{
		"package error":    `{"ImportPath":"example.com/m/a","Error":{"Err":"cannot find package"}}`,
		"dependency error": `{"ImportPath":"example.com/m/a","DepsErrors":[{"Err":"no required module provides package example.com/m/missing"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := archcheck.Decode(strings.NewReader(stream))
			require.ErrorContains(t, err, "example.com/m/a")
		})
	}
}

func TestDecodeRejectsGarbage(t *testing.T) {
	_, err := archcheck.Decode(strings.NewReader(`{"ImportPath": `))
	require.Error(t, err)
}

// writeModule lays out a throwaway module so the checker is proven on a real `go list`, not only on tables.
func writeModule(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files["go.mod"] = "module example.com/m\n\ngo 1.22\n"
	for name, body := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	}
	return dir
}

func TestListFindsABadImportInARealModule(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"internal/ports/p.go":       "package ports\n",
		"internal/domain/gate/g.go": "package gate\n\nimport _ \"example.com/m/internal/ports\"\n",
		"internal/testkit/k.go":     "package testkit\n",
		"cmd/server/main.go":        "package main\n\nimport _ \"example.com/m/internal/testkit\"\n\nfunc main() {}\n",
	})
	module, pkgs, err := archcheck.List(context.Background(), dir)
	require.NoError(t, err)
	require.Equal(t, "example.com/m", module)

	got := archcheck.Check(module, pkgs)
	want := []archcheck.Violation{
		{Package: "cmd/server", Import: "internal/testkit", Rule: "a production binary must not depend on test code (D121)"},
		{Package: "cmd/server", Import: "internal/testkit", Rule: "test code stays out of production (internal/testkit is for tests, cmd/heimdall-testagent and e2e)"},
		{Package: "internal/domain/gate", Import: "internal/ports", Rule: "domain imports only the standard library and internal/domain"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("violations mismatch (-want +got):\n%s", diff)
	}
}

func TestListOnACleanModuleFindsNothing(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"internal/domain/gate/g.go": "package gate\n\nimport \"fmt\"\n\nvar _ = fmt.Sprint\n",
		"internal/app/a.go":         "package app\n\nimport _ \"example.com/m/internal/domain/gate\"\n",
	})
	module, pkgs, err := archcheck.List(context.Background(), dir)
	require.NoError(t, err)
	require.Empty(t, archcheck.Check(module, pkgs))
}

func TestListFailsOnAModuleThatDoesNotBuild(t *testing.T) {
	dir := writeModule(t, map[string]string{"a/a.go": "package a\n\nimport _ \"example.com/m/missing\"\n"})
	_, _, err := archcheck.List(context.Background(), dir)
	require.Error(t, err)
}
