package main

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/bench"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// The reviewer's test file for a cargo or vitest case: where it may go, how its tests are named, and how the sandbox
// runs only it.

// runOverlayTests runs the reviewer's tests: the vitest files it wrote, or the cargo tests in its modules (a filter
// of "module::" matches each test path that holds it).
func (rp *repo) runOverlayTests(ctx context.Context, c bench.Case, tree, label string, files, modules []string) (testRun, error) {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout+timeoutGrace)
	defer cancel()
	argv := strings.Fields(c.TestCommand)
	if c.Runner() == bench.ToolchainCargo {
		return rp.runCargo(ctx, c, tree, append(append(argv, "--"), modules...))
	}
	rel := make([]string, len(files))
	for i, f := range files {
		rel[i] = strings.TrimPrefix(f, c.Workdir+"/")
	}
	return rp.runVitest(ctx, c, tree, label, rel, argv)
}

// rustModule is a module name the reviewer may give its Rust test file.
var rustModule = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// testKind names the test file the reviewer writes, for the tool descriptions.
func testKind(c bench.Case) string {
	switch c.Runner() {
	case bench.ToolchainCargo:
		return "Rust test module file"
	case bench.ToolchainVitest:
		return "vitest test file"
	case bench.ToolchainGo:
	}
	return "Go test file"
}

// testPathHint describes test_path for the case's runner.
func testPathHint(c bench.Case) string {
	switch c.Runner() {
	case bench.ToolchainCargo:
		return "A new " + c.Workdir + "/src/<module>.rs file; the sandbox declares it as a #[cfg(test)] module of the crate root."
	case bench.ToolchainVitest:
		return "A new *.test.ts path under " + c.Workdir + "."
	case bench.ToolchainGo:
	}
	return "A new *_test.go path in the package under test."
}

// reviewTestPath refuses a test path the case's runner would not run as the reviewer's test: a Go *_test.go file, a
// vitest test file under the workdir, or a new Rust module file directly in the workdir's src directory.
func reviewTestPath(c bench.Case, p string) error {
	switch c.Runner() {
	case bench.ToolchainCargo:
		dir, file := path.Split(p)
		stem, ok := strings.CutSuffix(file, ".rs")
		if dir != c.Workdir+"/src/" || !ok || !rustModule.MatchString(stem) {
			return fmt.Errorf("test_path %q: want %s/src/<module>.rs with a lower-case module name", p, c.Workdir)
		}
	case bench.ToolchainVitest:
		if !strings.HasPrefix(p, c.Workdir+"/") || !bench.VitestTestFile(p) {
			return fmt.Errorf("test_path %q: want a *.test.ts file under %s", p, c.Workdir)
		}
	case bench.ToolchainGo:
		if !strings.HasSuffix(p, "_test.go") {
			return fmt.Errorf("test_path %q: want a *_test.go file", p)
		}
	}
	return nil
}

// reviewTestName checks the test a finding names and returns it as the sandbox names it: a Go test function, a
// Rust module::function, or a vitest describe path.
func reviewTestName(c bench.Case, testPath, source, name string) (string, error) {
	switch c.Runner() {
	case bench.ToolchainCargo:
		module := strings.TrimSuffix(path.Base(testPath), ".rs")
		fn, ok := strings.CutPrefix(name, module+"::")
		if !ok {
			fn = name
		}
		if strings.Contains(fn, "::") || !strings.Contains(source, "fn "+fn+"(") {
			return "", fmt.Errorf("test_name %q: test_source declares no fn %s() in module %s", name, fn, module)
		}
		return module + "::" + fn, nil
	case bench.ToolchainVitest:
		parts := strings.Split(name, " > ")
		if strings.TrimSpace(name) == "" || !strings.Contains(source, parts[len(parts)-1]) {
			return "", fmt.Errorf("test_name %q: test_source holds no test titled %q; use the name run_test listed", name, parts[len(parts)-1])
		}
		return name, nil
	case bench.ToolchainGo:
	}
	names, err := bench.TestFuncs([]byte(source), allLines(source))
	if err != nil {
		return "", fmt.Errorf("test_source: %w", err)
	}
	if !slices.Contains(names, name) {
		return "", fmt.Errorf("test_name %q: test_source declares no top-level test of that name (it declares %v)", name, names)
	}
	return name, nil
}

// focusNames names the tests the reviewer just wrote, so describeRun lists them first.
func (rp *replay) focusNames(testPath, source string, res ports.SandboxResult) ([]string, error) {
	switch rp.c.Runner() {
	case bench.ToolchainCargo:
		module := strings.TrimSuffix(path.Base(testPath), ".rs") + "::"
		var out []string
		for _, t := range res.Tests {
			if strings.HasPrefix(t.Name, module) {
				out = append(out, t.Name)
			}
		}
		return out, nil
	case bench.ToolchainVitest:
		return nil, nil // the sandbox runs only the reviewer's file
	case bench.ToolchainGo:
	}
	names, err := bench.TestFuncs([]byte(source), allLines(source))
	if err != nil {
		return nil, fmt.Errorf("test_source: %w", err)
	}
	return names, nil
}

// wireRustTestModule declares the reviewer's test file as a test-only module at the end of the crate root.
func wireRustTestModule(root []byte, module string) []byte {
	return []byte(strings.TrimRight(string(root), "\n") + "\n\n#[cfg(test)]\nmod " + module + ";\n")
}

// stripTestFiles renames each vitest test id to its name alone: the sandbox runs one file, the reviewer's. A cargo
// run keeps libtest's names, which hold "::" and no file.
func stripTestFiles(t bench.Toolchain, run bench.Run) bench.Run {
	if t != bench.ToolchainVitest {
		return run
	}
	out := bench.Run{Tests: map[string]bench.TestResult{}, BuildFailed: run.BuildFailed, BuildOutput: run.BuildOutput}
	for id, r := range run.Tests {
		if _, name, ok := bench.SplitTestID(id); ok {
			id = name
		}
		out.Tests[id] = r
	}
	return out
}
