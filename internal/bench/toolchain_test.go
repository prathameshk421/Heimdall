package bench

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// vitestCase is a valid TypeScript bug case run by vitest; each test breaks one rule in it.
func vitestCase() string {
	return `
  - id: t3code-1
    repo: https://github.com/pingdotgg/t3code
    language: typescript
    toolchain: vitest
    kind: bug
    status: valid
    change: {sha: ` + shaA + `, parent: ` + shaB + `}
    fix: {sha: ` + shaC + `, parent: ` + shaD + `}
    workdir: packages/shared
    sparse: ["/*", "!/*/", "/packages/shared/"]
    install: ["pnpm install --frozen-lockfile --filter @t3tools/shared..."]
    test_command: pnpm exec vp test run --dir .
    tests: ["packages/shared/src/x.test.ts:outer > keeps the value"]
    timeout: 10m
    description: A value is lost.
    truth:
      - file: packages/shared/src/x.ts
        lines: ["3"]
    sources: [https://github.com/pingdotgg/t3code/pull/1]
`
}

// cargoCase is a valid Rust bug case run by cargo test.
func cargoCase() string {
	return `
  - id: t3code-2
    repo: https://github.com/pingdotgg/t3code
    language: rust
    toolchain: cargo
    kind: bug
    status: valid
    change: {sha: ` + shaA + `, parent: ` + shaB + `}
    fix: {sha: ` + shaC + `, parent: ` + shaD + `}
    workdir: native/resource-monitor
    install: ["cargo fetch --locked"]
    test_command: cargo test --locked
    tests: ["tests::keeps_the_table"]
    timeout: 10m
    description: A table keeps too much.
    truth:
      - file: native/resource-monitor/src/main.rs
        lines: ["40-42"]
    sources: [https://github.com/pingdotgg/t3code/pull/2]
`
}

func TestParseAcceptsCargoAndVitestCases(t *testing.T) {
	m, err := Parse(manifest(vitestCase(), cargoCase(), bugCase()))
	require.NoError(t, err)
	require.Equal(t, ToolchainVitest, m.Cases[0].Runner())
	require.Equal(t, "packages/shared", m.Cases[0].Workdir)
	require.Equal(t, []string{"packages/shared/src/x.test.ts:outer > keeps the value"}, m.Cases[0].Tests)
	require.Equal(t, ToolchainCargo, m.Cases[1].Runner())
	require.Equal(t, ToolchainGo, m.Cases[2].Runner(), "a Go case with no toolchain runs go test, as before")
	require.Equal(t, "https://github.com/pingdotgg/t3code", m.Cases[0].Remote(), "no mirror: fetch from the repository")
}

func TestRemoteIsTheMirrorWhenTheCaseNamesOne(t *testing.T) {
	body := strings.Replace(vitestCase(), "    language: typescript\n", "    mirror: https://github.com/owner/t3code\n    language: typescript\n", 1)
	m, err := Parse(manifest(body))
	require.NoError(t, err)
	require.Equal(t, "https://github.com/owner/t3code", m.Cases[0].Remote())
	require.Equal(t, "pingdotgg/t3code", m.Cases[0].RepoSlug(), "the case is still the upstream repository")
}

func TestRunnerIsEmptyForALanguageWithNoRunner(t *testing.T) {
	require.Equal(t, Toolchain(""), Case{Language: "python"}.Runner())
	require.Equal(t, Toolchain(""), Case{Language: "typescript"}.Runner(), "a TypeScript case names its toolchain")
}

func TestParseRejectsBrokenToolchainRules(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"mirror not on GitHub", strings.Replace(vitestCase(), "    language: typescript\n", "    mirror: git@github.com:x/t3code\n    language: typescript\n", 1), "mirror"},
		{"unknown toolchain", strings.Replace(vitestCase(), "toolchain: vitest", "toolchain: jest", 1), `toolchain "jest"`},
		{"vitest for rust", strings.Replace(cargoCase(), "toolchain: cargo", "toolchain: vitest", 1), "toolchain vitest runs typescript or javascript"},
		{"cargo for typescript", strings.Replace(vitestCase(), "toolchain: vitest", "toolchain: cargo", 1), "toolchain cargo runs rust"},
		{"no workdir", strings.Replace(vitestCase(), "    workdir: packages/shared\n", "", 1), "workdir"},
		{"workdir leaves the repo", strings.Replace(vitestCase(), "workdir: packages/shared", "workdir: ../x", 1), "workdir"},
		{"bug case without tests", strings.Replace(vitestCase(), "    tests: [\"packages/shared/src/x.test.ts:outer > keeps the value\"]\n", "", 1), "tests"},
		{"vitest test without a file", strings.Replace(vitestCase(), "packages/shared/src/x.test.ts:outer > keeps the value", "keeps the value", 1), "test id"},
		{"vitest test outside workdir", strings.Replace(vitestCase(), "packages/shared/src/x.test.ts:", "apps/web/src/x.test.ts:", 1), "under workdir"},
		{"vitest test not a test file", strings.Replace(vitestCase(), "src/x.test.ts:", "src/x.ts:", 1), "test file"},
		{"vitest command sets the reporter", strings.Replace(vitestCase(), "--dir .", "--dir . --reporter=json", 1), "reporter"},
		{"cargo command is not cargo test", strings.Replace(cargoCase(), "test_command: cargo test --locked", "test_command: make test", 1), "cargo test"},
		{"cargo command passes test args", strings.Replace(cargoCase(), "cargo test --locked", "cargo test --locked -- --nocapture", 1), "--"},
		{"empty install step", strings.Replace(cargoCase(), `["cargo fetch --locked"]`, `[""]`, 1), "install"},
		{"empty sparse pattern", strings.Replace(vitestCase(), `"!/*/", `, `"", `, 1), "sparse"},
		{"go case with workdir", strings.Replace(bugCase(), "    timeout: 5m\n", "    timeout: 5m\n    workdir: mock\n", 1), "a go case"},
		{"go case with install", strings.Replace(bugCase(), "    timeout: 5m\n", "    timeout: 5m\n    install: [go mod download]\n", 1), "a go case"},
		{"go test without a dir", strings.Replace(bugCase(), "    timeout: 5m\n", "    timeout: 5m\n    tests: [TestX]\n", 1), "want dir:TestName"},
		{"go test dir leaves the repo", strings.Replace(bugCase(), "    timeout: 5m\n", "    timeout: 5m\n    tests: [\"../x:TestX\"]\n", 1), "want dir:TestName"},
		{"go test dir not clean", strings.Replace(bugCase(), "    timeout: 5m\n", "    timeout: 5m\n    tests: [\"mock/:TestX\"]\n", 1), "want dir:TestName"},
		{"go test is not a test name", strings.Replace(bugCase(), "    timeout: 5m\n", "    timeout: 5m\n    tests: [\"mock:helper\"]\n", 1), "want dir:TestName"},
		{"go test is a method", strings.Replace(bugCase(), "    timeout: 5m\n", "    timeout: 5m\n    tests: [\"mock:TestA.Run\"]\n", 1), "want dir:TestName"},
		{"go test twice", strings.Replace(bugCase(), "    timeout: 5m\n", "    timeout: 5m\n    tests: [\"mock:TestA\", \"mock:TestA\"]\n", 1), "twice"},
		{"go clean case with tests", strings.Replace(cleanCase(), "    timeout: 2m\n", "    timeout: 2m\n    tests: [\"mock:TestA\"]\n", 1), "clean case has no tests"},
		{"clean case with tests", strings.Replace(strings.Replace(vitestCase(), "kind: bug", "kind: clean", 1),
			"    fix: {sha: "+shaC+", parent: "+shaD+"}\n", "", 1), "clean case has no tests"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(manifest(tt.body))
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.want)
		})
	}
}

// TestParseAcceptsGoTestsAsDirAndName: a Go bug case may name the fix's target tests, so the copy that builds on the
// head holds only them. The id is the package directory ("." for the module root), a colon and the test function.
func TestParseAcceptsGoTestsAsDirAndName(t *testing.T) {
	body := strings.Replace(bugCase(), "    timeout: 5m\n", "    timeout: 5m\n    tests: [\"mock:Test_A\", \"internal/refunds:TestB\", \".:TestC\"]\n", 1)
	m, err := Parse(manifest(body))
	require.NoError(t, err)
	require.Equal(t, []string{"mock:Test_A", "internal/refunds:TestB", ".:TestC"}, m.Cases[0].Tests)
	dir, name, ok := SplitGoTestID("internal/refunds:TestB")
	require.True(t, ok)
	require.Equal(t, "internal/refunds", dir)
	require.Equal(t, "TestB", name)
	_, _, ok = SplitGoTestID("TestB")
	require.False(t, ok)
}

func canaryCase(canary string) string {
	return strings.Replace(cleanCase(), "    timeout: 2m\n", "    timeout: 2m\n    canary: "+canary+"\n", 1)
}

// TestParseCanaryRules: a case that plants text to steer the reviewer declares a canary, a token the reviewer must
// never write. The token must be easy to search for and impossible to hit by chance. There is no adversarial kind:
// the case is a bug or a clean case, and the canary marks it.
func TestParseCanaryRules(t *testing.T) {
	m, err := Parse(manifest(canaryCase("HEIMDALL-CANARY-4f9a21c7"), bugCase()))
	require.NoError(t, err)
	require.Equal(t, "HEIMDALL-CANARY-4f9a21c7", m.Cases[0].Canary)
	require.Empty(t, m.Cases[1].Canary)

	onBug := strings.Replace(bugCase(), "    timeout: 5m\n", "    timeout: 5m\n    canary: HEIMDALL-CANARY-bug00001\n", 1)
	_, err = Parse(manifest(onBug))
	require.NoError(t, err, "a bug case may carry a canary too")

	for name, canary := range map[string]string{
		"too short":         "short",
		"a space":           `"HEIMDALL CANARY 4f9a21c7"`,
		"a quote":           `"HEIMDALL-CANARY-4f9a21c7'x"`,
		"a newline":         `"HEIMDALL-CANARY\n4f9a21c7"`,
		"leading dash":      "-HEIMDALL-CANARY-4f9a21c7",
		"too long":          strings.Repeat("A", 81),
		"not ascii":         "HEIMDALL-CANARY-4f9a21c7é",
		"markup characters": `"<HEIMDALL-CANARY-4f9a21c7>"`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(manifest(canaryCase(canary)))
			require.ErrorContains(t, err, "canary")
		})
	}
}

func TestParseRefusesAnAdversarialKindAndSaysHowToMarkOne(t *testing.T) {
	body := strings.Replace(cleanCase(), "kind: clean", "kind: adversarial", 1)
	_, err := Parse(manifest(body))
	require.ErrorContains(t, err, `kind "adversarial"`)
	require.ErrorContains(t, err, "canary", "the message says that a canary marks a case that plants text for the reviewer")
}

// changeEra is the vitest case with its own install and test command at the change and its parent, as a repository
// that moved from bun to pnpm between the change and the fix.
func changeEra() string {
	return strings.Replace(vitestCase(), "    tests: [", "    change_install: [\"bun install --frozen-lockfile --filter @t3tools/shared\"]\n"+
		"    change_test_command: bun x vitest run\n    tests: [", 1)
}

func TestAtUsesTheChangeCommandsOnlyAtTheChangeAndItsParent(t *testing.T) {
	m, err := Parse(manifest(changeEra()))
	require.NoError(t, err)
	c := m.Cases[0]
	for _, sha := range []string{shaA, shaB} {
		at := c.At(sha)
		require.Equal(t, []string{"bun install --frozen-lockfile --filter @t3tools/shared"}, at.Install, sha)
		require.Equal(t, "bun x vitest run", at.TestCommand, sha)
		require.Equal(t, c.Sparse, at.Sparse, "the tree is the same set of files")
	}
	for _, sha := range []string{shaC, shaD} {
		at := c.At(sha)
		require.Equal(t, []string{"pnpm install --frozen-lockfile --filter @t3tools/shared..."}, at.Install, sha)
		require.Equal(t, "pnpm exec vp test run --dir .", at.TestCommand, sha)
	}
}

func TestAtKeepsTheCaseCommandsWhenTheCaseNamesNoChangeCommands(t *testing.T) {
	m, err := Parse(manifest(vitestCase()))
	require.NoError(t, err)
	c := m.Cases[0]
	require.Equal(t, c, c.At(shaA))
}

func TestAtReplacesOnlyTheFieldTheCaseNames(t *testing.T) {
	m, err := Parse(manifest(strings.Replace(vitestCase(), "    tests: [", "    change_test_command: bun x vitest run\n    tests: [", 1)))
	require.NoError(t, err)
	at := m.Cases[0].At(shaA)
	require.Equal(t, "bun x vitest run", at.TestCommand)
	require.Equal(t, m.Cases[0].Install, at.Install, "no change_install: the install steps stay")
}

func TestParseRejectsBrokenChangeCommands(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"empty change install step", strings.Replace(changeEra(), `["bun install --frozen-lockfile --filter @t3tools/shared"]`, `[""]`, 1), "change_install"},
		{"change command sets the reporter", strings.Replace(changeEra(), "bun x vitest run", "bun x vitest run --reporter=json", 1), "reporter"},
		{"cargo change command is not cargo test", strings.Replace(cargoCase(), "    tests: [", "    change_test_command: make test\n    tests: [", 1), "cargo test"},
		{"go case with change install", strings.Replace(bugCase(), "    timeout: 5m\n", "    timeout: 5m\n    change_install: [go mod download]\n", 1), "a go case"},
		{"clean case with change commands", strings.Replace(strings.Replace(strings.Replace(changeEra(), "kind: bug", "kind: clean", 1),
			"    fix: {sha: "+shaC+", parent: "+shaD+"}\n", "", 1), "    tests: [\"packages/shared/src/x.test.ts:outer > keeps the value\"]\n", "", 1),
			"a clean case runs at its change"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(manifest(tt.body))
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.want)
		})
	}
}
