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

// TestStability judges three runs of the same tests: stable pass, stable fail, or flaky (any difference across runs,
// including a test that disappears). A flaky test is rejected from the gate. Suites are judged apart and the output
// is sorted.
func TestStability(t *testing.T) {
	t.Parallel()
	balance := func(ss ...verify.Status) []verify.Report {
		runs := make([]verify.Report, 0, len(ss))
		for _, s := range ss {
			runs = append(runs, run(c("TestBalance", s)))
		}
		return runs
	}
	pass, fail, err := verify.StatusPass, verify.StatusFail, verify.StatusError
	stable := func(s verify.Outcome) []verify.TestStability {
		return []verify.TestStability{{Suite: "pkg", Test: "TestBalance", Stability: s}}
	}
	flaky := []verify.TestStability{{Suite: "pkg", Test: "TestBalance", Stability: verify.Flaky}}

	multi := func(a, b verify.Status) verify.Report {
		return verify.Report{Suites: []verify.Suite{
			{Name: "refunds", Cases: []verify.Case{{Name: "TestRefund", Status: a}}},
			{Name: "ledger", Cases: []verify.Case{{Name: "TestRefund", Status: b}}},
		}}
	}

	cases := []struct {
		name     string
		runs     []verify.Report
		want     []verify.TestStability
		rejected []verify.TestStability
	}{
		{"all pass", balance(pass, pass, pass), stable(verify.StablePass), nil},
		{"all fail", balance(fail, fail, fail), stable(verify.StableFail), nil},
		{"passes twice and fails once", balance(pass, pass, fail), flaky, flaky},
		{"mixed failure modes", balance(fail, err, fail), flaky, flaky},
		{"missing from one run", []verify.Report{
			run(c("TestBalance", pass), c("TestDeposit", pass)),
			run(c("TestBalance", pass)),
			run(c("TestBalance", pass), c("TestDeposit", pass)),
		}, []verify.TestStability{
			{Suite: "pkg", Test: "TestBalance", Stability: verify.StablePass},
			{Suite: "pkg", Test: "TestDeposit", Stability: verify.Flaky},
		}, []verify.TestStability{{Suite: "pkg", Test: "TestDeposit", Stability: verify.Flaky}}},
		{"same name in two suites is judged apart", []verify.Report{multi(pass, pass), multi(pass, fail), multi(pass, pass)},
			[]verify.TestStability{
				{Suite: "ledger", Test: "TestRefund", Stability: verify.Flaky},
				{Suite: "refunds", Test: "TestRefund", Stability: verify.StablePass},
			}, []verify.TestStability{{Suite: "ledger", Test: "TestRefund", Stability: verify.Flaky}}},
		{"output is sorted whatever the report order", []verify.Report{
			run(c("TestC", pass), c("TestA", pass)),
			run(c("TestA", pass), c("TestC", pass)),
			run(c("TestC", pass), c("TestA", pass)),
		}, []verify.TestStability{
			{Suite: "pkg", Test: "TestA", Stability: verify.StablePass},
			{Suite: "pkg", Test: "TestC", Stability: verify.StablePass},
		}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := verify.Stability(tc.runs)
			require.NoError(t, err)
			require.Equal(t, tc.want, got.Tests)
			require.Equal(t, tc.rejected, got.Rejected())
		})
	}
}

func TestStabilityNeedsExactlyThreeRuns(t *testing.T) {
	t.Parallel()
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
