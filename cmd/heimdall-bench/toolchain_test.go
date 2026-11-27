package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/bench"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

func vitestReviewCase() bench.Case {
	return bench.Case{ID: "ts", Language: "typescript", Toolchain: bench.ToolchainVitest, Workdir: "packages/shared"}
}

func cargoReviewCase() bench.Case {
	return bench.Case{ID: "rs", Language: "rust", Toolchain: bench.ToolchainCargo, Workdir: "native/resource-monitor"}
}

func TestReviewTestNameMatchesTheSandboxNames(t *testing.T) {
	src := "#[test]\nfn finds_the_bug() {}\n"
	name, err := reviewTestName(cargoReviewCase(), "native/resource-monitor/src/heimdall_review.rs", src, "finds_the_bug")
	require.NoError(t, err)
	require.Equal(t, "heimdall_review::finds_the_bug", name, "libtest names a test by its module path")
	name, err = reviewTestName(cargoReviewCase(), "native/resource-monitor/src/heimdall_review.rs", src, "heimdall_review::finds_the_bug")
	require.NoError(t, err)
	require.Equal(t, "heimdall_review::finds_the_bug", name)
	_, err = reviewTestName(cargoReviewCase(), "native/resource-monitor/src/heimdall_review.rs", src, "missing")
	require.ErrorContains(t, err, "missing")

	ts := `describe("tokenizer", () => { it("keeps a value", () => {}); });`
	name, err = reviewTestName(vitestReviewCase(), "packages/shared/src/r.test.ts", ts, "tokenizer > keeps a value")
	require.NoError(t, err)
	require.Equal(t, "tokenizer > keeps a value", name)
	_, err = reviewTestName(vitestReviewCase(), "packages/shared/src/r.test.ts", ts, "tokenizer > other")
	require.ErrorContains(t, err, "other")
}

func TestToolSandboxResultNamesTestsWithoutTheFile(t *testing.T) {
	// The reviewer runs one vitest file, so the result names each test by its describe path, as the reviewer wrote it.
	run := bench.Run{Tests: map[string]bench.TestResult{
		"packages/shared/src/r.test.ts:tokenizer > keeps a value": bench.ResultFail,
		"packages/shared/src/r.test.ts:tokenizer > ok":            bench.ResultPass,
	}}
	res := sandboxResult(stripTestFiles(bench.ToolchainVitest, run))
	names := map[string]ports.Outcome{}
	for _, t := range res.Tests {
		names[t.Name] = t.Outcome
	}
	require.Equal(t, map[string]ports.Outcome{"tokenizer > keeps a value": ports.OutcomeFailure, "tokenizer > ok": ports.OutcomePassed}, names)
}

func TestToolSandboxResultKeepsCargoNames(t *testing.T) {
	// A libtest name holds "::", which is not a vitest file prefix. The replay of t3code-19745 caught this.
	run := bench.Run{Tests: map[string]bench.TestResult{"heimdall_review::finds_the_bug": bench.ResultFail}}
	got := stripTestFiles(bench.ToolchainCargo, run)
	require.Equal(t, map[string]bench.TestResult{"heimdall_review::finds_the_bug": bench.ResultFail}, got.Tests)
}
