package reviewlearn

import (
	"slices"
	"strings"
)

// MuteAfterDrops is how many dropped leads, with no confirmed one, mute a rule for a repository (REVIEW_ARCH 9.1).
const MuteAfterDrops = 5

// RuleTally counts what happened to the leads and findings one rule card or check rule produced.
type RuleTally struct {
	Rule      string
	Confirmed int
	Dropped   int
}

// Weight is the rule's weight in the ranker: the add-one (Laplace) share of confirmed outcomes, so a rule with no history
// weighs one half and a rule is never zero or one from one outcome.
func (t RuleTally) Weight() float64 {
	return float64(t.Confirmed+1) / float64(t.Confirmed+t.Dropped+2)
}

// Muted reports whether a rule is muted: dropped MuteAfterDrops times or more and never confirmed.
func (t RuleTally) Muted() bool {
	return t.Dropped >= MuteAfterDrops && t.Confirmed == 0
}

// Listed returns the muted rules, sorted by name, so a person can delete them.
func Listed(tallies []RuleTally) []RuleTally {
	var out []RuleTally
	for _, t := range tallies {
		if t.Muted() {
			out = append(out, t)
		}
	}
	slices.SortFunc(out, func(a, b RuleTally) int { return strings.Compare(a.Rule, b.Rule) })
	return out
}

// RefuterRecord counts one seat's refutations and how many were later overturned by evidence (REVIEW_ARCH 9.2).
type RefuterRecord struct {
	Seat       string
	Refuted    int
	Overturned int
}

// Hides reports whether the seat's refutations still hide findings. From three refutations on, a seat whose refutations
// were overturned more often than not is shown as an opinion instead: its refutations hide nothing.
func (r RefuterRecord) Hides() bool {
	return r.Refuted < 3 || 2*r.Overturned <= r.Refuted
}

// Dismissal is the category a person gives when dismissing a finding (REVIEW_ARCH 9.3).
type Dismissal string

// The four categories. Intended trade-off and preference become convention proposals for the path's owner.
const (
	DismissalWrong      Dismissal = "wrong"
	DismissalOutOfScope Dismissal = "out_of_scope"
	DismissalTradeOff   Dismissal = "trade_off"
	DismissalPreference Dismissal = "preference"
)

// ProposesConvention reports whether a dismissal of this category is a convention to propose.
func (d Dismissal) ProposesConvention() bool {
	return d == DismissalTradeOff || d == DismissalPreference
}

// Valid reports whether d is one of the four categories.
func (d Dismissal) Valid() bool {
	switch d {
	case DismissalWrong, DismissalOutOfScope, DismissalTradeOff, DismissalPreference:
		return true
	}
	return false
}
