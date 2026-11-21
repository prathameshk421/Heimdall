package main

import (
	"fmt"
	"io"
	"math"
	"sync"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/bench"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// The progress lines of a review run. A live run takes minutes to hours, because a reasoning model needs 20 to 60 s
// per turn and a case takes about eight turns, so the operator must see where the run is. Every line is one line, short
// and stable, so a run can be read and grepped while it is still going.
//
//	bench case=<id> start kind=<kind>
//	bench case=<id> turn=<n> tool=<tool,tool|-> in=<n> out=<n> case$=<usd> run$=<usd> <elapsed>
//	bench case=<id> test=<tree> start
//	bench case=<id> test=<tree> done pass=<n> fail=<n> errored=<n> <seconds>
//	bench case=<id> end scored=<yes|no> verdict=<v> cost$=<usd> run$=<usd> <wall>

// progress writes those lines to one writer. The workers of a parallel run share it, so a line never interleaves with
// another.
type progress struct {
	mu sync.Mutex
	w  io.Writer
}

// treeLabel names a tree in a progress line by the first eight characters of its commit, which is how an operator
// reads a commit in git.
func treeLabel(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

// newProgress returns a writer of progress lines. A nil writer drops them, for a test that does not read them.
func newProgress(w io.Writer) *progress { return &progress{w: w} }

// line writes one line. A failed write to stderr has nowhere better to go.
func (p *progress) line(format string, args ...any) {
	if p.w == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_, _ = fmt.Fprintf(p.w, format, args...)
}

// caseProgress is the timing and the spend of one case: its turns, its tool calls and its test runs. Only the case's
// own worker writes it, so it holds no lock of its own. The progress writer it holds is shared and locks itself.
type caseProgress struct {
	p     *progress
	id    string
	start time.Time
	turn  int
	spend float64
	turns []float64
	tools []bench.TurnToolLatency
}

// newCaseProgress starts the clock of one case.
func newCaseProgress(p *progress, id string) *caseProgress {
	return &caseProgress{p: p, id: id, start: time.Now()}
}

// startCase writes the line of a case that starts.
func (c *caseProgress) startCase(kind bench.Kind) {
	c.p.line("bench case=%s start kind=%s\n", c.id, kind)
}

// elapsed is the time since the case started.
func (c *caseProgress) elapsed() time.Duration { return time.Since(c.start) }

// modelTurn records the answer that just arrived and writes its line: the turn number, the tools it called, the tokens,
// the spend of the case and of the run, and the elapsed time of the case.
func (c *caseProgress) modelTurn(resp benchTurn, runSpent float64, elapsed time.Duration) {
	c.turn++
	c.spend += resp.cost
	c.turns = append(c.turns, roundSeconds(resp.seconds))
	c.p.line("bench case=%s turn=%d tool=%s in=%d out=%d case$=%.4f run$=%.4f %.1fs\n",
		c.id, c.turn, toolNames(resp.tools), resp.input, resp.output, c.spend, runSpent, elapsed.Seconds())
}

// toolCall records the latency of one tool call, for the results file and the Markdown table.
func (c *caseProgress) toolCall(name string, seconds float64) {
	c.tools = append(c.tools, bench.TurnToolLatency{Turn: c.turn, Tool: name, Seconds: roundSeconds(seconds)})
}

// roundSeconds keeps the milliseconds of a latency, so a results file is readable and a percentile of a fast scripted
// turn is not a fraction of a microsecond.
func roundSeconds(s float64) float64 { return math.Round(s*1000) / 1000 }

// testStart writes the line of a test run that starts on one tree.
func (c *caseProgress) testStart(label string) {
	c.p.line("bench case=%s test=%s start\n", c.id, label)
}

// testEnd writes the line of a test run that ended: the counts and the seconds. An error is a run that produced no
// result at all, so it says so instead of a zero that looks like a fast pass.
func (c *caseProgress) testEnd(label string, res ports.SandboxResult, err error, seconds float64) {
	if err != nil {
		c.p.line("bench case=%s test=%s done error=yes %.1fs\n", c.id, label, seconds)
		return
	}
	c.p.line("bench case=%s test=%s done pass=%d fail=%d errored=%d %.1fs\n",
		c.id, label, res.Passed, res.Failed, res.Errored, seconds)
}

// end writes the line of a case that ended: the verdict, the cost and the wall time.
func (c *caseProgress) end(rc reviewCase, runSpent float64) {
	c.p.line("bench case=%s end scored=%s verdict=%s cost$=%.4f run$=%.4f %.1fs\n",
		c.id, yes(rc.Scored), verdictOf(rc), rc.CostUSD, runSpent, time.Since(c.start).Seconds())
}

// turnsSeconds returns the model latency of every turn of the case.
func (c *caseProgress) turnsSeconds() []float64 { return c.turns }

// toolSeconds returns the tool latency of every tool call of the case.
func (c *caseProgress) toolSeconds() []bench.TurnToolLatency { return c.tools }

// benchTurn is the part of one model answer that a progress line and the timing need.
type benchTurn struct {
	tools   []string
	input   int64
	output  int64
	cost    float64
	seconds float64
}

// toolNames writes the tool names of one answer as one field, or "-" when the answer called no tool.
func toolNames(names []string) string {
	if len(names) == 0 {
		return "-"
	}
	out := names[0]
	for _, n := range names[1:] {
		out += "," + n
	}
	return out
}

// verdictOf names what the case found, in one word the operator can read at a glance.
func verdictOf(rc reviewCase) string {
	switch {
	case rc.Error != "":
		return "error"
	case !rc.Scored:
		return "unscored"
	case rc.Kind == bench.KindBug && rc.Hit:
		return "hit"
	case rc.Kind == bench.KindBug && rc.Located:
		return "located"
	case rc.Kind == bench.KindBug:
		return "miss"
	case rc.FalsePositive:
		return "false_positive"
	default:
		return "clean"
	}
}
