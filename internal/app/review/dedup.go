package review

import (
	"strings"
	"unicode"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

const duplicateClaimJaccard = 0.6

func merge(existing, accepted []FiledFinding) []FiledFinding {
	merged := append([]FiledFinding(nil), existing...)
	for _, candidate := range accepted {
		match := -1
		for i, known := range merged {
			if hasReporter(known.Reporters, candidate.Reporters[0].Agent) {
				continue
			}
			if overlaps(known.Input.Location, candidate.Input.Location) && jaccard(known.Input.Claim, candidate.Input.Claim) >= duplicateClaimJaccard {
				match = i
				break
			}
		}
		if match < 0 {
			merged = append(merged, candidate)
			continue
		}
		known := &merged[match]
		known.Reporters = append(known.Reporters, candidate.Reporters...)
		if candidate.Tier.AtLeast(known.Tier) && candidate.Tier != known.Tier {
			known.Input = candidate.Input
			known.Tier = candidate.Tier
		}
	}
	return merged
}

func hasReporter(reporters []Reporter, agent id.AgentName) bool {
	for _, reporter := range reporters {
		if reporter.Agent == agent {
			return true
		}
	}
	return false
}

func overlaps(a, b finding.Location) bool {
	return a.File == b.File && a.Lines[0] <= b.Lines[1] && b.Lines[0] <= a.Lines[1]
}

func jaccard(a, b string) float64 {
	left, right := tokens(a), tokens(b)
	if len(left) == 0 && len(right) == 0 {
		return 1
	}
	intersection := 0
	for token := range left {
		if _, ok := right[token]; ok {
			intersection++
		}
	}
	union := len(left) + len(right) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func tokens(text string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, token := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }) {
		set[token] = struct{}{}
	}
	return set
}
