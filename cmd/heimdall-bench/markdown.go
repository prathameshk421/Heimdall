package main

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/bench"
)

// reviewMarkdown writes a review run as a BENCH.md section: the run, one row per case and the summary.
func reviewMarkdown(rep reviewReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n## Reviewer run %s (model %s)\n\n", rep.Started, rep.Model)
	b.WriteString("| Item | Value |\n|---|---|\n")
	fmt.Fprintf(&b, "| Command | `%s` |\n", rep.Command)
	fmt.Fprintf(&b, "| Heimdall commit | `%s` |\n", rep.HeimdallCommit)
	fmt.Fprintf(&b, "| Machine | %s, %s |\n", rep.Machine, rep.GoVersion)
	fmt.Fprintf(&b, "| Price (USD per million tokens) | input %.4g, cached input %.4g, output %.4g |\n",
		rep.Price.InputPerM, rep.Price.CachedInputPerM, rep.Price.OutputPerM)
	fmt.Fprintf(&b, "| Cost cap | $%.2f for the run, $%.2f per case |\n", rep.MaxCostUSD, rep.PerCaseUSD)
	fmt.Fprintf(&b, "| Blocking severity | %s and above |\n", rep.BlockSeverity)
	fmt.Fprintf(&b, "| Sandbox | %s |\n", rep.Sandbox)
	fmt.Fprintf(&b, "| Wall time | %.1f s |\n", rep.Seconds)
	if rep.Stopped != "" {
		fmt.Fprintf(&b, "| Stopped | %s |\n", rep.Stopped)
	}
	b.WriteString("\n| Case | Kind | Scored | Stop | Turns | Retries | Turn s p50/p95 | Proposed | Proven | Hit | Located | FP | Tokens in/out | Cost | Time | Unproven or error |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	retries := 0
	for _, c := range rep.Cases {
		retries += c.Retries
		b.WriteString(caseRow(c))
	}
	s := rep.Summary
	t := rep.Turns
	b.WriteString("\n| Measure | Value |\n|---|---|\n")
	fmt.Fprintf(&b, "| Recall (bug cases with a proven finding on the truth) | %s |\n", s.Recall)
	fmt.Fprintf(&b, "| False positives (clean cases with a proven blocking finding) | %s |\n", s.FalsePositives)
	fmt.Fprintf(&b, "| Clean passes (scored clean cases with no false positive and no injection) | %s |\n", s.CleanPasses)
	fmt.Fprintf(&b, "| Injected cases (the model wrote the case's canary) | %d |\n", s.Injected)
	fmt.Fprintf(&b, "| Evidence rate (proven / proposed) | %s |\n", s.Evidence)
	fmt.Fprintf(&b, "| Unproven by reason | %s |\n", escapeCell(countList(s.Unproven)))
	fmt.Fprintf(&b, "| Cases scored / not finished | %d / %d |\n", s.Scored, s.Unfinished)
	fmt.Fprintf(&b, "| Cost | $%.4f total; per case p50 $%.4f, p95 $%.4f |\n", s.CostUSD, s.CostP50, s.CostP95)
	fmt.Fprintf(&b, "| Tokens | %d input (%d cached), %d output |\n", s.InputTokens, s.CachedTokens, s.OutputTokens)
	fmt.Fprintf(&b, "| Wall time per case | p50 %.1f s, p95 %.1f s |\n", s.WallP50, s.WallP95)
	fmt.Fprintf(&b, "| Retries | %d |\n", retries)
	fmt.Fprintf(&b, "| Model turns | %d total, %.1f per case; %.1f s median, %.1f s p95 |\n", t.Turns, t.PerCase, t.TurnP50, t.TurnP95)
	fmt.Fprintf(&b, "| Tool calls | %.1f s total, %.1f s median, %.1f s p95 |\n", t.ToolSecs, t.ToolP50, t.ToolP95)
	return b.String()
}

// caseRow writes the table row of one case. Its note is the error, else the unproven reasons, after a warning when the
// model wrote the case's canary.
func caseRow(c reviewCase) string {
	note := c.Error
	if note == "" {
		note = countList(c.Unproven)
	}
	if c.Injected {
		note = "INJECTED: the canary is in " + strings.Join(c.InjectedAt, "; ") + ". " + note
	}
	return fmt.Sprintf("| %s | %s | %s | %s | %d | %d | %s | %d | %d | %s | %s | %s | %d/%d | $%.4f | %.1f s | %s |\n",
		c.ID, c.Kind, yes(c.Scored), c.Stop, c.Turns, c.Retries, turnLatency(c.TurnSeconds), c.Proposed, c.Proven,
		bugOnly(c.Kind == "bug", c.Hit), bugOnly(c.Kind == "bug", c.Located), bugOnly(c.Kind == "clean", c.FalsePositive),
		c.InputTokens, c.OutputTokens, c.CostUSD, c.Seconds, escapeCell(note))
}

func yes(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

// turnLatency writes the median and p95 seconds of one model turn, or "n/a" for a case with no recorded turn.
func turnLatency(seconds []float64) string {
	if len(seconds) == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.1f/%.1f", bench.Percentile(seconds, 50), bench.Percentile(seconds, 95))
}

// bugOnly writes a measure only for the kind it applies to.
func bugOnly(applies, v bool) string {
	if !applies {
		return ""
	}
	return yes(v)
}

func countList(m map[string]int) string {
	var parts []string
	for _, k := range slices.Sorted(maps.Keys(m)) {
		parts = append(parts, fmt.Sprintf("%s %d", k, m[k]))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

func escapeCell(s string) string {
	return strings.NewReplacer("|", "\\|", "\n", " ").Replace(s)
}
