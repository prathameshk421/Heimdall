// Command prototype runs the prototype board: every step of scenarios P1 to P9 against a story, and prints one cell
// per step (MVP_SCENARIOS section 2). It is the progress bar for the whole project. Run it through `make prototype`.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/e2e"
	"github.com/prathameshk421/flip_the_switch_hackathon/e2e/scenarios"
)

func main() {
	code, err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "prototype:", err)
		os.Exit(2)
	}
	os.Exit(code)
}

type options struct {
	story     string
	agents    string
	only      string
	json      bool
	strict    bool
	demo      bool
	progress  string
	task      string
	noHistory bool
	binary    string
}

func parseFlags() options {
	var o options
	flag.StringVar(&o.story, "story", envOr("STORY", "fixture-pay"), "story name under e2e/stories")
	flag.StringVar(&o.agents, "agents", envOr("AGENTS", "scripted"), "scripted or live")
	flag.StringVar(&o.only, "only", os.Getenv("ONLY"), "run one scenario, for example P3")
	flag.BoolVar(&o.json, "json", os.Getenv("JSON") == "1", "print the board as JSON")
	flag.BoolVar(&o.strict, "strict", false, "exit 1 unless every step is green")
	flag.BoolVar(&o.demo, "demo", false, "run the demo script as a test (make demo-check)")
	flag.StringVar(&o.progress, "progress", "docs/PROGRESS.md", "progress file whose board history gets a row")
	flag.StringVar(&o.task, "task", os.Getenv("TASK"), "task id to write in the board history row")
	flag.BoolVar(&o.noHistory, "no-history", false, "do not append a board history row")
	flag.StringVar(&o.binary, "heimdall", ".cache/heimdall", "the heimdall command-line binary")
	flag.Parse()
	return o
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// run returns the exit code: 0, or 1 when -strict is set and a step is not green.
func run() (int, error) {
	o := parseFlags()
	if o.agents != "scripted" && o.agents != "live" {
		return 0, fmt.Errorf("-agents must be scripted or live, got %q", o.agents)
	}
	story, err := e2e.LoadStory(filepath.Join("e2e", "stories", o.story+".yaml"))
	if err != nil {
		return 0, err
	}
	scs, err := selectScenarios(scenarios.All(), o.only)
	if err != nil {
		return 0, err
	}
	world := e2e.NewLocalWorld(e2e.LocalConfig{
		Story:          story,
		Agents:         o.agents,
		ServerURL:      os.Getenv("HEIMDALL_URL"),
		ForwardAddrs:   []string{"127.0.0.1:8000", "127.0.0.1:8065"},
		HeimdallBinary: o.binary,
	})
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	board := &e2e.Board{World: world, Story: story, Now: time.Now, Scripted: o.agents == "scripted"}
	results := board.Run(ctx, scs)
	meta := e2e.Meta{Story: story.Name, Agents: o.agents, At: time.Now()}
	if o.demo {
		return runDemo(results)
	}
	if err := render(o, meta, results); err != nil {
		return 0, err
	}
	if err := recordHistory(o, meta, results); err != nil {
		return 0, err
	}
	if t := e2e.Summarize(results); o.strict && t.Green != t.Steps {
		return 1, nil
	}
	return 0, nil
}

func render(o options, meta e2e.Meta, results []e2e.ScenarioResult) error {
	if o.json {
		return e2e.RenderJSON(os.Stdout, meta, results)
	}
	return e2e.Render(os.Stdout, meta, results)
}

// selectScenarios keeps all scenarios, or the one named by only (P3, or p3).
func selectScenarios(all []e2e.Scenario, only string) ([]e2e.Scenario, error) {
	if only == "" {
		return all, nil
	}
	for _, sc := range all {
		if strings.EqualFold(sc.ID, only) {
			return []e2e.Scenario{sc}, nil
		}
	}
	return nil, fmt.Errorf("no scenario %q (P1 to P9)", only)
}

// runDemo prints the demo script as a test: one line per beat of the video, and fails unless every beat is real.
func runDemo(results []e2e.ScenarioResult) (int, error) {
	beats := e2e.DemoCheck(results)
	if _, err := fmt.Fprint(os.Stdout, e2e.RenderDemo(beats)); err != nil {
		return 0, err
	}
	for _, b := range beats {
		if b.Status != e2e.StatusPass {
			return 1, nil
		}
	}
	return 0, nil
}

// recordHistory appends a row to the progress file's board history after a full run.
func recordHistory(o options, meta e2e.Meta, results []e2e.ScenarioResult) error {
	if o.noHistory || o.only != "" || o.demo {
		return nil
	}
	row := e2e.HistoryRow{Date: meta.At, Task: o.task, Story: meta.Story, Agents: meta.Agents, Totals: e2e.Summarize(results)}
	return e2e.AppendHistory(o.progress, row)
}
