package bench

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParseGoTestJSONPassFailAndSubtests: real go test -json output (Go 1.27) of a package with a passing test, a
// failing test and a test whose subtest fails. Subtests roll up into their top-level test.
func TestParseGoTestJSONPassFailAndSubtests(t *testing.T) {
	run, err := ParseGoTestJSON(readFixture(t, "gotest-fail.jsonl"))
	require.NoError(t, err)
	require.False(t, run.BuildFailed)
	require.Equal(t, map[string]TestResult{"TestOK": ResultPass, "TestBad": ResultFail, "TestSub": ResultFail}, run.Tests)
}

// TestParseGoTestJSONBuildFailure: real output when the test file does not compile.
func TestParseGoTestJSONBuildFailure(t *testing.T) {
	run, err := ParseGoTestJSON(readFixture(t, "gotest-buildfail.jsonl"))
	require.NoError(t, err)
	require.True(t, run.BuildFailed)
	require.Contains(t, run.BuildOutput, "undefined: Missing")
	require.Empty(t, run.Tests)
}

func TestParseGoTestJSONSkipsNoiseAndMarksUnfinishedTestsFailed(t *testing.T) {
	out := `go: downloading github.com/x/y v1.0.0
{"Action":"run","Package":"p","Test":"TestHang"}
{"Action":"run","Package":"p","Test":"TestSkip"}
{"Action":"skip","Package":"p","Test":"TestSkip"}
{"Action":"output","Package":"p","Output":"panic: test timed out after 1m0s\n"}
{"Action":"fail","Package":"p"}
`
	run, err := ParseGoTestJSON([]byte(out))
	require.NoError(t, err)
	require.Equal(t, map[string]TestResult{"TestHang": ResultFail, "TestSkip": ResultSkip}, run.Tests)
}

func TestParseGoTestJSONRejectsBrokenJSON(t *testing.T) {
	_, err := ParseGoTestJSON([]byte(`{"Action":"run",` + "\n"))
	require.ErrorContains(t, err, "go test -json line 1")
}

// TestTestFuncsOfARealFix: the lines stretchr/testify#1207 added to mock/mock_test.go sit in one test.
func TestTestFuncsOfARealFix(t *testing.T) {
	names, err := TestFuncs(readFixture(t, "testify-1207-mock_test.go.txt"), []int{918, 928, 929})
	require.NoError(t, err)
	require.Equal(t, []string{"Test_AssertExpectationsForObjects_Helper"}, names)
}

func TestTestFuncsOnlyNamesTopLevelTests(t *testing.T) {
	src := `package x

import "testing"

func helper() int {
	return 1
}

func TestA(t *testing.T) {
	_ = helper()
}

func Testlower(t *testing.T) {}

func (s *suite) TestMethod(t *testing.T) {}

func BenchmarkB(b *testing.B) {}

func TestB(t *testing.T) {
	t.Run("sub", func(t *testing.T) {})
}

func Test(t *testing.T) {}
`
	names, err := TestFuncs([]byte(src), []int{6, 10, 13, 15, 17, 20, 23})
	require.NoError(t, err)
	require.Equal(t, []string{"TestA", "TestB", "Test"}, names)

	names, err = TestFuncs([]byte(src), []int{1, 6})
	require.NoError(t, err)
	require.Empty(t, names)

	_, err = TestFuncs([]byte("package x\nfunc {"), []int{1})
	require.Error(t, err)
}

func TestRunPatternQuotesNames(t *testing.T) {
	require.Equal(t, `^(TestA|Test_B)$`, RunPattern([]string{"TestA", "Test_B"}))
}
