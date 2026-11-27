package bench

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// readSample reads a captured runner output from testdata/bench/samples (pingdotgg/t3code, captured 2026-11-21).
func readSample(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("../../testdata/bench/samples/" + name)
	require.NoError(t, err)
	return b
}

// TestParseLibtestPass: real cargo test output of native/resource-monitor with one test selected by --exact.
func TestParseLibtestPass(t *testing.T) {
	run, err := ParseLibtest(readSample(t, "cargo-pass.stdout"), readSample(t, "cargo-pass.stderr"))
	require.NoError(t, err)
	require.False(t, run.BuildFailed)
	require.Equal(t, map[string]TestResult{"tests::keeps_command_lines_and_memory_out_of_the_discovery_table": ResultPass}, run.Tests)
}

// TestParseLibtestFailAndIgnored: real output with a failing test, an ignored test and 18 passing tests.
func TestParseLibtestFailAndIgnored(t *testing.T) {
	run, err := ParseLibtest(readSample(t, "cargo-fail.stdout"), readSample(t, "cargo-fail.stderr"))
	require.NoError(t, err)
	require.False(t, run.BuildFailed)
	require.Len(t, run.Tests, 20)
	require.Equal(t, ResultFail, run.Tests["tests::heimdall_sample_fails"])
	require.Equal(t, ResultSkip, run.Tests["tests::heimdall_sample_ignored"])
	require.Equal(t, ResultPass, run.Tests["tests::clamps_sample_interval"])
	require.Contains(t, run.Messages["tests::heimdall_sample_fails"], "sample failure")
}

// TestParseLibtestCompileErrorIsABuildFailure: a compile error is a build error, never a failing test.
func TestParseLibtestCompileErrorIsABuildFailure(t *testing.T) {
	run, err := ParseLibtest(readSample(t, "cargo-build.stdout"), readSample(t, "cargo-build.stderr"))
	require.NoError(t, err)
	require.True(t, run.BuildFailed)
	require.Empty(t, run.Tests)
	require.Contains(t, run.BuildOutput, "error[E0308]: mismatched types")
	require.Contains(t, run.BuildOutput, "could not compile")
}

func TestParseLibtestSumsSeveralBinariesAndKeepsTheWorstOfADuplicate(t *testing.T) {
	out := `
running 2 tests
test a::one ... ok
test a::two ... ignored, needs a display
test result: ok. 1 passed; 0 failed; 1 ignored; 0 measured; 0 filtered out; finished in 0.00s

running 2 tests
test a::one ... FAILED
test b - doc (line 3) ... ok
test result: FAILED. 1 passed; 1 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.01s
`
	run, err := ParseLibtest([]byte(out), nil)
	require.NoError(t, err)
	require.Equal(t, map[string]TestResult{"a::one": ResultFail, "a::two": ResultSkip, "b - doc (line 3)": ResultPass}, run.Tests)
}

func TestParseLibtestRefusesOutputThatDisagreesWithTheSummary(t *testing.T) {
	out := "running 2 tests\ntest a ... ok\ntest result: ok. 2 passed; 0 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.00s\n"
	_, err := ParseLibtest([]byte(out), nil)
	require.ErrorContains(t, err, "summary")
}

func TestParseLibtestWithNoTestRunAndNoCompileErrorIsAnError(t *testing.T) {
	_, err := ParseLibtest(nil, []byte("error: could not find `Cargo.toml` in `/x` or any parent directory\n"))
	require.ErrorContains(t, err, "no test binary ran")
}

// TestParseVitestJSONPassFailSkipAndALoadFailure: real Vite+ (vitest 5.0.5) JSON reporter output for two files. One
// file does not load (a missing module): that is a build error. The other has nested describes, a fail and a skip.
func TestParseVitestJSONPassFailSkipAndALoadFailure(t *testing.T) {
	run, err := ParseVitestJSON(readSample(t, "vitest-mixed.json"), "/private/var/folders/xx/T/t3wt-shared")
	require.NoError(t, err)
	require.True(t, run.BuildFailed)
	require.Contains(t, run.BuildOutput, "packages/shared/src/heimdallBroken.test.ts: Cannot find module './doesNotExist.ts'")
	file := "packages/shared/src/heimdallSample.test.ts:"
	require.Equal(t, map[string]TestResult{
		file + "outer > inner > passes": ResultPass,
		file + "outer > fails":          ResultFail,
		file + "outer > is skipped":     ResultSkip,
		file + "top level passes":       ResultPass,
	}, run.Tests)
	require.Contains(t, run.Messages[file+"outer > fails"], "AssertionError: expected 1 to be 2")
}

func TestParseVitestJSONKeepsAPathOutsideTheRootWhole(t *testing.T) {
	run, err := ParseVitestJSON(readSample(t, "vitest-mixed.json"), "/elsewhere")
	require.NoError(t, err)
	require.Contains(t, run.Tests, "/private/var/folders/xx/T/t3wt-shared/packages/shared/src/heimdallSample.test.ts:top level passes")
}

func TestParseVitestJSONRefusesBrokenJSON(t *testing.T) {
	_, err := ParseVitestJSON([]byte(`{"testResults":[`), "/x")
	require.ErrorContains(t, err, "vitest json")
}

func TestParseVitestJSONFailsAnEachCaseThatFailsOnce(t *testing.T) {
	out := `{"testResults":[{"name":"/r/a.test.ts","status":"failed","message":"","assertionResults":[
		{"ancestorTitles":[],"title":"case","status":"passed","failureMessages":[]},
		{"ancestorTitles":[],"title":"case","status":"failed","failureMessages":["boom"]},
		{"ancestorTitles":[],"title":"todo","status":"todo","failureMessages":[]}]}]}`
	run, err := ParseVitestJSON([]byte(out), "/r")
	require.NoError(t, err)
	require.Equal(t, map[string]TestResult{"a.test.ts:case": ResultFail, "a.test.ts:todo": ResultSkip}, run.Tests)
}
