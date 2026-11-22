//go:build benchreplay

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/bench"
)

// The reviewer's run_test on real t3code cases, through the review path (openCase, the replay's tools and the local
// sandbox): install must succeed and the test must report pass or fail. Live run 8 met the opposite: every run_test on
// t3code-32169 failed at install, because its head used bun and the case installed with pnpm.

// reviewTool opens the case for review and returns its run_test tool.
func reviewTool(t *testing.T, id string) (*replay, fnTool) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "bench", "cases.yaml"))
	require.NoError(t, err)
	m, err := bench.Parse(b)
	require.NoError(t, err)
	var c bench.Case
	for _, mc := range m.Cases {
		if mc.ID == id {
			c = mc
		}
	}
	require.Equal(t, id, c.ID, "the manifest has the case")
	ctx := context.Background()
	rep, err := openCase(ctx, newRepoSet(), execRunner{}, filepath.Join(os.TempDir(), "heimdall-bench"), nil, c)
	require.NoError(t, err)
	t.Cleanup(func() { rep.close(context.WithoutCancel(ctx)) })
	for _, tool := range rep.tools(nil) {
		if tool.spec.Name == "run_test" {
			return rep, tool
		}
	}
	t.Fatal("the review has no run_test tool")
	return nil, fnTool{}
}

func callRunTest(t *testing.T, tool fnTool, path, source string) string {
	t.Helper()
	args, err := json.Marshal(map[string]string{"test_path": path, "test_source": source})
	require.NoError(t, err)
	out, err := tool.run(context.Background(), args)
	require.NoError(t, err, "run_test must install and run, not fail at install")
	return out
}

func TestReviewRunTestInstallsAndRunsOnABunEraVitestHead(t *testing.T) {
	rep, tool := reviewTool(t, "t3code-32169")
	require.NotEmpty(t, rep.c.ChangeInstall, "the head of this case builds with bun, not with the fix's pnpm")
	out := callRunTest(t, tool, "packages/shared/src/heimdallBenchProbe.test.ts", `import { describe, expect, it } from "vitest";

import { compareSemverVersions } from "./semver.ts";

describe("heimdall bench probe", () => {
  it("orders a prerelease before its stable version", () => {
    expect(compareSemverVersions("1.0.0-beta.1", "1.0.0")).toBeLessThan(0);
  });
  it("fails on purpose", () => {
    expect(1).toBe(2);
  });
});
`)
	t.Log(out)
	require.True(t, strings.HasPrefix(out, "passed 1, failed 1, errored 0\n"), out)
	require.Contains(t, out, "heimdall bench probe > orders a prerelease before its stable version: passed")
	require.Contains(t, out, "heimdall bench probe > fails on purpose: failure")
}

func TestReviewRunTestInstallsAndRunsOnACargoHead(t *testing.T) {
	_, tool := reviewTool(t, "t3code-19745")
	out := callRunTest(t, tool, "native/resource-monitor/src/heimdall_bench_probe.rs", `#[test]
fn adds() {
    assert_eq!(1 + 1, 2);
}

#[test]
fn fails_on_purpose() {
    assert_eq!(1, 2);
}
`)
	t.Log(out)
	require.True(t, strings.HasPrefix(out, "passed 1, failed 1, errored 0\n"), out)
}
