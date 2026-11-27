package agentloop

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// bigRead makes the read tool answer with 6000 bytes, so a few turns pass a small context budget.
func bigRead(h *harness) {
	h.read.run = func(json.RawMessage) (string, error) { return strings.Repeat("x", 6000), nil }
}

// readTexts are the tool results of the reads, without the submission's own "accepted".
func readTexts(res Result) []string {
	var out []string
	for _, text := range toolTexts(res) {
		if text != "accepted" {
			out = append(out, text)
		}
	}
	return out
}

func reads(n int) []modelStep {
	var steps []modelStep
	for i := range n {
		steps = append(steps, answer(0.001, call(fmt.Sprintf("c%d", i+1), "read_code",
			fmt.Sprintf(`{"path":"mock/mock.go","start_line":%d,"end_line":%d}`, i*10+1, i*10+10))))
	}
	return steps
}

func TestContextBudgetTrimsOldToolResultsAndKeepsTheLastThreeWhole(t *testing.T) {
	h := newHarness(t, append(reads(8), answer(0.001, call("s", "submit_plan", `{}`)))...)
	bigRead(h)
	h.cfg.ContextBytes = 30_000
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	require.Equal(t, StopSubmitted, res.Reason)
	for i, turn := range res.Turn {
		require.Positive(t, turn.RequestBytes, "turn %d records its request size", i+1)
		require.LessOrEqual(t, turn.RequestBytes, 30_000, "turn %d stays under the budget", i+1)
	}
	texts := readTexts(res)
	require.Len(t, texts, 8)
	for _, recent := range texts[5:] {
		require.Len(t, recent, 6000, "the last three tool results stay whole")
	}
	require.Equal(t, "[result of read_code mock/mock.go:1-10 trimmed, 5.9 KB]", texts[0])
	require.Positive(t, res.Turn[len(res.Turn)-1].TrimmedResults)
}

func TestContextBudgetKeepsRecentResultsWholeEvenOverTheBudget(t *testing.T) {
	// The budget is a target: the newest results are what the model works from, so they are never cut, and the
	// request may then pass the budget. Every older result is trimmed first.
	h := newHarness(t, append(reads(5), answer(0.001, call("s", "submit_plan", `{}`)))...)
	bigRead(h)
	h.cfg.ContextBytes = 1000
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	texts := readTexts(res)
	require.Len(t, texts, 5)
	require.True(t, strings.HasPrefix(texts[0], "[result of read_code"), texts[0])
	require.True(t, strings.HasPrefix(texts[1], "[result of read_code"), texts[1])
	for _, recent := range texts[2:] {
		require.Len(t, recent, 6000)
	}
	require.Greater(t, res.Turn[len(res.Turn)-1].RequestBytes, 1000, "the size is recorded as it was sent")
}

func TestTrimStubNamesTheCallAndTheSize(t *testing.T) {
	tests := []struct {
		name, tool, args string
		size             int
		want             string
	}{
		{"a read with lines", "read_code", `{"path":"mock/mock.go","start_line":200,"end_line":260}`, 4198,
			"[result of read_code mock/mock.go:200-260 trimmed, 4.1 KB]"},
		{"a read of a whole file", "read_code", `{"path":"a.go"}`, 2048, "[result of read_code a.go trimmed, 2.0 KB]"},
		{"a search", "search_code", `{"query":"m.mutex.Lock()","path_prefix":"mock/"}`, 512,
			`[result of search_code "m.mutex.Lock()" mock/ trimmed, 0.5 KB]`},
		{"a test run keeps the path and drops the source", "run_test",
			`{"test_path":"mock/x_test.go","test_source":"` + strings.Repeat("y", 500) + `"}`, 1024,
			"[result of run_test mock/x_test.go trimmed, 1.0 KB]"},
		{"no arguments", "list_changes", `{}`, 10240, "[result of list_changes trimmed, 10.0 KB]"},
		{"arguments that are not an object", "list_changes", `nope`, 1024, "[result of list_changes trimmed, 1.0 KB]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, trimStub(ports.ModelToolCall{Name: tt.tool, Arguments: tt.args}, tt.size))
		})
	}
}

func TestContextBudgetDoesNotChangeARequestAlreadySent(t *testing.T) {
	h := newHarness(t, append(reads(6), answer(0.001, call("s", "submit_plan", `{}`)))...)
	bigRead(h)
	h.cfg.ContextBytes = 20_000
	_, err := h.run(context.Background(), t)
	require.NoError(t, err)
	// The fourth request held the first result whole; a later trim must not reach back into it.
	fourth := h.model.requests[3].Messages
	var first string
	for _, m := range fourth {
		if m.Role == ports.ModelRoleTool {
			first = m.Content
			break
		}
	}
	require.Len(t, first, 6000)
}

func TestContextConfigRefusesNegativeValues(t *testing.T) {
	h := newHarness(t)
	h.cfg.ContextBytes = -1
	_, err := New(h.model, h.clock, h.cfg, []Tool{h.read}, []Submitter{h.sub})
	require.Error(t, err)
}

func TestContextBudgetNeverTrimsTheTaskOrTheAnswersOrTheirReasoning(t *testing.T) {
	// DeepSeek wants each answer's reasoning sent back whole (AM12d), and the task is what the model works on.
	long := ports.ModelReasoning{Text: strings.Repeat("r", 3000), Field: "reasoning_content"}
	steps := reads(6)
	for i := range steps {
		steps[i] = withReasoning(steps[i], long)
	}
	h := newHarness(t, append(steps, answer(0.001, call("s", "submit_plan", `{}`)))...)
	bigRead(h)
	h.cfg.ContextBytes = 10_000
	_, err := h.run(context.Background(), t)
	require.NoError(t, err)
	last := h.model.requests[len(h.model.requests)-1].Messages
	require.Equal(t, "go", last[0].Content)
	got := assistants(ports.ModelRequest{Messages: last})
	require.Len(t, got, 6)
	for _, a := range got {
		require.Equal(t, long, a.Reasoning)
		require.Len(t, a.ToolCalls, 1)
	}
	stubs := 0
	for _, m := range last {
		if m.Role == ports.ModelRoleTool && strings.HasPrefix(m.Content, "[result of") {
			stubs++
		}
	}
	require.Equal(t, 3, stubs, "only the tool results older than the last three are stubs")
}

func TestContextBudgetProtectsLatestListChangesAndRunTest(t *testing.T) {
	diffBody := strings.Repeat("d", 4000)
	testBody := strings.Repeat("t", 4000)
	readBody := strings.Repeat("r", 4000)
	steps := []modelStep{
		answer(0.001, call("c1", "list_changes", `{}`)),
		answer(0.001, call("c2", "run_test", `{"test_path":"a_test.go"}`)),
		answer(0.001, call("c3", "read_code", `{"path":"a.go"}`)),
		answer(0.001, call("c4", "read_code", `{"path":"b.go"}`)),
		answer(0.001, call("c5", "read_code", `{"path":"c.go"}`)),
		answer(0.001, call("c6", "read_code", `{"path":"d.go"}`)),
		answer(0.001, call("s", "submit_plan", `{}`)),
	}
	h := newHarness(t, steps...)
	h.read.run = func(json.RawMessage) (string, error) { return readBody, nil }
	listTool := &fakeTool{
		name: "list_changes",
		run:  func(json.RawMessage) (string, error) { return diffBody, nil },
	}
	testTool := &fakeTool{
		name: "run_test",
		run:  func(json.RawMessage) (string, error) { return testBody, nil },
	}
	h.cfg.ContextBytes = 10_000
	l, err := New(h.model, h.clock, h.cfg, []Tool{h.read, listTool, testTool}, []Submitter{h.sub})
	require.NoError(t, err)
	res, err := l.Run(context.Background(), []ports.ModelMessage{{Role: ports.ModelRoleUser, Content: "go"}})
	require.NoError(t, err)
	require.Equal(t, StopSubmitted, res.Reason)
	texts := readTexts(res)
	require.Len(t, texts, 6)
	require.Equal(t, diffBody, texts[0], "latest list_changes must stay whole")
	require.Equal(t, testBody, texts[1], "latest run_test must stay whole")
	require.True(t, strings.HasPrefix(texts[2], "[result of read_code a.go trimmed"), "older read_code is trimmed first: %s", texts[2])
}

func TestContextBudgetNeverTrimsAResultUnderTwoKB(t *testing.T) {
	// Live run 8 stubbed results of 0.1 to 0.3 KB: a stub that size saves nothing and hides what the result said.
	small := strings.Repeat("s", minTrimBytes-1)
	steps := []modelStep{answer(0.001, call("c0", "search_code", `{"query":"x"}`))}
	steps = append(steps, reads(6)...)
	h := newHarness(t, append(steps, answer(0.001, call("s", "submit_plan", `{}`)))...)
	search := &fakeTool{name: "search_code", run: func(json.RawMessage) (string, error) { return small, nil }}
	bigRead(h)
	h.cfg.ContextBytes = 24_000
	l, err := New(h.model, h.clock, h.cfg, []Tool{h.read, search}, []Submitter{h.sub})
	require.NoError(t, err)
	res, err := l.Run(context.Background(), []ports.ModelMessage{{Role: ports.ModelRoleUser, Content: "go"}})
	require.NoError(t, err)
	texts := readTexts(res)
	require.Len(t, texts, 7)
	require.Equal(t, small, texts[0], "a result under 2 KB stays whole")
	require.True(t, strings.HasPrefix(texts[1], "[result of read_code"), "the big reads are trimmed instead: %s", texts[1])
	for i, turn := range res.Turn {
		require.LessOrEqual(t, turn.RequestBytes, 24_000, "turn %d: the budget still holds", i+1)
	}
}

// testRunResult is a run_test result as the bench and the product write it (describeRun), padded past 2 KB.
func testRunResult(head, outcomes, output string) string {
	return head + "\n" + outcomes + "output:\n" + output + strings.Repeat("log line\n", 400)
}

func TestContextBudgetKeepsTheOutcomeOfATrimmedTestRun(t *testing.T) {
	results := []string{
		testRunResult("passed 2, failed 1, errored 0", "a > fails: failure\na > b: passed\na > c: passed\n",
			" FAIL  src/a.test.ts > a > fails\nAssertionError: expected 2 to be 3 // Object.is equality\n"),
		"error: install \"pnpm install --frozen-lockfile\": exit status 1: ERR_PNPM_NO_LOCKFILE Headless installation " +
			"requires a pnpm-lock.yaml file" + strings.Repeat(" more", 600),
		testRunResult("passed 3, failed 0, errored 0", "a: passed\nb: passed\nc: passed\n", "ok\n"),
		testRunResult("passed 0, failed 0, errored 1", "[build]: error\n", "src/x.rs:3:5: error[E0425]: cannot find value `y`\n"),
	}
	var steps []modelStep
	for i := range results {
		steps = append(steps, answer(0.001, call(fmt.Sprintf("t%d", i), "run_test",
			fmt.Sprintf(`{"test_path":"packages/x/t%d.test.ts","test_source":"x"}`, i))))
	}
	steps = append(steps, reads(4)...)
	h := newHarness(t, append(steps, answer(0.001, call("s", "submit_plan", `{}`)))...)
	n := 0
	runTest := &fakeTool{name: "run_test", run: func(json.RawMessage) (string, error) {
		n++
		return results[n-1], nil
	}}
	bigRead(h)
	h.cfg.ContextBytes = 26_000
	l, err := New(h.model, h.clock, h.cfg, []Tool{h.read, runTest}, []Submitter{h.sub})
	require.NoError(t, err)
	res, err := l.Run(context.Background(), []ports.ModelMessage{{Role: ports.ModelRoleUser, Content: "go"}})
	require.NoError(t, err)
	texts := readTexts(res)
	require.Len(t, texts, 8)
	require.Equal(t, "[run_test packages/x/t0.test.ts: FAIL 1 of 3 (AssertionError: expected 2 to be 3 // Object.is "+
		"equality); full output trimmed, 3.7 KB]", texts[0])
	require.True(t, strings.HasPrefix(texts[1], "[run_test packages/x/t1.test.ts: ERROR (install \"pnpm install "+
		"--frozen-lockfile\": exit status 1: ERR_PNPM_NO_LOCKFILE"), texts[1])
	require.Equal(t, "[run_test packages/x/t2.test.ts: PASS 3 of 3; full output trimmed, 3.6 KB]", texts[2])
	require.Equal(t, results[3], texts[3], "the latest run_test stays whole")
	for i, turn := range res.Turn {
		require.LessOrEqual(t, turn.RequestBytes, 26_000, "turn %d: the budget still holds", i+1)
	}
}

func TestTestOutcomeReadsTheResultHead(t *testing.T) {
	tests := []struct{ name, result, want string }{
		{"a pass", "passed 2, failed 0, errored 0\na: passed\noutput:\nok\n", "PASS 2 of 2"},
		{"a fail with its first error line", "passed 0, failed 1, errored 0\na: failure\noutput:\n\n  Error: boom\n  at x\n",
			"FAIL 1 of 1 (Error: boom)"},
		{"a panic", "passed 0, failed 1, errored 0\noutput:\nthread 'tests::a' panicked at src/a.rs:3:5:\n",
			"FAIL 1 of 1 (thread 'tests::a' panicked at src/a.rs:3:5:)"},
		{"a build error", "passed 0, failed 0, errored 1\n[build]: error\noutput:\nerror[E0425]: no y\n",
			"BUILD ERROR (error[E0425]: no y)"},
		{"no tests", "passed 0, failed 0, errored 0\noutput:\n", "NO TESTS RAN"},
		{"a tool error", "error: install \"bun install\": exit status 1", `ERROR (install "bun install": exit status 1)`},
		{"another shape", "something else\nmore", "something else"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, testOutcome(tt.result))
		})
	}
}
