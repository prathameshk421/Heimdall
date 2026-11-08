package reviewlearn

import (
	"cmp"
	"slices"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/rulecard"
)

// Thresholds of the noisy category rule.
const (
	noisyMinDecided = 3
	noisyShare      = 0.6
)

// Snapshot is a read model of every finding and its outcome at one moment. It is safe to read from many goroutines
// because it is not changed after it is built.
type Snapshot struct {
	rows      []row
	proposals []Proposal
}

// Calibration says how one agent's findings turned out.
type Calibration struct {
	Useful          int
	Noise           int
	Ignored         int
	NoisyCategories []string
	ResolutionRate  float64
}

// PriorFinding is an earlier finding in the same repository, with how people answered it.
type PriorFinding struct {
	Change   id.ChangeID
	Ref      string
	File     string
	Claim    string
	Category string
	Outcome  string
	Reason   string
}

// AgentReport is the outcome count of one agent.
type AgentReport struct {
	Agent          id.AgentName
	Findings       int
	Useful         int
	Noise          int
	Ignored        int
	Neutral        int
	Pending        int
	ResolutionRate float64
}

// CardRecords counts, for one agent, how the findings that cite each rule card turned out. The key is the card file name.
func (s *Snapshot) CardRecords(agent id.AgentName) map[string]rulecard.Record {
	out := map[string]rulecard.Record{}
	for _, r := range s.rows {
		if r.agent != agent {
			continue
		}
		for _, card := range r.cardRefs {
			rec := out[card]
			switch r.outcome {
			case OutcomeUseful:
				rec.Useful++
			case OutcomeNoise:
				rec.Noise++
			case OutcomeIgnored:
				rec.Ignored++
			}
			out[card] = rec
		}
	}
	return out
}

// Calibration returns the outcome counts of one agent, the categories where it is mostly noise, and its resolution rate.
func (s *Snapshot) Calibration(agent id.AgentName) Calibration {
	var cal Calibration
	byCategory := map[finding.Category]*[2]int{} // decided, noise
	for _, r := range s.rows {
		if r.agent != agent {
			continue
		}
		decided := r.outcome == OutcomeUseful || r.outcome == OutcomeNoise || r.outcome == OutcomeIgnored
		cal.add(r.outcome)
		if !decided {
			continue
		}
		counts := byCategory[r.category]
		if counts == nil {
			counts = &[2]int{}
			byCategory[r.category] = counts
		}
		counts[0]++
		if r.outcome == OutcomeNoise {
			counts[1]++
		}
	}
	for category, counts := range byCategory {
		if counts[0] >= noisyMinDecided && float64(counts[1])/float64(counts[0]) >= noisyShare {
			cal.NoisyCategories = append(cal.NoisyCategories, string(category))
		}
	}
	slices.Sort(cal.NoisyCategories)
	cal.ResolutionRate = rate(cal.Useful, cal.Noise, cal.Ignored)
	return cal
}

func (c *Calibration) add(outcome string) {
	switch outcome {
	case OutcomeUseful:
		c.Useful++
	case OutcomeNoise:
		c.Noise++
	case OutcomeIgnored:
		c.Ignored++
	}
}

func rate(useful, noise, ignored int) float64 {
	total := useful + noise + ignored
	if total == 0 {
		return 0
	}
	return float64(useful) / float64(total)
}

// PriorFindings lists decided findings of repo, not from the excluded change. Findings a person dismissed or upheld come
// first, then the newest. When files is not empty, findings on those files come before the rest of the same group.
func (s *Snapshot) PriorFindings(repo change.Repo, files []string, exclude id.ChangeID, limit int) []PriorFinding {
	var picked []row
	for _, r := range s.rows {
		if r.repo != repo || r.change == exclude || r.outcome == OutcomePending {
			continue
		}
		picked = append(picked, r)
	}
	slices.SortFunc(picked, func(a, b row) int {
		if c := cmp.Compare(priorRank(a, files), priorRank(b, files)); c != 0 {
			return c
		}
		if c := b.at.Compare(a.at); c != 0 {
			return c
		}
		return cmp.Or(cmp.Compare(a.change, b.change), cmp.Compare(a.ref, b.ref))
	})
	if limit >= 0 && len(picked) > limit {
		picked = picked[:limit]
	}
	out := make([]PriorFinding, 0, len(picked))
	for _, r := range picked {
		out = append(out, PriorFinding{Change: r.change, Ref: r.ref, File: r.file, Claim: r.claim,
			Category: string(r.category), Outcome: r.outcome, Reason: r.reason})
	}
	return out
}

// priorRank orders findings: a person's answer first, then a match with the files of the new change.
func priorRank(r row, files []string) int {
	rank := 0
	if r.state != finding.StateDismissed && r.state != finding.StateUpheld {
		rank += 2
	}
	if len(files) > 0 && !slices.Contains(files, r.file) {
		rank++
	}
	return rank
}

// Proposals returns the rule cards that human decisions suggest, in name order.
func (s *Snapshot) Proposals() []Proposal { return slices.Clone(s.proposals) }

// Report returns one line per agent, in name order.
func (s *Snapshot) Report() []AgentReport {
	byAgent := map[id.AgentName]*AgentReport{}
	for _, r := range s.rows {
		rep := byAgent[r.agent]
		if rep == nil {
			rep = &AgentReport{Agent: r.agent}
			byAgent[r.agent] = rep
		}
		rep.Findings++
		switch r.outcome {
		case OutcomeUseful:
			rep.Useful++
		case OutcomeNoise:
			rep.Noise++
		case OutcomeIgnored:
			rep.Ignored++
		case OutcomeNeutral:
			rep.Neutral++
		default:
			rep.Pending++
		}
	}
	out := make([]AgentReport, 0, len(byAgent))
	for _, rep := range byAgent {
		rep.ResolutionRate = rate(rep.Useful, rep.Noise, rep.Ignored)
		out = append(out, *rep)
	}
	slices.SortFunc(out, func(a, b AgentReport) int { return cmp.Compare(a.Agent, b.Agent) })
	return out
}
