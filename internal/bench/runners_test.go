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

func TestParseLibtestRefusesOutputThatDisagreesWithTheSummary(t *testing.T) {
	out := "running 2 tests\ntest a ... ok\ntest result: ok. 2 passed; 0 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.00s\n"
	_, err := ParseLibtest([]byte(out), nil)
	require.ErrorContains(t, err, "summary")
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
