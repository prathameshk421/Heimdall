package e2e_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/e2e"
)

func sample() []e2e.ScenarioResult {
	return []e2e.ScenarioResult{
		{ID: "P1", Title: "Ticket to red tests", Results: []e2e.Result{
			{Step: "P1.1", Status: e2e.StatusPass, Elapsed: 2 * time.Second, Bar: 45 * time.Second},
			{Step: "P1.2", Status: e2e.StatusBlocked, BlockedOn: e2e.NeedForward, Why: "nothing listens on 127.0.0.1:8000"},
			{Step: "P1.3", Status: e2e.StatusNotReached, Why: "P1.2 did not pass"},
		}},
		{ID: "P2", Title: "The door", Results: []e2e.Result{
			{Step: "P2.1", Status: e2e.StatusRed, Why: "expected the gate to fail within 15s, saw no check"},
			{Step: "P2.2", Status: e2e.StatusBlocked, BlockedOn: "driver", Task: "T36", Why: "the Jira driver is not built yet (T36)"},
			{Step: "P2.3", Status: e2e.StatusBlocked, BlockedOn: "story", Task: "T31", Why: "story fixture-pay is incomplete until T31"},
		}, Quality: []e2e.Violation{{Check: e2e.CheckQuiet, Detail: "progress chatter"}}, Unmeasured: []string{"idempotent replay: the poller replay is not built yet (T26)"}},
	}
}

func TestRenderPrintsTheMatrixThenWhyThenTheTotals(t *testing.T) {
	var buf bytes.Buffer
	meta := e2e.Meta{Story: "fixture-pay", Agents: "scripted", At: time.Date(2026, 11, 5, 14, 2, 0, 0, time.UTC)}
	require.NoError(t, e2e.Render(&buf, meta, sample()))
	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")

	require.Equal(t, "Heimdall prototype board   story=fixture-pay   agents=scripted   2026-11-05 14:02", lines[0])
	require.Equal(t, "P1 Ticket to red tests"+strings.Repeat(" ", 26-len("P1 Ticket to red tests"))+"P1.1 ✔  P1.2 ✘ blocked(forward)  P1.3 ·", lines[1])
	require.Equal(t, "P2 The door"+strings.Repeat(" ", 26-len("P2 The door"))+"P2.1 ✘ red  P2.2 ✘ blocked(driver T36)  P2.3 ✘ blocked(story T31)", lines[2])

	require.Contains(t, out, "P1.2  blocked(forward): nothing listens on 127.0.0.1:8000")
	require.Contains(t, out, "P2.1  red: expected the gate to fail within 15s, saw no check")
	require.Contains(t, out, "P1.3  not reached: P1.2 did not pass")
	require.Contains(t, out, "P2  quality: quiet: progress chatter")
	require.Contains(t, out, "P2  not measured: idempotent replay: the poller replay is not built yet (T26)")
	require.NotContains(t, out, "P1.1  pass", "a green step needs no explanation")

	last := lines[len(lines)-1]
	require.True(t, strings.HasPrefix(last, "steps 1/6 green · 3 blocked · 1 red · 1 not reached · 0 manual"), last)
}

func TestRenderJSONHasOneEntryPerStepWithAStatusWord(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, e2e.RenderJSON(&buf, e2e.Meta{Story: "fixture-pay", Agents: "scripted"}, sample()))
	var got struct {
		Story     string
		Agents    string
		Scenarios []struct {
			ID    string
			Steps []struct {
				Step, Status, Why, Task string
				BlockedOn               string `json:"blocked_on"`
			}
		}
		Totals struct{ Steps, Green, Blocked, Red int }
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	require.Equal(t, "fixture-pay", got.Story)
	require.Len(t, got.Scenarios, 2)
	require.Equal(t, "blocked", got.Scenarios[0].Steps[1].Status)
	require.Equal(t, "forward", got.Scenarios[0].Steps[1].BlockedOn)
	require.Equal(t, "T36", got.Scenarios[1].Steps[1].Task)
	require.Equal(t, 6, got.Totals.Steps)
	require.Equal(t, 1, got.Totals.Green)
}

const progress = `# Heimdall progress

## Board history

| Date | Task | Story / agents | Green / blocked / red |
|---|---|---|---|

## Other
text
`

func TestAppendHistoryAddsARowUnderTheBoardHistoryTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "PROGRESS.md")
	require.NoError(t, os.WriteFile(path, []byte(progress), 0o600))
	row := e2e.HistoryRow{Date: time.Date(2026, 11, 5, 14, 2, 0, 0, time.UTC), Task: "T98", Story: "fixture-pay", Agents: "scripted", Totals: e2e.Totals{Steps: 40, Green: 2, Blocked: 30, Red: 8}}
	require.NoError(t, e2e.AppendHistory(path, row))
	require.NoError(t, e2e.AppendHistory(path, row))
	b, err := os.ReadFile(path) // #nosec G304 -- the test's own temp file
	require.NoError(t, err)
	text := string(b)
	require.Equal(t, 2, strings.Count(text, "| 2026-11-05 | T98 | fixture-pay / scripted | 2 / 30 / 8 |"))
	require.Less(t, strings.Index(text, "| 2026-11-05 | T98"), strings.Index(text, "## Other"), "rows stay inside the table")
	require.Contains(t, text, "## Other\ntext\n", "the rest of the file is untouched")
}

func TestAppendHistoryRefusesAFileWithoutTheTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "PROGRESS.md")
	require.NoError(t, os.WriteFile(path, []byte("# nothing here\n"), 0o600))
	require.Error(t, e2e.AppendHistory(path, e2e.HistoryRow{}))
}

func TestRenderWidensTheTitleColumnForALongTitle(t *testing.T) {
	res := []e2e.ScenarioResult{
		{ID: "P5", Title: "Fix to green, and the ways to cheat that must fail", Results: []e2e.Result{{Step: "P5.1", Status: e2e.StatusPass}}},
		{ID: "P1", Title: "Short", Results: []e2e.Result{{Step: "P1.1", Status: e2e.StatusPass}}},
	}
	var buf bytes.Buffer
	require.NoError(t, e2e.Render(&buf, e2e.Meta{Story: "s", Agents: "scripted", At: time.Now()}, res))
	lines := strings.Split(buf.String(), "\n")
	long, short := lines[1], lines[2]
	require.Contains(t, long, "must fail  P5.1", "at least two spaces between the title and the first cell")
	require.Equal(t, strings.Index(long, "P5.1 ✔"), strings.Index(short, "P1.1 ✔"), "the cells line up in one column")
}
