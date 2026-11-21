package bench

import (
	"errors"
	"path"
	"strings"
)

// RustTestStart returns the 1-based line of the "#[cfg(test)]" that opens a file's top-level test module, or 0 when
// the file has none. Attributes may sit between it and "mod"; an indented module is not top level.
func RustTestStart(src []byte) int {
	lines := strings.Split(string(src), "\n")
	for i, line := range lines {
		if strings.TrimRight(line, " \t\r") != "#[cfg(test)]" {
			continue
		}
		for _, next := range lines[i+1:] {
			next = strings.TrimRight(next, " \t\r")
			if next == "" || strings.HasPrefix(next, "#[") {
				continue
			}
			if strings.HasPrefix(next, "mod ") || strings.HasPrefix(next, "pub mod ") || strings.HasPrefix(next, "pub(crate) mod ") {
				return i + 1
			}
			break
		}
	}
	return 0
}

// SpliceRustTests puts the fix's top-level test module into the base file: the base's code up to its own test module,
// then the fix's module to the end of the file. So the fix's tests run against the code before the fix. The module
// must be the last item of both files, which is the Rust convention.
func SpliceRustTests(base, fix []byte) ([]byte, error) {
	fixStart := RustTestStart(fix)
	if fixStart == 0 {
		return nil, errors.New("the fix's file has no top-level test module")
	}
	tests := strings.Join(strings.Split(string(fix), "\n")[fixStart-1:], "\n")
	baseStart := RustTestStart(base)
	if baseStart == 0 {
		return []byte(strings.TrimRight(string(base), "\n") + "\n\n" + tests), nil
	}
	code := strings.Join(strings.Split(string(base), "\n")[:baseStart-1], "\n")
	return []byte(code + "\n" + tests), nil
}

// SourceTruth returns the truth spans of a fix diff for the toolchain: the old-side spans in source files, not in
// tests (doc.go, "Truth"). testFrom maps a Rust file to the first line of its inline test module (RustTestStart on the
// fix parent); a hunk that starts there or later is test code and gives no truth.
func SourceTruth(t Toolchain, files []FileDiff, testFrom map[string]int) []FileTruth {
	if t == ToolchainGo {
		return GoTruth(files)
	}
	var out []FileTruth
	for _, f := range files {
		if f.Status == FileAdded || !isSource(t, f.Path) {
			continue
		}
		ft := FileTruth{File: f.Path}
		for _, h := range f.Hunks {
			if from := testFrom[f.Path]; from > 0 && h.OldStart >= from {
				continue
			}
			ft.Lines = append(ft.Lines, oldSpan(h))
		}
		if len(ft.Lines) > 0 {
			out = append(out, ft)
		}
	}
	return out
}

// isSource says whether path is source code that ships for the toolchain: not a test, a type declaration, generated
// output or a dependency.
func isSource(t Toolchain, p string) bool {
	for _, part := range strings.Split(path.Dir(p), "/") {
		switch part {
		case "testdata", "vendor", "node_modules", "dist", "target", "__tests__", "__fixtures__":
			return false
		}
	}
	switch t {
	case ToolchainGo:
		return isGoSource(p)
	case ToolchainCargo:
		return strings.HasSuffix(p, ".rs") && !rustIntegrationTest(p)
	case ToolchainVitest:
		if vitestTestFile(p) || strings.HasSuffix(p, ".d.ts") {
			return false
		}
		for _, ext := range []string{".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs"} {
			if strings.HasSuffix(p, ext) {
				return true
			}
		}
	}
	return false
}

// rustIntegrationTest says whether path is a cargo integration test or bench: a .rs file under a tests/ or benches/
// directory.
func rustIntegrationTest(p string) bool {
	if !strings.HasSuffix(p, ".rs") {
		return false
	}
	for _, part := range strings.Split(path.Dir(p), "/") {
		if part == "tests" || part == "benches" {
			return true
		}
	}
	return false
}

// ToolchainTestFiles returns the test files of a fix diff that are copied whole into the tree before the fix: vitest
// test files, or cargo integration tests. Rust unit tests live inside source files, so the caller splices those
// (SpliceRustTests). A deleted file cannot run.
func ToolchainTestFiles(t Toolchain, files []FileDiff) []string {
	var out []string
	for _, f := range files {
		if f.Status == FileDeleted {
			continue
		}
		if (t == ToolchainVitest && vitestTestFile(f.Path)) || (t == ToolchainCargo && rustIntegrationTest(f.Path)) {
			out = append(out, f.Path)
		}
	}
	return out
}

// ChangesInstall says whether a diff touches an install input (a manifest, a lockfile or a pnpm patch), so a tree
// that took the diff must install again.
func ChangesInstall(files []FileDiff) bool {
	for _, f := range files {
		switch path.Base(f.Path) {
		case "package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml", ".pnpmfile.cjs", "Cargo.toml", "Cargo.lock":
			return true
		}
		if strings.HasPrefix(f.Path, "patches/") {
			return true
		}
	}
	return false
}

// TestLeaf returns the last part of a test id: the test title for vitest, the function name for cargo. The fix's
// file must hold it, so a typo in the manifest cannot name a test the fix did not write.
func TestLeaf(t Toolchain, id string) string {
	switch t {
	case ToolchainGo:
	case ToolchainVitest:
		_, name, ok := SplitTestID(id)
		if !ok {
			return id
		}
		parts := strings.Split(name, " > ")
		return parts[len(parts)-1]
	case ToolchainCargo:
		parts := strings.Split(id, "::")
		return parts[len(parts)-1]
	}
	return id
}
