//go:build benchreplay

// The replay tests run the whole review pipeline on real cases from the manifest with a scripted model, so the harness
// is proven before any model spend. They fetch real repositories and run go test, so they need the network and are
// behind the benchreplay tag:
//
//	CGO_ENABLED=0 go test -tags benchreplay -count=1 -v -run TestReplay ./cmd/heimdall-bench -replay.dir DIR
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/bench"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

var replayDir = flag.String("replay.dir", "", "keep the replay results (JSON and Markdown) in this directory")

// script is a recorded tool-call sequence for one case (testdata/bench/scripts).
type script struct {
	Case  string `json:"case"`
	Turns []struct {
		Calls []struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		} `json:"calls"`
		Usage struct {
			Input  int64 `json:"input_tokens"`
			Cached int64 `json:"cached_input_tokens"`
			Output int64 `json:"output_tokens"`
		} `json:"usage"`
		CostUSD float64 `json:"cost_usd"`
		// Expect must appear in the tool results of the previous answer, so a script proves the tools work.
		Expect string `json:"expect"`
	} `json:"turns"`
}

// scriptedModel plays one script per case, in case order. A conversation with no assistant message starts the next
// script. It answers with an error when a tool result lacks what the script expects.
type scriptedModel struct {
	scripts []script
	cur     int
	turn    int
}

func loadScripts(t *testing.T, ids ...string) *scriptedModel {
	t.Helper()
	m := &scriptedModel{cur: -1}
	for _, id := range ids {
		b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "bench", "scripts", id+".json"))
		require.NoError(t, err)
		var s script
		require.NoError(t, json.Unmarshal(b, &s))
		require.Equal(t, id, s.Case)
		m.scripts = append(m.scripts, s)
	}
	return m
}

func (m *scriptedModel) Complete(_ context.Context, req ports.ModelRequest) (ports.ModelResponse, error) {
	if !hasAssistant(req.Messages) {
		m.cur, m.turn = m.cur+1, 0
	}
	if m.cur >= len(m.scripts) || m.turn >= len(m.scripts[m.cur].Turns) {
		return ports.ModelResponse{}, errors.New("scripted model: no recorded turn left")
	}
	s := m.scripts[m.cur]
	turn := s.Turns[m.turn]
	m.turn++
	if turn.Expect != "" && !strings.Contains(lastToolResults(req.Messages), turn.Expect) {
		return ports.ModelResponse{}, fmt.Errorf("scripted model %s turn %d: the tool results lack %q:\n%s",
			s.Case, m.turn, turn.Expect, lastToolResults(req.Messages))
	}
	resp := ports.ModelResponse{Finish: ports.ModelFinishToolCalls, CostUSD: turn.CostUSD,
		Usage: ports.ModelUsage{InputTokens: turn.Usage.Input, CachedInputTokens: turn.Usage.Cached, OutputTokens: turn.Usage.Output}}
	for i, c := range turn.Calls {
		resp.ToolCalls = append(resp.ToolCalls, ports.ModelToolCall{ID: fmt.Sprintf("%s-%d-%d", s.Case, m.turn, i),
			Name: c.Name, Arguments: string(c.Arguments)})
	}
	return resp, nil
}

func hasAssistant(msgs []ports.ModelMessage) bool {
	for _, m := range msgs {
		if m.Role == ports.ModelRoleAssistant {
			return true
		}
	}
	return false
}

// lastToolResults joins the tool messages after the last assistant message.
func lastToolResults(msgs []ports.ModelMessage) string {
	var parts []string
	for i := len(msgs) - 1; i >= 0 && msgs[i].Role == ports.ModelRoleTool; i-- {
		parts = append([]string{msgs[i].Content}, parts...)
	}
	return strings.Join(parts, "\n---\n")
}

func replayOptions(t *testing.T, only string, maxCost float64) reviewOptions {
	t.Helper()
	dir := *replayDir
	if dir == "" {
		dir = t.TempDir()
	}
	require.NoError(t, os.MkdirAll(dir, 0o750))
	md := filepath.Join(dir, "review.md")
	require.NoError(t, os.WriteFile(md, nil, 0o600))
	return reviewOptions{
		manifest: filepath.Join("..", "..", "testdata", "bench", "cases.yaml"),
		cache:    filepath.Join(os.TempDir(), "heimdall-bench"),
		out:      filepath.Join(dir, "review.json"), md: md, only: only,
		model: "scripted (testdata/bench/scripts)", price: bench.Price{InputPerM: 0.09, CachedInputPerM: 0.09, OutputPerM: 0.18},
		maxCost: maxCost, perCase: 0.50, blockAt: finding.SeverityMedium,
		commandFor: "go test -tags benchreplay -run " + t.Name() + " ./cmd/heimdall-bench",
	}
}

func readReport(t *testing.T, path string) reviewReport {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	var rep reviewReport
	require.NoError(t, json.Unmarshal(b, &rep))
	return rep
}

func TestReplayTwoRealCasesWithAScriptedModel(t *testing.T) {
	o := replayOptions(t, "testify-1207,cobra-1643-clean", testingCapUSD)
	model := loadScripts(t, "testify-1207", "cobra-1643-clean")
	code, err := reviewRun(context.Background(), o, execRunner{}, model, testWriter{t})
	require.NoError(t, err)
	require.Equal(t, exitAgrees, code)

	rep := readReport(t, o.out)
	require.Len(t, rep.Cases, 2)
	bug, clean := rep.Cases[0], rep.Cases[1]

	require.Equal(t, "testify-1207", bug.ID)
	require.True(t, bug.Scored, bug.Error)
	require.Equal(t, "submitted", string(bug.Stop))
	require.Equal(t, 1, bug.Proposed)
	require.Equal(t, 1, bug.Proven, "the nil mutex test fails twice on the head and passes on the fix")
	require.True(t, bug.Hit, "mock/mock.go 507-511 overlaps the truth 510-511 on the head")
	require.Len(t, bug.Findings[0].Runs, 3)
	require.NotNil(t, bug.Truth)

	require.Equal(t, "cobra-1643-clean", clean.ID)
	require.True(t, clean.Scored, clean.Error)
	require.Equal(t, 2, clean.Proposed)
	require.Zero(t, clean.Proven)
	require.False(t, clean.FalsePositive)
	require.Equal(t, map[string]int{"passes_on_head": 1, "build_error_head": 1}, clean.Unproven)

	s := rep.Summary
	require.Equal(t, bench.Ratio{Num: 1, Den: 1}, s.Recall)
	require.Equal(t, bench.Ratio{Num: 0, Den: 1}, s.FalsePositives)
	require.Equal(t, bench.Ratio{Num: 1, Den: 3}, s.Evidence)
	require.Positive(t, s.CostUSD)
	require.Equal(t, sandboxNote, rep.Sandbox)

	md, err := os.ReadFile(o.md)
	require.NoError(t, err)
	require.Contains(t, string(md), "| testify-1207 | bug | yes | submitted |")
	t.Log("\n" + string(md))
}

func TestReplayStopsAtTheCostCap(t *testing.T) {
	// The first recorded answers of testify-1207 cost about $0.0002 each, and each call's bound is larger than that,
	// so a $0.001 cap stops the first case after a few calls and the second case never runs.
	o := replayOptions(t, "testify-1207,cobra-1643-clean", 0.001)
	o.out = filepath.Join(t.TempDir(), "capped.json")
	o.md = ""
	model := loadScripts(t, "testify-1207", "cobra-1643-clean")
	code, err := reviewRun(context.Background(), o, execRunner{}, model, testWriter{t})
	require.NoError(t, err)
	require.Equal(t, exitDisagree, code)

	rep := readReport(t, o.out)
	require.Equal(t, "cost cap", rep.Stopped)
	require.False(t, rep.Cases[0].Scored)
	require.Contains(t, rep.Cases[0].Error, "cost cap")
	require.Equal(t, "not run: cost cap", rep.Cases[1].Error)
	require.LessOrEqual(t, rep.Summary.CostUSD, 0.001, "the spend never passes the cap")
}

// testWriter sends the progress lines to the test log.
type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

var _ io.Writer = testWriter{}
