package bench

import (
	"errors"
	"fmt"
	"go/token"
	"path"
	"slices"
	"strings"
)

// Toolchain names the runner of a case: the tool that installs its dependencies and runs its tests.
type Toolchain string

// The toolchains with a runner.
const (
	ToolchainGo     Toolchain = "go"     // go test -json, per package (gotest.go)
	ToolchainCargo  Toolchain = "cargo"  // cargo test, libtest's text output (runners.go)
	ToolchainVitest Toolchain = "vitest" // vitest or Vite+ with the JSON reporter (runners.go)
)

// Runner returns the case's toolchain. A Go case with no toolchain runs go test. Any other case with no toolchain
// has no runner, and the result is empty.
func (c Case) Runner() Toolchain {
	if c.Toolchain != "" {
		return c.Toolchain
	}
	if c.Language == "go" {
		return ToolchainGo
	}
	return ""
}

// toolchainLanguages lists the languages each toolchain runs.
func toolchainLanguages() map[Toolchain][]string {
	return map[Toolchain][]string{
		ToolchainGo:     {"go"},
		ToolchainCargo:  {"rust"},
		ToolchainVitest: {"typescript", "javascript"},
	}
}

// VitestTestFile says whether path is a file vitest collects by default: *.test.* or *.spec.* with a JS or TS
// extension.
func VitestTestFile(p string) bool {
	base := path.Base(p)
	for _, ext := range []string{".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs"} {
		stem, ok := strings.CutSuffix(base, ext)
		if ok && (strings.HasSuffix(stem, ".test") || strings.HasSuffix(stem, ".spec")) {
			return true
		}
	}
	return false
}

// checkToolchain checks the toolchain fields of a case (doc.go, "Toolchains").
func checkToolchain(c Case) []error {
	langs, known := toolchainLanguages()[c.Toolchain]
	switch {
	case c.Toolchain == "":
	case !known:
		return []error{fmt.Errorf("toolchain %q: want go, cargo or vitest", c.Toolchain)}
	case !slices.Contains(langs, c.Language):
		return []error{fmt.Errorf("toolchain %s runs %s, not %s", c.Toolchain, strings.Join(langs, " or "), c.Language)}
	}
	if c.Toolchain == "" || c.Toolchain == ToolchainGo {
		if c.Workdir != "" || len(c.Install) > 0 || len(c.Sparse) > 0 {
			return []error{errors.New("a go case, or a case with no toolchain, has no workdir, install or sparse: go test finds them")}
		}
		return checkGoTests(c)
	}
	var errs []error
	if !cleanDir(c.Workdir) {
		errs = append(errs, fmt.Errorf("workdir %q: want a clean relative directory inside the repository", c.Workdir))
	}
	errs = append(errs, checkLines("install step", c.Install)...)
	errs = append(errs, checkLines("sparse pattern", c.Sparse)...)
	errs = append(errs, checkTestCommand(c)...)
	errs = append(errs, checkTests(c)...)
	return errs
}

// SplitGoTestID splits a Go test id into its package directory (relative to the repository root, "." for the root) and
// its test function: "mock:Test_AssertExpectations".
func SplitGoTestID(id string) (dir, name string, ok bool) {
	dir, name, ok = strings.Cut(id, ":")
	if !ok || (dir != "." && !cleanDir(dir)) || !token.IsIdentifier(name) || !isTestName(name) {
		return "", "", false
	}
	return dir, name, true
}

// checkGoTests checks the tests of a Go case. A Go bug case may name the fix's target tests; then the before tree gets
// only those tests from the fix, not its whole test files. With none named, the targets are the tests the fix adds
// lines to (cmd/heimdall-bench). A clean case runs its whole test command.
func checkGoTests(c Case) []error {
	if len(c.Tests) == 0 {
		return nil
	}
	if c.Kind == KindClean {
		return []error{errors.New("a clean case has no tests: it runs its whole test command")}
	}
	var errs []error
	seen := map[string]bool{}
	for _, id := range c.Tests {
		if _, _, ok := SplitGoTestID(id); !ok {
			errs = append(errs, fmt.Errorf("test id %q: want dir:TestName (a clean directory from the repository root, or ., and a top-level test function)", id))
		}
		if seen[id] {
			errs = append(errs, fmt.Errorf("test id %q: named twice", id))
		}
		seen[id] = true
	}
	return errs
}

// cleanDir says whether dir is a clean relative directory inside the repository.
func cleanDir(dir string) bool {
	return dir != "" && path.Clean(dir) == dir && dir != "." && dir != ".." && !strings.HasPrefix(dir, "../") && !path.IsAbs(dir)
}

// checkLines refuses an empty or multi-line entry.
func checkLines(what string, lines []string) []error {
	var errs []error
	for _, s := range lines {
		if strings.TrimSpace(s) == "" || strings.Contains(s, "\n") {
			errs = append(errs, fmt.Errorf("%s %q: want one non-empty line", what, s))
		}
	}
	return errs
}

func checkTestCommand(c Case) []error {
	argv := strings.Fields(c.TestCommand)
	switch c.Toolchain {
	case ToolchainGo:
	case ToolchainCargo:
		if len(argv) < 2 || argv[0] != "cargo" || argv[1] != "test" {
			return []error{fmt.Errorf("test_command %q: a cargo case runs cargo test", c.TestCommand)}
		}
		if slices.Contains(argv, "--") {
			return []error{fmt.Errorf("test_command %q: the runner adds -- and the test names", c.TestCommand)}
		}
	case ToolchainVitest:
		for _, a := range argv {
			if strings.HasPrefix(a, "--reporter") || strings.HasPrefix(a, "--outputFile") {
				return []error{fmt.Errorf("test_command %q: the runner sets the reporter and the output file", c.TestCommand)}
			}
		}
	}
	return nil
}

// checkTests checks the target tests: a bug case names them, a clean case has none. A vitest id is a test file under
// the workdir, a colon and the test's name (SplitTestID). A cargo id is libtest's test name.
func checkTests(c Case) []error {
	if c.Kind == KindClean {
		if len(c.Tests) > 0 {
			return []error{errors.New("a clean case has no tests: it runs its whole test command")}
		}
		return nil
	}
	if len(c.Tests) == 0 {
		return []error{errors.New("tests: a cargo or vitest bug case names the fix's tests")}
	}
	var errs []error
	for _, id := range c.Tests {
		if strings.TrimSpace(id) == "" || strings.Contains(id, "\n") {
			errs = append(errs, fmt.Errorf("test id %q: want one non-empty line", id))
			continue
		}
		if c.Toolchain != ToolchainVitest {
			continue
		}
		file, _, ok := SplitTestID(id)
		switch {
		case !ok:
			errs = append(errs, fmt.Errorf("test id %q: want file:name", id))
		case !strings.HasPrefix(file, c.Workdir+"/"):
			errs = append(errs, fmt.Errorf("test id %q: the file must be under workdir %s", id, c.Workdir))
		case !VitestTestFile(file):
			errs = append(errs, fmt.Errorf("test id %q: %s is not a test file (*.test.ts or *.spec.ts)", id, file))
		}
	}
	return errs
}
