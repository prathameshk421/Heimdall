package bench

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const rustBefore = `use std::io;

fn keep(x: u32) -> u32 {
    x
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn keeps() {
        assert_eq!(keep(1), 1);
    }
}
`

const rustFix = `use std::io;

fn keep(x: u32) -> u32 {
    x + 0
}

fn added() {}

#[cfg(test)]
#[allow(clippy::all)]
mod tests {
    use super::*;

    #[test]
    fn keeps() {
        assert_eq!(keep(1), 1);
    }

    #[test]
    fn keeps_two() {
        assert_eq!(keep(2), 2);
    }
}
`

func TestRustTestStartFindsTheTopLevelTestModule(t *testing.T) {
	require.Equal(t, 7, RustTestStart([]byte(rustBefore)))
	require.Equal(t, 9, RustTestStart([]byte(rustFix)), "attributes between cfg(test) and mod belong to the module")
	require.Zero(t, RustTestStart([]byte("fn main() {}\n")))
	require.Zero(t, RustTestStart([]byte("    #[cfg(test)]\n    mod inner {}\n")), "an indented module is not top level")
	require.Zero(t, RustTestStart([]byte("#[cfg(test)]\nfn helper() {}\n")), "cfg(test) on a function is not the module")
}

func TestSpliceRustTestsTakesTheFixModuleAndKeepsTheBaseCode(t *testing.T) {
	got, err := SpliceRustTests([]byte(rustBefore), []byte(rustFix))
	require.NoError(t, err)
	require.Contains(t, string(got), "    x\n}\n\n#[cfg(test)]\n#[allow", "the base code stays: the bug is still there")
	require.NotContains(t, string(got), "fn added()")
	require.Contains(t, string(got), "fn keeps_two()")
	require.Contains(t, string(got), "#[allow(clippy::all)]")
}

func TestSpliceRustTestsAppendsWhenTheBaseHasNoModule(t *testing.T) {
	got, err := SpliceRustTests([]byte("fn keep() {}\n"), []byte(rustFix))
	require.NoError(t, err)
	require.Equal(t, "fn keep() {}\n\n"+rustFix[len("use std::io;\n\nfn keep(x: u32) -> u32 {\n    x + 0\n}\n\nfn added() {}\n\n"):], string(got))
}

func TestSpliceRustTestsNeedsAModuleInTheFix(t *testing.T) {
	_, err := SpliceRustTests([]byte(rustBefore), []byte("fn keep() {}\n"))
	require.ErrorContains(t, err, "no top-level test module")
}

func TestSourceTruthForCargoDropsTheTestModule(t *testing.T) {
	files := []FileDiff{
		{Path: "native/m/src/main.rs", Status: FileModified, Hunks: []Hunk{
			{OldStart: 4, OldCount: 1, NewStart: 4, NewCount: 1},  // code
			{OldStart: 5, OldCount: 0, NewStart: 6, NewCount: 2},  // code insertion after line 5
			{OldStart: 7, OldCount: 0, NewStart: 10, NewCount: 1}, // attribute after cfg(test): test code
			{OldStart: 14, OldCount: 0, NewStart: 18, NewCount: 5},
		}},
		{Path: "native/m/tests/cli.rs", Status: FileModified, Hunks: []Hunk{{OldStart: 1, OldCount: 1, NewStart: 1, NewCount: 1}}},
		{Path: "native/m/Cargo.toml", Status: FileModified, Hunks: []Hunk{{OldStart: 1, OldCount: 1, NewStart: 1, NewCount: 1}}},
	}
	got := SourceTruth(ToolchainCargo, files, map[string]int{"native/m/src/main.rs": 7})
	require.Equal(t, []FileTruth{{File: "native/m/src/main.rs", Lines: []string{"4", "5-6"}}}, got)
}

func TestSourceTruthForVitestSkipsTestsAndTypes(t *testing.T) {
	h := []Hunk{{OldStart: 3, OldCount: 2, NewStart: 3, NewCount: 1}}
	files := []FileDiff{
		{Path: "packages/shared/src/x.ts", Status: FileModified, Hunks: h},
		{Path: "packages/shared/src/x.test.ts", Status: FileModified, Hunks: h},
		{Path: "packages/shared/src/view.tsx", Status: FileModified, Hunks: h},
		{Path: "packages/shared/src/types.d.ts", Status: FileModified, Hunks: h},
		{Path: "packages/shared/node_modules/y/index.js", Status: FileModified, Hunks: h},
		{Path: "packages/shared/src/new.ts", Status: FileAdded, Hunks: h},
		{Path: "packages/shared/package.json", Status: FileModified, Hunks: h},
	}
	got := SourceTruth(ToolchainVitest, files, nil)
	require.Equal(t, []FileTruth{{File: "packages/shared/src/x.ts", Lines: []string{"3-4"}}, {File: "packages/shared/src/view.tsx", Lines: []string{"3-4"}}}, got)
}

func TestSourceTruthForGoIsGoTruth(t *testing.T) {
	files := []FileDiff{{Path: "mock/mock.go", Status: FileModified, Hunks: []Hunk{{OldStart: 510, OldCount: 2, NewStart: 510, NewCount: 2}}}}
	require.Equal(t, GoTruth(files), SourceTruth(ToolchainGo, files, nil))
}

func TestToolchainTestFiles(t *testing.T) {
	files := []FileDiff{
		{Path: "packages/shared/src/x.test.ts", Status: FileModified},
		{Path: "packages/shared/src/gone.test.ts", Status: FileDeleted},
		{Path: "packages/shared/src/x.ts", Status: FileModified},
		{Path: "native/m/tests/cli.rs", Status: FileAdded},
		{Path: "native/m/src/main.rs", Status: FileModified},
	}
	require.Equal(t, []string{"packages/shared/src/x.test.ts"}, ToolchainTestFiles(ToolchainVitest, files))
	require.Equal(t, []string{"native/m/tests/cli.rs"}, ToolchainTestFiles(ToolchainCargo, files),
		"inline test modules are spliced, not copied (SpliceRustTests)")
}

func TestChangesInstall(t *testing.T) {
	require.False(t, ChangesInstall([]FileDiff{{Path: "packages/shared/src/x.ts"}}))
	for _, p := range []string{"packages/shared/package.json", "pnpm-lock.yaml", "native/m/Cargo.lock", "patches/a@1.patch"} {
		require.True(t, ChangesInstall([]FileDiff{{Path: p}}), p)
	}
}

func TestTestLeaf(t *testing.T) {
	require.Equal(t, "keeps a: b", TestLeaf(ToolchainVitest, "packages/x/a.test.ts:outer > keeps a: b"))
	require.Equal(t, "keeps", TestLeaf(ToolchainCargo, "tests::keeps"))
}
