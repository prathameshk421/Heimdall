package e2e

import "fmt"

// Totals counts steps by outcome. Blocked and manual are never counted as green.
type Totals struct {
	Steps, Green, Blocked, Red, NotReached, Manual int
	CostUSD                                        float64 // summed over the steps whose cost was measured
	CostUnmeasured                                 int     // steps that ran but whose cost could not be measured
	// QualityViolations counts every quality-check failure: those on a step and those on a whole scenario.
	QualityViolations int
	// Unmeasured counts checks and readings that could not be made yet, so nothing is claimed for them.
	Unmeasured int
}

// Summarize counts the steps of all scenarios by outcome, and the quality and unmeasured checks.
func Summarize(results []ScenarioResult) Totals {
	var t Totals
	for _, sc := range results {
		t.QualityViolations += len(sc.Quality)
		t.Unmeasured += len(sc.Unmeasured)
		for _, r := range sc.Results {
			t.Steps++
			switch r.Status {
			case StatusPass:
				t.Green++
			case StatusBlocked:
				t.Blocked++
			case StatusRed:
				t.Red++
			case StatusNotReached:
				t.NotReached++
			case StatusManual:
				t.Manual++
			}
			t.QualityViolations += len(r.Violations)
			t.Unmeasured += len(r.Unmeasured)
			if r.Status == StatusPass || r.Status == StatusRed {
				if r.CostKnown {
					t.CostUSD += r.CostUSD
				} else {
					t.CostUnmeasured++
				}
			}
		}
	}
	return t
}

// StrictFailure reports whether -strict must exit non-zero: a step is not green, a quality check failed, a check
// or reading was not measured, or a step's cost was not measured. A board can only claim success when all four hold.
func (t Totals) StrictFailure() bool {
	return t.Green != t.Steps || t.QualityViolations > 0 || t.Unmeasured > 0 || t.CostUnmeasured > 0
}

// Line is the board's last line. The quality and unmeasured counts appear only when they are not zero.
func (t Totals) Line() string {
	cost := fmt.Sprintf("cost $%.2f", t.CostUSD)
	if t.CostUnmeasured > 0 {
		cost = fmt.Sprintf("cost not measured for %d steps", t.CostUnmeasured)
	}
	line := fmt.Sprintf("steps %d/%d green · %d blocked · %d red · %d not reached · %d manual",
		t.Green, t.Steps, t.Blocked, t.Red, t.NotReached, t.Manual)
	if t.QualityViolations > 0 {
		line += fmt.Sprintf(" · %d quality violation(s)", t.QualityViolations)
	}
	if t.Unmeasured > 0 {
		line += fmt.Sprintf(" · %d unmeasured", t.Unmeasured)
	}
	return line + "        " + cost
}
