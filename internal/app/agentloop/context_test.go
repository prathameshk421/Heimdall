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

func TestNoContextBudgetTrimsNothing(t *testing.T) {
	h := newHarness(t, append(reads(5), answer(0.001, call("s", "submit_plan", `{}`)))...)
	bigRead(h)
	res, err := h.run(context.Background(), t)
	require.NoError(t, err)
	for _, text := range readTexts(res) {
		require.Len(t, text, 6000)
	}
	require.Positive(t, res.Turn[0].RequestBytes, "the size is recorded with no budget too")
	require.Zero(t, res.Turn[len(res.Turn)-1].TrimmedResults)
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
