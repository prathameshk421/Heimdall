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
	"sync"
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

// scriptedModel plays one script per case. It picks the script by the repository slug in the conversation, so several
// cases can run at once: each case's own conversation drives its own script. It answers with an error when a tool
// result lacks what the script expects.
type scriptedModel struct {
	mu      sync.Mutex
	scripts []script
	byRepo  map[string]int // repository slug to script index
	turn    map[int]int    // script index to the turns already played
}

func loadScripts(t *testing.T, ids ...string) *scriptedModel {
	t.Helper()
	m, err := loadManifest(filepath.Join("..", "..", "testdata", "bench", "cases.yaml"))
	require.NoError(t, err)
	slug := map[string]string{}
	for _, c := range m.Cases {
		slug[c.ID] = c.RepoSlug()
	}
	sm := &scriptedModel{byRepo: map[string]int{}, turn: map[int]int{}}
	for i, id := range ids {
		b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "bench", "scripts", id+".json"))
		require.NoError(t, err)
		var s script
		require.NoError(t, json.Unmarshal(b, &s))
		require.Equal(t, id, s.Case)
		require.NotEmpty(t, slug[id], "the manifest names %s", id)
		sm.scripts = append(sm.scripts, s)
		sm.byRepo[slug[id]] = i
	}
	return sm
}

func (m *scriptedModel) Complete(_ context.Context, req ports.ModelRequest) (ports.ModelResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, err := m.scriptFor(req.Messages)
	if err != nil {
		return ports.ModelResponse{}, err
	}
	s := m.scripts[cur]
	turn := m.turn[cur]
	if turn >= len(s.Turns) {
		return ports.ModelResponse{}, errors.New("scripted model: no recorded turn left")
	}
	t := s.Turns[turn]
	m.turn[cur] = turn + 1
	if t.Expect != "" && !strings.Contains(lastToolResults(req.Messages), t.Expect) {
		return ports.ModelResponse{}, fmt.Errorf("scripted model %s turn %d: the tool results lack %q:\n%s",
			s.Case, turn+1, t.Expect, lastToolResults(req.Messages))
	}
	resp := ports.ModelResponse{Finish: ports.ModelFinishToolCalls, CostUSD: t.CostUSD,
		Usage: ports.ModelUsage{InputTokens: t.Usage.Input, CachedInputTokens: t.Usage.Cached, OutputTokens: t.Usage.Output}}
	for i, c := range t.Calls {
		resp.ToolCalls = append(resp.ToolCalls, ports.ModelToolCall{ID: fmt.Sprintf("%s-%d-%d", s.Case, turn+1, i),
			Name: c.Name, Arguments: string(c.Arguments)})
	}
	return resp, nil
}

// scriptFor picks the script of the case whose repository the conversation names.
func (m *scriptedModel) scriptFor(msgs []ports.ModelMessage) (int, error) {
	user := firstUser(msgs)
	for repo, i := range m.byRepo {
		if strings.Contains(user, "Repository: "+repo+".") {
			return i, nil
		}
	}
	return 0, fmt.Errorf("scripted model: no script for the conversation %q", user)
}

func firstUser(msgs []ports.ModelMessage) string {
	for _, m := range msgs {
		if m.Role == ports.ModelRoleUser {
			return m.Content
		}
	}
	return ""
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
	// Two real valid bug cases, each replaying the change that brought the bug in. The script is fixed, so this is a
	// harness check: it proves the tools, the sandbox, the proof rule and the scoring before any model spend.
	o := replayOptions(t, "testify-1207,chi-1185", testingCapUSD)
	model := loadScripts(t, "testify-1207", "chi-1185")
	code, err := reviewRun(context.Background(), o, execRunner{}, model, testWriter{t})
	require.NoError(t, err)
	require.Equal(t, exitAgrees, code)

	rep := readReport(t, o.out)
	require.Len(t, rep.Cases, 2)

	mock, chi := rep.Cases[0], rep.Cases[1]
	require.Equal(t, "testify-1207", mock.ID)
	require.True(t, mock.Scored, mock.Error)
	require.Equal(t, string(ports.AgentStopDone), string(mock.Stop))
	require.Equal(t, 1, mock.Proposed)
	require.Equal(t, 1, mock.Proven, "the nil mutex test fails twice on the head and passes on the fix")
	require.True(t, mock.Hit, "mock/mock.go 507-511 overlaps the truth 510-511 on the head")
	require.Len(t, mock.Findings[0].Runs, 3)
	require.NotNil(t, mock.Truth)

	require.Equal(t, "chi-1185", chi.ID)
	require.True(t, chi.Scored, chi.Error)
	require.Equal(t, string(ports.AgentStopDone), string(chi.Stop))
	require.Equal(t, 1, chi.Proposed)
	require.Equal(t, 1, chi.Proven, "the discard-and-flush test fails twice on the head and passes on the fix")
	require.True(t, chi.Hit, "the truth maps onto the head tree and the finding sits on one of its spans")
	require.Len(t, chi.Findings[0].Runs, 3)
	require.NotNil(t, chi.Truth)

	s := rep.Summary
	require.Equal(t, bench.Ratio{Num: 2, Den: 2}, s.Recall)
	require.Equal(t, bench.Ratio{Num: 0, Den: 0}, s.FalsePositives, "no clean case ran")
	require.Equal(t, bench.Ratio{Num: 2, Den: 2}, s.Evidence)
	require.Positive(t, s.CostUSD)
	require.Equal(t, sandboxNote, rep.Sandbox)

	md, err := os.ReadFile(o.md)
	require.NoError(t, err)
	require.Contains(t, string(md), "| testify-1207 | bug | yes | done |")
	require.Contains(t, string(md), "| chi-1185 | bug | yes | done |")
	t.Log("\n" + string(md))
}

func TestReplayACleanCaseWithAScriptedModel(t *testing.T) {
	// A clean case is scored on false positives. Both recorded findings are wrong on purpose: one test passes on the
	// head, one does not build. Neither is proven, so neither blocks, and the case is scored.
	o := replayOptions(t, "cobra-1643-clean", testingCapUSD)
	o.out = filepath.Join(t.TempDir(), "clean.json")
	o.md = ""
	model := loadScripts(t, "cobra-1643-clean")
	code, err := reviewRun(context.Background(), o, execRunner{}, model, testWriter{t})
	require.NoError(t, err)
	require.Equal(t, exitAgrees, code)

	rep := readReport(t, o.out)
	require.Len(t, rep.Cases, 1)
	clean := rep.Cases[0]
	require.Equal(t, "cobra-1643-clean", clean.ID)
	require.True(t, clean.Scored, clean.Error)
	require.Equal(t, 2, clean.Proposed)
	require.Zero(t, clean.Proven)
	require.False(t, clean.FalsePositive)
	require.Equal(t, map[string]int{"passes_on_head": 1, "build_error_head": 1}, clean.Unproven)

	require.Equal(t, bench.Ratio{Num: 0, Den: 1}, rep.Summary.FalsePositives)
	require.Equal(t, bench.Ratio{Num: 0, Den: 0}, rep.Summary.Recall, "no bug case ran")
}

func TestReplayAT3codeVitestCaseWithAScriptedModel(t *testing.T) {
	// A real TypeScript case from pingdotgg/t3code: the reviewer's vitest file runs in a sparse plain tree after
	// pnpm install, fails twice on the change that brought the bug in, and passes on the fix.
	o := replayOptions(t, "t3code-38193", testingCapUSD)
	o.out = filepath.Join(t.TempDir(), "t3code-vitest.json")
	o.md = ""
	model := loadScripts(t, "t3code-38193")
	code, err := reviewRun(context.Background(), o, execRunner{}, model, testWriter{t})
	require.NoError(t, err)
	require.Equal(t, exitAgrees, code)

	rep := readReport(t, o.out)
	require.Len(t, rep.Cases, 1)
	ts := rep.Cases[0]
	require.True(t, ts.Scored, ts.Error)
	require.Equal(t, string(ports.AgentStopDone), string(ts.Stop))
	require.Equal(t, 1, ts.Proposed)
	require.Equal(t, 1, ts.Proven, "the no-break space test fails twice on the head and passes on the fix")
	require.True(t, ts.Hit, "cliArgs.ts 43-45 overlaps the truth mapped onto the head")
	require.Len(t, ts.Findings[0].Runs, 3)
}

func TestReplayAT3codeCargoCaseWithAScriptedModel(t *testing.T) {
	// A real Rust case from pingdotgg/t3code: the reviewer's module file is declared in the binary crate's root, runs
	// under cargo test, fails twice on the change that brought the bug in (on macOS), and passes on the fix.
	o := replayOptions(t, "t3code-19745", testingCapUSD)
	o.out = filepath.Join(t.TempDir(), "t3code-cargo.json")
	o.md = ""
	model := loadScripts(t, "t3code-19745")
	code, err := reviewRun(context.Background(), o, execRunner{}, model, testWriter{t})
	require.NoError(t, err)
	require.Equal(t, exitAgrees, code)

	rep := readReport(t, o.out)
	require.Len(t, rep.Cases, 1)
	rs := rep.Cases[0]
	require.True(t, rs.Scored, rs.Error)
	require.Equal(t, string(ports.AgentStopDone), string(rs.Stop))
	require.Equal(t, 1, rs.Proposed)
	require.Equal(t, 1, rs.Proven, "the discovery test fails twice on the head and passes on the fix")
	require.True(t, rs.Hit, "main.rs 400 overlaps the truth mapped onto the head")
	require.Len(t, rs.Findings[0].Runs, 3)
}

func TestReplayTwoRealCasesInParallelWithAScriptedModel(t *testing.T) {
	// The same two real cases as the serial check, at two workers. Each case keeps its own cache, sandbox and worktrees,
	// the results file keeps the manifest order, and the one cost cap covers both workers.
	o := replayOptions(t, "testify-1207,chi-1185", testingCapUSD)
	o.parallel = 2
	model := loadScripts(t, "testify-1207", "chi-1185")
	var stderr strings.Builder
	code, err := reviewRun(context.Background(), o, execRunner{}, model, &stderr)
	require.NoError(t, err)
	require.Equal(t, exitAgrees, code)

	rep := readReport(t, o.out)
	require.Len(t, rep.Cases, 2)
	for _, c := range rep.Cases {
		t.Logf("case %s: turns=%d stop=%s scored=%v err=%q", c.ID, c.Turns, c.Stop, c.Scored, c.Error)
	}
	require.Equal(t, "testify-1207", rep.Cases[0].ID, "the results keep the manifest order")
	require.Equal(t, "chi-1185", rep.Cases[1].ID)
	for _, c := range rep.Cases {
		require.True(t, c.Scored, c.Error)
		require.Equal(t, 1, c.Proven)
		require.Equal(t, 6, c.Turns, "each case played its own six-turn script")
		require.Len(t, c.TurnSeconds, 6, "the model latency of every turn is recorded")
		require.NotEmpty(t, c.ToolSeconds, "the tool latency is recorded")
	}
	require.Positive(t, rep.Turns.Turns)
	require.InDelta(t, 6, rep.Turns.PerCase, 1e-12)
	require.LessOrEqual(t, rep.Summary.CostUSD, testingCapUSD)

	// The progress lines are the operator's only view of a long run, so they must name both cases and both kinds of
	// line: a turn, a test run and the end of the case.
	log := stderr.String()
	for _, id := range []string{"testify-1207", "chi-1185"} {
		require.Contains(t, log, "bench case="+id+" start kind=bug")
		require.Contains(t, log, "bench case="+id+" turn=1 tool=list_changes")
		require.Contains(t, log, "bench case="+id+" test=")
		require.Contains(t, log, "bench case="+id+" end scored=yes")
	}
	require.Contains(t, log, "done pass=")
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

// flakyModel answers its failOn-th call with the HTTP 500 the Deploy endpoint gave in live run 2, and passes every other
// call to the script. The failed call does not use up a scripted turn.
type flakyModel struct {
	inner  ports.ModelChat
	failOn int
	calls  int
}

func (m *flakyModel) Complete(ctx context.Context, req ports.ModelRequest) (ports.ModelResponse, error) {
	m.calls++
	if m.calls == m.failOn {
		return ports.ModelResponse{}, &ports.ModelError{Kind: ports.ModelErrorServer, Status: 500,
			ContentType: "application/json", Message: `{"status_code":500,"message":"Internal Server Error"}`}
	}
	return m.inner.Complete(ctx, req)
}

func TestReplayRecoversFromAServerErrorOnTheSecondCall(t *testing.T) {
	// Live run 2 lost each case to its first HTTP 500. Here the second call fails once, the loop waits on the real
	// clock (at most 2 s of jitter) and sends it again, and the case ends as the clean script run does.
	o := replayOptions(t, "testify-1207", testingCapUSD)
	model := &flakyModel{inner: loadScripts(t, "testify-1207"), failOn: 2}
	code, err := reviewRun(context.Background(), o, execRunner{}, model, testWriter{t})
	require.NoError(t, err)
	require.Equal(t, exitAgrees, code)

	rep := readReport(t, o.out)
	require.Empty(t, rep.Stopped)
	require.Len(t, rep.Cases, 1)
	c := rep.Cases[0]
	require.True(t, c.Scored, c.Error)
	require.Equal(t, string(ports.AgentStopDone), string(c.Stop))
	require.Equal(t, 1, c.Retries)
	require.Empty(t, c.FailedCall, "the call that failed was sent again and answered")
	require.Equal(t, 1, c.Proven, "the recovered run proves the same finding as the clean script run")
	require.True(t, c.Hit)
	require.GreaterOrEqual(t, len(c.Transcript), 2)
	require.Equal(t, 1, c.Transcript[0].Attempts)
	require.Equal(t, 2, c.Transcript[1].Attempts)
	require.Len(t, c.Transcript[1].Failed, 1)
	require.Equal(t, "server", c.Transcript[1].Failed[0].Kind)
	require.Equal(t, 500, c.Transcript[1].Failed[0].Status)
	require.LessOrEqual(t, c.Transcript[1].Failed[0].WaitSeconds, 2.0, "the first backoff draws from [0, 2 s]")
	require.Equal(t, model.calls, c.Turns+1, "every call answered once, and one failed")

	md, err := os.ReadFile(o.md)
	require.NoError(t, err)
	require.Contains(t, string(md), "| testify-1207 | bug | yes | done | "+fmt.Sprint(c.Turns)+" | 1 |")
	require.Contains(t, string(md), "| Retries | 1 |")
	t.Log("\n" + string(md))
}

// testWriter sends the progress lines to the test log.
type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

var _ io.Writer = testWriter{}
