package present

import (
	"slices"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
)

// Scope is where a finding sits relative to the change.
type Scope string

// The three scopes of REVIEW_ARCH 7.1.
const (
	ScopeChanged     Scope = "changed"
	ScopeCaused      Scope = "caused"
	ScopePreExisting Scope = "pre_existing"
)

// Range is an inclusive, 1-based line range.
type Range [2]int

// ScopeOf returns the scope of loc. changed maps each file to the ranges the diff added or changed; caused is the
// symbol graph's answer for an unchanged location.
func ScopeOf(loc finding.Location, changed map[string][]Range, caused bool) Scope {
	for _, r := range changed[loc.File] {
		if loc.Lines[0] <= r[1] && r[0] <= loc.Lines[1] {
			return ScopeChanged
		}
	}
	if caused {
		return ScopeCaused
	}
	return ScopePreExisting
}

// Item is one finding as the presenter sees it.
type Item struct {
	ID       string
	Blocking bool
	Severity finding.Severity
	Tier     finding.Tier
	Scope    Scope
}

// Plan is one round's arrangement. Each input item is in exactly one list.
type Plan struct {
	Comments  []Item // line comments, at most the cap
	Collapsed []Item // over the cap: listed in the summary
	Noticed   []Item // pre_existing: listed under "Noticed, not from this change", never commented
}

// Arrange splits items into the plan. Comments are the blocking items first, then by severity and tier, at most limit.
func Arrange(items []Item, limit int) Plan {
	var p Plan
	var live []Item
	for _, it := range items {
		if it.Scope == ScopePreExisting {
			p.Noticed = append(p.Noticed, it)
		} else {
			live = append(live, it)
		}
	}
	slices.SortStableFunc(live, compare)
	n := min(limit, len(live))
	p.Comments = live[:n]
	p.Collapsed = live[n:]
	return p
}

// compare orders the more important item first: blocking, then severity, then tier.
func compare(a, b Item) int {
	if a.Blocking != b.Blocking {
		if a.Blocking {
			return -1
		}
		return 1
	}
	if c := sevCompare(a.Severity, b.Severity); c != 0 {
		return c
	}
	return tierCompare(a.Tier, b.Tier)
}

// sevCompare is -1 when a is more severe than b, 1 when less, 0 when equal or not ordered.
func sevCompare(a, b finding.Severity) int {
	switch {
	case a.AtLeast(b) && !b.AtLeast(a):
		return -1
	case b.AtLeast(a) && !a.AtLeast(b):
		return 1
	}
	return 0
}

// tierCompare is -1 when a is a higher tier than b, 1 when lower, 0 when equal or not ordered.
func tierCompare(a, b finding.Tier) int {
	switch {
	case a.AtLeast(b) && !b.AtLeast(a):
		return -1
	case b.AtLeast(a) && !a.AtLeast(b):
		return 1
	}
	return 0
}
