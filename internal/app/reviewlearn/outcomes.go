package reviewlearn

import (
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/rulecard"
)

// CardOutcomeCounts folds kept, dismissed, overridden, ignored, and proven finding counts for each
// rule card cited by one reviewer agent. Scoped strictly to that agent's owner (B6).
func (s *Snapshot) CardOutcomeCounts(agent id.AgentName) map[string]rulecard.OutcomeCounts {
	return s.CardOutcomeCountsFor(func(a id.AgentName, _ change.Repo, _ id.SHA) bool {
		return a == agent
	})
}

// CardOutcomeCountsFor folds outcomes for findings whose (agent, repo, base) satisfy match.
// Server callers use this to resolve each row's reviewer agent back to the authenticated card owner
// using the trusted policy at the change's base SHA.
func (s *Snapshot) CardOutcomeCountsFor(match func(agent id.AgentName, repo change.Repo, base id.SHA) bool) map[string]rulecard.OutcomeCounts {
	out := map[string]rulecard.OutcomeCounts{}
	if match == nil {
		return out
	}
	for _, r := range s.rows {
		if len(r.cardRefs) == 0 || !match(r.agent, r.repo, r.base) {
			continue
		}
		for _, card := range r.cardRefs {
			out[card] = applyRowOutcome(out[card], r)
		}
	}
	return out
}

func applyRowOutcome(c rulecard.OutcomeCounts, r row) rulecard.OutcomeCounts {
	if r.proven {
		c.Proven++
	}
	switch {
	case r.overridden:
		c.Overridden++
		if r.reason != "" {
			c.DismissalReasons = append(c.DismissalReasons, r.reason)
		}
	case r.outcome == OutcomeUseful:
		c.Kept++
	case r.outcome == OutcomeNoise:
		c.Dismissed++
		if r.reason != "" {
			c.DismissalReasons = append(c.DismissalReasons, r.reason)
		}
	case r.outcome == OutcomeIgnored:
		c.Ignored++
	}
	return c
}

// CardOutcomes produces the owner-only calibration summary for one owner's cards using the folded
// outcomes of that owner's reviewer seat.
func (s *Snapshot) CardOutcomes(agent id.AgentName, cards []rulecard.Card) []rulecard.CardOutcome {
	return rulecard.SummarizeOutcomes(cards, s.CardOutcomeCounts(agent))
}

// CardOutcomesFor produces the owner-only calibration summary using a policy-bound row matcher.
func (s *Snapshot) CardOutcomesFor(cards []rulecard.Card, match func(agent id.AgentName, repo change.Repo, base id.SHA) bool) []rulecard.CardOutcome {
	return rulecard.SummarizeOutcomes(cards, s.CardOutcomeCountsFor(match))
}
