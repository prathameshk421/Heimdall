package e2e

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Meta describes a board run.
type Meta struct {
	Story  string
	Agents string
	At     time.Time
}

const titleWidth = 26

// cell is how one step looks in the matrix: ✔, ✘ red, ✘ blocked(x), · or manual.
func cell(r Result) string {
	switch r.Status {
	case StatusPass:
		return r.Step + " ✔"
	case StatusRed:
		return r.Step + " ✘ red"
	case StatusBlocked:
		return fmt.Sprintf("%s ✘ blocked(%s)", r.Step, blockedLabel(r))
	case StatusManual:
		return r.Step + " manual"
	case StatusNotReached:
		return r.Step + " ·"
	}
	return r.Step + " ?"
}

func blockedLabel(r Result) string {
	if r.Task != "" {
		return fmt.Sprintf("%s %s", r.BlockedOn, r.Task)
	}
	return string(r.BlockedOn)
}

// Render prints the board: one line per scenario, then why every step that is not green is not, then the totals.
func Render(w io.Writer, m Meta, results []ScenarioResult) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Heimdall prototype board   story=%s   agents=%s   %s\n", m.Story, m.Agents, m.At.Format("2006-01-02 15:04"))
	width := titleWidth
	for _, sc := range results {
		width = max(width, len(sc.ID+" "+sc.Title)+2) // at least two spaces between the title and the first cell
	}
	for _, sc := range results {
		cells := make([]string, len(sc.Results))
		for i, r := range sc.Results {
			cells[i] = cell(r)
		}
		fmt.Fprintf(&b, "%-*s%s\n", width, sc.ID+" "+sc.Title, strings.Join(cells, "  "))
	}
	b.WriteString("\n")
	for _, sc := range results {
		for _, r := range sc.Results {
			switch r.Status {
			case StatusRed:
				fmt.Fprintf(&b, "%s  red: %s\n", r.Step, r.Why)
			case StatusBlocked:
				fmt.Fprintf(&b, "%s  blocked(%s): %s\n", r.Step, blockedLabel(r), r.Why)
			case StatusNotReached:
				fmt.Fprintf(&b, "%s  not reached: %s\n", r.Step, r.Why)
			case StatusPass, StatusManual:
			}
		}
		for _, v := range sc.Quality {
			fmt.Fprintf(&b, "%s  quality: %s\n", sc.ID, v)
		}
		for _, u := range sc.Unmeasured {
			fmt.Fprintf(&b, "%s  not measured: %s\n", sc.ID, u)
		}
	}
	b.WriteString("\n" + Summarize(results).Line() + "\n")
	_, err := io.WriteString(w, b.String())
	return err
}

type jsonStep struct {
	Step      string  `json:"step"`
	Status    string  `json:"status"`
	Why       string  `json:"why,omitempty"`
	BlockedOn string  `json:"blocked_on,omitempty"`
	Task      string  `json:"task,omitempty"`
	ElapsedMS int64   `json:"elapsed_ms"`
	BarMS     int64   `json:"bar_ms,omitempty"`
	CostUSD   float64 `json:"cost_usd"`
	CostKnown bool    `json:"cost_known"`
}

type jsonScenario struct {
	ID         string     `json:"id"`
	Title      string     `json:"title"`
	Steps      []jsonStep `json:"steps"`
	Quality    []string   `json:"quality,omitempty"`
	Unmeasured []string   `json:"unmeasured,omitempty"`
}

// RenderJSON prints the board as JSON, for tools.
func RenderJSON(w io.Writer, m Meta, results []ScenarioResult) error {
	out := struct {
		Story     string         `json:"story"`
		Agents    string         `json:"agents"`
		At        time.Time      `json:"at"`
		Scenarios []jsonScenario `json:"scenarios"`
		Totals    Totals         `json:"totals"`
	}{Story: m.Story, Agents: m.Agents, At: m.At, Totals: Summarize(results)}
	for _, sc := range results {
		js := jsonScenario{ID: sc.ID, Title: sc.Title, Unmeasured: sc.Unmeasured}
		for _, r := range sc.Results {
			js.Steps = append(js.Steps, jsonStep{
				Step: r.Step, Status: r.Status.String(), Why: r.Why, BlockedOn: string(r.BlockedOn), Task: r.Task,
				ElapsedMS: r.Elapsed.Milliseconds(), BarMS: r.Bar.Milliseconds(), CostUSD: r.CostUSD, CostKnown: r.CostKnown,
			})
		}
		for _, v := range sc.Quality {
			js.Quality = append(js.Quality, v.String())
		}
		out.Scenarios = append(out.Scenarios, js)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// HistoryRow is one line of the board history in docs/PROGRESS.md.
type HistoryRow struct {
	Date                time.Time
	Task, Story, Agents string
	Totals              Totals
}

const historyHeader = "| Date | Task | Story / agents | Green / blocked / red |"

// AppendHistory adds a row to the Board history table of the progress file. It refuses a file without the
// table, so a renamed heading is noticed instead of the history silently stopping.
func AppendHistory(path string, r HistoryRow) error {
	// os.Root confines the read and the write to the progress file's own directory.
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("open progress directory: %w", err)
	}
	defer func() { _ = root.Close() }()
	name := filepath.Base(path)
	raw, err := root.ReadFile(name)
	if err != nil {
		return fmt.Errorf("read progress file: %w", err)
	}
	lines := strings.Split(string(raw), "\n")
	header := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == historyHeader {
			header = i
			break
		}
	}
	if header < 0 || header+1 >= len(lines) {
		return errors.New("progress file has no Board history table (expected the header " + historyHeader + ")")
	}
	end := header + 2 // after the |---| separator
	for end < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[end]), "|") {
		end++
	}
	row := fmt.Sprintf("| %s | %s | %s / %s | %d / %d / %d |", r.Date.Format("2006-01-02"), r.Task, r.Story, r.Agents,
		r.Totals.Green, r.Totals.Blocked, r.Totals.Red)
	lines = append(lines[:end], append([]string{row}, lines[end:]...)...)
	return root.WriteFile(name, []byte(strings.Join(lines, "\n")), 0o600)
}
