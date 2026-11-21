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
		{"go case with tests", strings.Replace(bugCase(), "    timeout: 5m\n", "    timeout: 5m\n    tests: [TestX]\n", 1), "a go case"},
		{"go case with workdir", strings.Replace(bugCase(), "    timeout: 5m\n", "    timeout: 5m\n    workdir: mock\n", 1), "a go case"},
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
