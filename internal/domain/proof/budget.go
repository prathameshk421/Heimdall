package proof

import (
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

// Spend is what a proof run used. The caller adds it up from the provider's usage on each response (ACTION_MODE 7).
type Spend struct {
	USD          float64
	InputTokens  int
	OutputTokens int
}

// Exceeds reports whether the spend is above a per-PR cap. A spend equal to the cap does not exceed it: the loop stops
// at the cap, so a run that ends exactly there is complete. A value that cannot be true (negative, not a number or
// infinite) counts as over the cap, because a missing measure must not look like a cheap run.
func (s Spend) Exceeds(b policy.ActionBudget) bool {
	return !validMoney(s.USD) || s.InputTokens < 0 || s.OutputTokens < 0 ||
		s.USD > b.PerPRUSD || s.InputTokens > b.PerPRTokens.Input || s.OutputTokens > b.PerPRTokens.Output
}

// MonthlyStop is the check a proof job makes before its first call. It returns ReasonMonthlyCap when the month's spend
// so far plus one per-PR budget is over the monthly cap, and "" when the run may start. A month total that cannot be
// true is not believed. Caps come from the action area with defaults filled. The cap is advisory: runs that start
// together can pass it by their number times the per-PR budget, and the provider's own limit is the hard stop.
func MonthlyStop(a policy.Action, monthSpentUSD float64) Reason {
	b := a.Effective().Budget
	if !validMoney(monthSpentUSD) || monthSpentUSD+b.PerPRUSD > b.MonthlyUSD {
		return ReasonMonthlyCap
	}
	return ""
}

// maxMoney is far above any real spend. The range check also refuses not-a-number and infinity, because every
// comparison with them is false.
const maxMoney = 1e12

func validMoney(v float64) bool { return v >= 0 && v < maxMoney }
