package verify_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/verify"
)

// reports builds a one-suite JUnit report from cases, the way a sandbox run returns it.
func reports(cases ...verify.Case) verify.Report {
	for i := range cases {
		if cases[i].Suite == "" {
			cases[i].Suite = "pkg"
		}
	}
	return verify.Report{Suites: []verify.Suite{{Name: "pkg", Cases: cases}}}
}

func passed(name string) verify.Case {
	return verify.Case{Name: name, Status: verify.StatusPass}
}

func failed(name, message, text string) verify.Case {
	return verify.Case{Name: name, Status: verify.StatusFail, Message: message, Text: text}
}

// A test that fails on an assertion without the feature and passes with it is red-first.
func TestJudgeAcceptsAnAssertionRed(t *testing.T) {
	before := reports(failed("TestRefundPartial", "Failed", "expected: 500 actual: 200"))
	after := reports(passed("TestRefundPartial"))
	v := verify.Judge(before, after, map[string][]string{"AC1": {"TestRefundPartial"}})
	require.Equal(t, []string{"TestRefundPartial"}, v.Accepted)
	require.Empty(t, v.Rejected)
	require.Empty(t, v.Uncovered)
}

// A test that already passes without the feature proves nothing about the change.
func TestJudgeRejectsATestThatPassesWithoutTheFeature(t *testing.T) {
	v := verify.Judge(reports(passed("TestRefundPartial")), reports(passed("TestRefundPartial")), nil)
	require.Empty(t, v.Accepted)
	require.Equal(t, []verify.Rejection{{
		Test:   "TestRefundPartial",
		Reason: "TestRefundPartial passes without the feature",
	}}, v.Rejected)
}

// A compile error is an <error>, and it is never red-first: it does not show the behaviour is missing.
func TestJudgeRejectsACompileError(t *testing.T) {
	before := reports(verify.Case{
		Name: "TestPartialRefund", Status: verify.StatusError,
		Message: "Failed to compile refunds_test", Text: "refunds_test.go:9:2: undefined: NewRefund\n",
	})
	v := verify.Judge(before, reports(passed("TestPartialRefund")), nil)
	require.Empty(t, v.Accepted)
	require.Len(t, v.Rejected, 1)
	require.Equal(t, "TestPartialRefund", v.Rejected[0].Test)
	require.Contains(t, v.Rejected[0].Reason, "TestPartialRefund")
	require.Contains(t, v.Rejected[0].Reason, "compile or setup error")
	require.Contains(t, v.Rejected[0].Reason, "undefined: NewRefund")
}

// A <failure> whose text is a setup failure is rejected too, because it never reached an assertion.
func TestJudgeRejectsASetupFailureWearingAFailureTag(t *testing.T) {
	cases := map[string]verify.Case{
		"undefined":          failed("TestA", "Failed", "refunds_test.go:9:2: undefined: NewRefund"),
		"missing package":    failed("TestA", "Failed", "cannot find package \"github.com/x/y\" in any of:"),
		"missing module":     failed("TestA", "Failed", "no required module provides package github.com/x/y"),
		"build failed":       failed("TestA", "Failed", "FAIL github.com/x/refunds [build failed]"),
		"panic before setup": failed("TestA", "Failed", "panic: runtime error: invalid memory address or nil pointer dereference"),
		"in the message":     failed("TestA", "undefined: NewRefund", ""),
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			v := verify.Judge(reports(c), reports(passed("TestA")), nil)
			require.Empty(t, v.Accepted)
			require.Len(t, v.Rejected, 1)
			require.Contains(t, v.Rejected[0].Reason, "compile or setup error")
		})
	}
}

func TestJudgeRejectsASkippedTest(t *testing.T) {
	before := reports(verify.Case{Name: "TestA", Status: verify.StatusSkipped, Message: "short mode"})
	v := verify.Judge(before, reports(passed("TestA")), nil)
	require.Empty(t, v.Accepted)
	require.Equal(t, "TestA is skipped without the feature", v.Rejected[0].Reason)
}

// A test must appear in both runs: one that never ran without the feature cannot be red.
func TestJudgeRejectsATestMissingFromTheBaseRun(t *testing.T) {
	v := verify.Judge(reports(), reports(passed("TestA")), nil)
	require.Empty(t, v.Accepted)
	require.Equal(t, "TestA does not run without the feature", v.Rejected[0].Reason)
}

func TestJudgeRejectsATestMissingFromTheRunWithTheFeature(t *testing.T) {
	v := verify.Judge(reports(failed("TestA", "Failed", "boom")), reports(), nil)
	require.Empty(t, v.Accepted)
	require.Equal(t, "TestA does not run with the feature", v.Rejected[0].Reason)
}

// Red without green is not done: the test has to pass once the feature is there.
func TestJudgeRejectsATestThatIsStillRedWithTheFeature(t *testing.T) {
	v := verify.Judge(reports(failed("TestA", "Failed", "boom")), reports(failed("TestA", "Failed", "boom")), nil)
	require.Empty(t, v.Accepted)
	require.Equal(t, "TestA does not pass with the feature: status fail", v.Rejected[0].Reason)
}

func TestJudgeRejectsATestThatErrorsWithTheFeature(t *testing.T) {
	v := verify.Judge(reports(failed("TestA", "Failed", "boom")), reports(verify.Case{Name: "TestA", Status: verify.StatusError}), nil)
	require.Equal(t, "TestA does not pass with the feature: status error", v.Rejected[0].Reason)
}

// An acceptance criterion without an accepted test is listed, so the author sees which one to cover.
func TestJudgeListsUncoveredAcceptanceCriteria(t *testing.T) {
	before := reports(
		failed("TestRefundPartial", "Failed", "expected 500"),
		failed("TestRefundFull", "Failed", "expected 204"),
		passed("TestBalance"),
	)
	after := reports(passed("TestRefundPartial"), passed("TestRefundFull"), passed("TestBalance"))
	v := verify.Judge(before, after, map[string][]string{
		"AC1": {"TestRefundPartial"},
		"AC2": {"TestBalance"},
		"AC3": {"TestMissing"},
		"AC4": nil,
	})
	require.Equal(t, []string{"TestRefundFull", "TestRefundPartial"}, v.Accepted)
	require.Equal(t, []verify.Rejection{{Test: "TestBalance", Reason: "TestBalance passes without the feature"}}, v.Rejected)
	require.Equal(t, []string{"AC2", "AC3", "AC4"}, v.Uncovered)
}

// Output is sorted, so two runs over the same reports produce the same verdict regardless of map order.
func TestJudgeOutputIsDeterministic(t *testing.T) {
	before := reports(failed("TestB", "Failed", "b"), failed("TestA", "Failed", "a"))
	after := reports(passed("TestB"), passed("TestA"))
	first := verify.Judge(before, after, map[string][]string{"AC2": {"TestA"}, "AC1": {"TestB"}})
	second := verify.Judge(before, after, map[string][]string{"AC1": {"TestB"}, "AC2": {"TestA"}})
	require.Equal(t, first, second)
	require.Equal(t, []string{"TestA", "TestB"}, first.Accepted)
}

// A name that appears in two suites is accepted only when every run of it is an assertion failure and then passes.
func TestJudgeTreatsADuplicateNameConservatively(t *testing.T) {
	before := verify.Report{Suites: []verify.Suite{
		{Name: "a", Cases: []verify.Case{{Name: "TestA", Status: verify.StatusFail, Message: "Failed"}}},
		{Name: "b", Cases: []verify.Case{{Name: "TestA", Status: verify.StatusPass}}},
	}}
	after := reports(passed("TestA"))
	v := verify.Judge(before, after, nil)
	require.Empty(t, v.Accepted)
	require.Equal(t, "TestA passes without the feature", v.Rejected[0].Reason)
}

func TestErrorDetailNamesTheLineWithTheCause(t *testing.T) {
	c := verify.Case{Name: "TestX", Status: verify.StatusError, Text: "=== RUN   TestX\n--- FAIL: TestX (0.00s)\npanic: httptest: failed to listen on a port: listen tcp6 [::1]:0\n\ngoroutine 7 [running]:"}
	if got := verify.ErrorDetail(c); got != "panic: httptest: failed to listen on a port: listen tcp6 [::1]:0" {
		t.Fatalf("ErrorDetail = %q", got)
	}
	c = verify.Case{Name: "TestY", Status: verify.StatusError, Text: "./a_test.go:9:2: undefined: refunds.New"}
	if got := verify.ErrorDetail(c); got != "./a_test.go:9:2: undefined: refunds.New" {
		t.Fatalf("ErrorDetail = %q", got)
	}
}
