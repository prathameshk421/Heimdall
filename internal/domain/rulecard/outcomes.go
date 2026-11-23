package rulecard

import (
	"cmp"
	"slices"
	"strings"
)

const (
	// maxOutcomeReasons bounds how many dismissal reasons are surfaced per card to its owner.
	maxOutcomeReasons = 5
	// maxOutcomeReasonRunes bounds one surfaced dismissal reason.
	maxOutcomeReasonRunes = 240
	// overrideWatchThreshold flags a card whose findings are frequently waived or bypassed.
	overrideWatchThreshold = 2
	// strongUsefulThreshold marks a card as high-signal when it has enough upheld findings.
	strongUsefulThreshold = 2
	strongPosterior       = 0.72
)

// Outcome status labels shown exclusively to a card's owner.
const (
	OutcomeStatusStrong = "strong"
	OutcomeStatusActive = "active"
	OutcomeStatusWatch  = "watch"
	OutcomeStatusMuted  = "muted"
	OutcomeStatusUnused = "unused"
)

// OutcomeCounts holds the raw review outcome counts for one card belonging to one owner.
type OutcomeCounts struct {
	Kept             int
	Dismissed        int
	Overridden       int
	Ignored          int
	Proven           int
	DismissalReasons []string
}

// Record converts OutcomeCounts into the Bayesian ranker's Record. Overrides count as noise because
// the team explicitly chose not to act on the card's finding.
func (c OutcomeCounts) Record() Record {
	return Record{
		Useful:  c.Kept,
		Noise:   c.Dismissed + c.Overridden,
		Ignored: c.Ignored,
	}
}

// CardOutcome is the owner-only calibration summary for one personal rule card.
// There is intentionally no owner or team field: outcomes are never aggregated across people.
type CardOutcome struct {
	CardName         string   `json:"card_name"`
	Kind             string   `json:"kind,omitempty"`
	Kept             int      `json:"kept"`
	Dismissed        int      `json:"dismissed"`
	Overridden       int      `json:"overridden"`
	Ignored          int      `json:"ignored"`
	Proven           int      `json:"proven"`
	Weight           float64  `json:"weight"`
	Muted            bool     `json:"muted"`
	Status           string   `json:"status"`
	Hint             string   `json:"hint,omitempty"`
	DismissalReasons []string `json:"dismissal_reasons,omitempty"`
}

// SummarizeOutcomes pairs one owner's cards with their past review outcomes and computes
// deterministic ranking weights, mute status, and owner coaching hints.
func SummarizeOutcomes(cards []Card, counts map[string]OutcomeCounts) []CardOutcome {
	out := make([]CardOutcome, 0, len(cards))
	for _, card := range cards {
		if card.Status == StatusRejected {
			continue
		}
		c := counts[card.Name]
		rec := c.Record()
		status, hint := classifyOutcome(card, c, rec)
		out = append(out, CardOutcome{
			CardName:         card.Name,
			Kind:             card.Kind,
			Kept:             c.Kept,
			Dismissed:        c.Dismissed,
			Overridden:       c.Overridden,
			Ignored:          c.Ignored,
			Proven:           c.Proven,
			Weight:           roundTwoDecimals(rec.Weight()),
			Muted:            rec.Muted(),
			Status:           status,
			Hint:             hint,
			DismissalReasons: clipReasons(c.DismissalReasons),
		})
	}
	slices.SortStableFunc(out, func(a, b CardOutcome) int {
		if n := cmp.Compare(outcomeOrder(a.Status), outcomeOrder(b.Status)); n != 0 {
			return n
		}
		return strings.Compare(a.CardName, b.CardName)
	})
	return out
}

func classifyOutcome(card Card, c OutcomeCounts, rec Record) (status, hint string) {
	total := c.Kept + c.Dismissed + c.Overridden + c.Ignored
	switch {
	case rec.Muted():
		return OutcomeStatusMuted, "muted after repeated dismissals; narrow applies_to, tighten check, or archive"
	case total == 0:
		return OutcomeStatusUnused, ""
	case c.Overridden >= overrideWatchThreshold && c.Overridden >= c.Kept:
		return OutcomeStatusWatch, "frequently overridden; tighten check wording or narrow scope"
	case c.Dismissed > c.Kept && c.Dismissed >= 2:
		return OutcomeStatusWatch, "more dismissals than kept findings; review dismissal reasons and refine triggers"
	case c.Kept >= strongUsefulThreshold && rec.posterior() >= strongPosterior:
		if card.EffectiveScope() == ScopePerson && card.Proof == "" {
			return OutcomeStatusStrong, "high signal; add a proof hint so reviewers can prove violations faster"
		}
		return OutcomeStatusStrong, "high signal; consider confirming this card for E3 enforcement"
	default:
		return OutcomeStatusActive, ""
	}
}

func outcomeOrder(status string) int {
	switch status {
	case OutcomeStatusMuted:
		return 0
	case OutcomeStatusWatch:
		return 1
	case OutcomeStatusStrong:
		return 2
	case OutcomeStatusActive:
		return 3
	default:
		return 4
	}
}

func clipReasons(reasons []string) []string {
	if len(reasons) == 0 {
		return nil
	}
	var out []string
	seen := make(map[string]bool, len(reasons))
	for _, r := range reasons {
		clean := strings.TrimSpace(stripFormatControls(r))
		if clean == "" || seen[clean] {
			continue
		}
		seen[clean] = true
		if runes := []rune(clean); len(runes) > maxOutcomeReasonRunes {
			clean = string(runes[:maxOutcomeReasonRunes]) + "..."
		}
		out = append(out, clean)
		if len(out) == maxOutcomeReasons {
			break
		}
	}
	return out
}

func roundTwoDecimals(v float64) float64 {
	if v < 0 {
		return float64(int(v*100-0.5)) / 100
	}
	return float64(int(v*100+0.5)) / 100
}
