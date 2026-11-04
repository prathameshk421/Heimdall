package e2e

import "fmt"

// Totals counts steps by outcome. Blocked is never counted as green.
type Totals struct {
	Steps, Green, Blocked, Red, NotReached, Manual int
	CostUSD                                        float64 // summed over the steps whose cost was measured
	CostUnmeasured                                 int     // steps that ran but whose cost could not be measured
}

// Summarize counts the steps of all scenarios by outcome.
func Summarize(results []ScenarioResult) Totals {
	var t Totals
	for _, sc := range results {
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

// Line is the board's last line.
func (t Totals) Line() string {
	cost := fmt.Sprintf("cost $%.2f", t.CostUSD)
	if t.CostUnmeasured > 0 {
		cost = fmt.Sprintf("cost not measured for %d steps", t.CostUnmeasured)
	}
	return fmt.Sprintf("steps %d/%d green · %d blocked · %d red · %d not reached · %d manual        %s",
		t.Green, t.Steps, t.Blocked, t.Red, t.NotReached, t.Manual, cost)
}
