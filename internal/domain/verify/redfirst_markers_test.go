package verify_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/verify"
)

// These are the other ways a Go test run says "I never reached an assertion". Each one wearing a failure tag must
// not count as a red test (TECH_SPEC 12.1).
func TestJudgeRejectsMoreSetupFailuresWearingAFailureTag(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{"syntax error", "./a_test.go:9:2: syntax error: unexpected newline, expected comma"},
		{"unused variable", "./a_test.go:12:2: declared and not used: got"},
		{"unused import", "./a_test.go:4:2: \"fmt\" imported and not used"},
		{"setup failed", "FAIL example.com/m [setup failed]"},
		{"missing module", "cannot find module providing package example.com/other"},
		{"no test files", "no non-test Go files in /work/pkg"},
		{"wrong type", "./a_test.go:20:15: cannot use \"x\" (untyped string constant) as int value in argument to f"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := reports(failed("TestNew", "build output", c.text))
			after := reports(passed("TestNew"))
			v := verify.Judge(before, after, nil)
			require.Empty(t, v.Accepted, "a setup failure must not be accepted as red-first")
			require.Len(t, v.Rejected, 1)
			require.Contains(t, v.Rejected[0].Reason, "compile or setup error")
		})
	}
}

// A real assertion failure that happens to mention an ordinary word stays red-first.
func TestJudgeStillAcceptsARealAssertionFailure(t *testing.T) {
	before := reports(failed("TestRefund", "Not equal", "expected: 100\nactual  : 150\nrefund exceeds the payment"))
	after := reports(passed("TestRefund"))
	v := verify.Judge(before, after, nil)
	require.Equal(t, []string{"TestRefund"}, v.Accepted)
	require.Empty(t, v.Rejected)
}
