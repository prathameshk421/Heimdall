package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// manyPassed is n passed tests named so that the last one sorts after every other name.
func manyPassed(n int, focus string) ports.SandboxResult {
	res := ports.SandboxResult{Passed: n + 1}
	for i := range n {
		res.Tests = append(res.Tests, ports.TestResult{Name: fmt.Sprintf("TestA%03d", i), Outcome: ports.OutcomePassed})
	}
	res.Tests = append(res.Tests, ports.TestResult{Name: focus, Outcome: ports.OutcomePassed})
	return res
}

func TestDescribeRunShowsTheTestTheReviewerWrote(t *testing.T) {
	// A package with more tests than the listing takes must still show the test that was just written. Otherwise the
	// reviewer cannot tell whether its own test passed, and a clean case looks like a failed run.
	res := manyPassed(maxTestsListed+20, "TestHeimdallBenchZzzProposed")
	out := describeRun(res, []string{"TestHeimdallBenchZzzProposed"})
	require.Contains(t, out, "TestHeimdallBenchZzzProposed: passed")
	require.Contains(t, out, "[", "the rest of the list is still cut, and the cut says so")
}

func TestDescribeRunListsFailingAndErroredTestsFirst(t *testing.T) {
	res := ports.SandboxResult{
		Passed: 1, Failed: 1, Errored: 1,
		Tests: []ports.TestResult{
			{Name: "TestPass", Outcome: ports.OutcomePassed},
			{Name: "TestFail", Outcome: ports.OutcomeFailure},
			{Name: "TestError", Outcome: ports.OutcomeError},
		},
	}
	out := describeRun(res, nil)
	lines := strings.Split(out, "\n")
	require.Less(t, strings.Index(out, "TestError: error"), strings.Index(out, "TestFail: failure"))
	require.Less(t, strings.Index(out, "TestFail: failure"), strings.Index(out, "TestPass: passed"))
	require.Contains(t, lines[0], "passed 1, failed 1, errored 1")
}
