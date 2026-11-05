package verify_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/verify"
)

func run(cases ...verify.Case) verify.Report {
	for i := range cases {
		if cases[i].Suite == "" {
			cases[i].Suite = "pkg"
		}
	}
	return verify.Report{Suites: []verify.Suite{{Name: "pkg", Cases: cases}}}
}

func c(name string, status verify.Status) verify.Case {
	return verify.Case{Name: name, Status: status}
}

func TestStabilityAllPass(t *testing.T) {
	runs := []verify.Report{
		run(c("TestBalance", verify.StatusPass)),
		run(c("TestBalance", verify.StatusPass)),
		run(c("TestBalance", verify.StatusPass)),
	}
	got, err := verify.Stability(runs)
	require.NoError(t, err)
	require.Equal(t, verify.FlakeReport{
		Runs:  verify.RequiredRuns,
		Tests: []verify.TestStability{{Suite: "pkg", Test: "TestBalance", Stability: verify.StablePass}},
	}, got)
	require.Empty(t, got.Rejected())
}

func TestStabilityAllFail(t *testing.T) {
	runs := []verify.Report{
		run(c("TestBalance", verify.StatusFail)),
		run(c("TestBalance", verify.StatusFail)),
		run(c("TestBalance", verify.StatusFail)),
	}
	got, err := verify.Stability(runs)
	require.NoError(t, err)
	require.Len(t, got.Tests, 1)
	require.Equal(t, verify.StableFail, got.Tests[0].Stability)
	require.Empty(t, got.Rejected())
}

// A test that passes twice and fails once is flaky, and a flaky test is rejected from the gate.
func TestStabilityFlakyIsRejected(t *testing.T) {
	runs := []verify.Report{
		run(c("TestBalance", verify.StatusPass)),
		run(c("TestBalance", verify.StatusPass)),
		run(c("TestBalance", verify.StatusFail)),
	}
	got, err := verify.Stability(runs)
	require.NoError(t, err)
	require.Equal(t, []verify.TestStability{{Suite: "pkg", Test: "TestBalance", Stability: verify.Flaky}}, got.Tests)
	require.Equal(t, []verify.TestStability{{Suite: "pkg", Test: "TestBalance", Stability: verify.Flaky}}, got.Rejected())
}

// Two different non-pass statuses also differ across runs, so the test is flaky rather than a stable failure.
func TestStabilityMixedFailureModesAreFlaky(t *testing.T) {
	runs := []verify.Report{
		run(c("TestBalance", verify.StatusFail)),
		run(c("TestBalance", verify.StatusError)),
		run(c("TestBalance", verify.StatusFail)),
	}
	got, err := verify.Stability(runs)
	require.NoError(t, err)
	require.Equal(t, verify.Flaky, got.Tests[0].Stability)
}

// A test that disappears from one run (a build broke that run) is unstable.
func TestStabilityMissingFromOneRunIsFlaky(t *testing.T) {
	runs := []verify.Report{
		run(c("TestBalance", verify.StatusPass), c("TestDeposit", verify.StatusPass)),
		run(c("TestBalance", verify.StatusPass)),
		run(c("TestBalance", verify.StatusPass), c("TestDeposit", verify.StatusPass)),
	}
	got, err := verify.Stability(runs)
	require.NoError(t, err)
	require.Equal(t, []verify.TestStability{
		{Suite: "pkg", Test: "TestBalance", Stability: verify.StablePass},
		{Suite: "pkg", Test: "TestDeposit", Stability: verify.Flaky},
	}, got.Tests)
	require.Equal(t, []verify.TestStability{{Suite: "pkg", Test: "TestDeposit", Stability: verify.Flaky}}, got.Rejected())
}

func TestStabilityNeedsExactlyThreeRuns(t *testing.T) {
	one := run(c("TestBalance", verify.StatusPass))
	for _, n := range []int{0, 1, 2, 4} {
		runs := make([]verify.Report, n)
		for i := range runs {
			runs[i] = one
		}
		_, err := verify.Stability(runs)
		require.ErrorIs(t, err, verify.ErrRunCount, "runs=%d", n)
		require.Contains(t, err.Error(), "exactly 3 runs")
	}
}

// Two packages can hold a test with the same name; they are judged apart.
func TestStabilityKeepsSuitesApart(t *testing.T) {
	multi := func(a, b verify.Status) verify.Report {
		return verify.Report{Suites: []verify.Suite{
			{Name: "refunds", Cases: []verify.Case{{Name: "TestRefund", Status: a}}},
			{Name: "ledger", Cases: []verify.Case{{Name: "TestRefund", Status: b}}},
		}}
	}
	runs := []verify.Report{
		multi(verify.StatusPass, verify.StatusPass),
		multi(verify.StatusPass, verify.StatusFail),
		multi(verify.StatusPass, verify.StatusPass),
	}
	got, err := verify.Stability(runs)
	require.NoError(t, err)
	require.Equal(t, []verify.TestStability{
		{Suite: "ledger", Test: "TestRefund", Stability: verify.Flaky},
		{Suite: "refunds", Test: "TestRefund", Stability: verify.StablePass},
	}, got.Tests)
}

// The output is sorted, so the order tests happen to appear in a report does not change the result.
func TestStabilityOutputIsSorted(t *testing.T) {
	runs := []verify.Report{
		run(c("TestC", verify.StatusPass), c("TestA", verify.StatusPass)),
		run(c("TestA", verify.StatusPass), c("TestC", verify.StatusPass)),
		run(c("TestC", verify.StatusPass), c("TestA", verify.StatusPass)),
	}
	got, err := verify.Stability(runs)
	require.NoError(t, err)
	require.Equal(t, []verify.TestStability{
		{Suite: "pkg", Test: "TestA", Stability: verify.StablePass},
		{Suite: "pkg", Test: "TestC", Stability: verify.StablePass},
	}, got.Tests)
}
