package finding

import (
	"fmt"
	"slices"
)

// Tier is how strong a finding's evidence is (TECH_SPEC 11.1). E1 is the strongest.
type Tier string

// The evidence tiers. E1 to E3 can block a merge; E4 never does (invariant 3).
const (
	TierE1 Tier = "E1" // a repro test the sandbox ran on head and saw fail
	TierE2 Tier = "E2" // a deterministic tool result whose location matches
	TierE3 Tier = "E3" // confirmed memory or a prior decision, re-queried at verification
	TierE4 Tier = "E4" // reasoning, a pack reference, or observed memory: shown as unverified
)

// Severity is how bad a finding says the problem is.
type Severity string

// The severities, lowest first.
const (
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// Confidence is the reporter's confidence in a finding. It feeds consensus scoring.
type Confidence string

// The confidence levels.
const (
	ConfidenceHigh   Confidence = "high"
	ConfidenceMedium Confidence = "medium"
	ConfidenceLow    Confidence = "low"
)

// Category is what kind of problem a finding is about.
type Category string

// The categories.
const (
	CategoryCorrectness Category = "correctness"
	CategorySecurity    Category = "security"
	CategoryCompat      Category = "compat"
	CategoryOps         Category = "ops"
	CategorySpecDrift   Category = "spec_drift"
	CategoryTests       Category = "tests"
	CategoryStyle       Category = "style"
)

// State is a finding's place in its lifecycle (TECH_SPEC 11.2).
type State string

// The finding states.
const (
	StateProposed    State = "proposed"
	StateOpen        State = "open"
	StateUnverified  State = "unverified"
	StateDisputed    State = "disputed"
	StateWithdrawn   State = "withdrawn"
	StateEscalated   State = "escalated"
	StateUpheld      State = "upheld"
	StateDismissed   State = "dismissed"
	StateSpecChanged State = "spec_changed"
	StateFixClaimed  State = "fix_claimed"
	StateVerifying   State = "verifying"
	StateResolved    State = "resolved"
)

// parseEnum returns s as a T when it is one of valid, and an error that lists the choices otherwise.
func parseEnum[T ~string](kind, s string, valid ...T) (T, error) {
	v := T(s)
	if !slices.Contains(valid, v) {
		return "", fmt.Errorf("%s %q is not one of %v", kind, s, valid)
	}
	return v, nil
}

// ParseTier validates s.
func ParseTier(s string) (Tier, error) {
	return parseEnum("tier", s, TierE1, TierE2, TierE3, TierE4)
}

// ParseSeverity validates s.
func ParseSeverity(s string) (Severity, error) {
	return parseEnum("severity", s, SeverityLow, SeverityMedium, SeverityHigh, SeverityCritical)
}

// ParseConfidence validates s.
func ParseConfidence(s string) (Confidence, error) {
	return parseEnum("confidence", s, ConfidenceHigh, ConfidenceMedium, ConfidenceLow)
}

// RuleVerdict is what a reviewer found when it checked one rule card.
type RuleVerdict string

// The rule verdicts.
const (
	RuleViolated      RuleVerdict = "violated"
	RuleHolds         RuleVerdict = "holds"
	RuleNotApplicable RuleVerdict = "not_applicable"
)

// ParseRuleVerdict validates s.
func ParseRuleVerdict(s string) (RuleVerdict, error) {
	return parseEnum("rule verdict", s, RuleViolated, RuleHolds, RuleNotApplicable)
}

// UnmarshalText validates while decoding.
func (v *RuleVerdict) UnmarshalText(b []byte) error {
	r, err := ParseRuleVerdict(string(b))
	*v = r
	return err
}

// ParseCategory validates s.
func ParseCategory(s string) (Category, error) {
	return parseEnum("category", s, CategoryCorrectness, CategorySecurity, CategoryCompat, CategoryOps, CategorySpecDrift, CategoryTests, CategoryStyle)
}

// ParseState validates s.
func ParseState(s string) (State, error) {
	return parseEnum("state", s, StateProposed, StateOpen, StateUnverified, StateDisputed, StateWithdrawn, StateEscalated,
		StateUpheld, StateDismissed, StateSpecChanged, StateFixClaimed, StateVerifying, StateResolved)
}

// UnmarshalText validates while decoding, so a value outside the set cannot enter the domain through JSON.
func (t *Tier) UnmarshalText(b []byte) error { v, err := ParseTier(string(b)); *t = v; return err }

// UnmarshalText validates while decoding.
func (s *Severity) UnmarshalText(b []byte) error {
	v, err := ParseSeverity(string(b))
	*s = v
	return err
}

// UnmarshalText validates while decoding.
func (c *Confidence) UnmarshalText(b []byte) error {
	v, err := ParseConfidence(string(b))
	*c = v
	return err
}

// UnmarshalText validates while decoding.
func (c *Category) UnmarshalText(b []byte) error {
	v, err := ParseCategory(string(b))
	*c = v
	return err
}

// UnmarshalText validates while decoding.
func (s *State) UnmarshalText(b []byte) error { v, err := ParseState(string(b)); *s = v; return err }

// AtLeast reports whether t is as strong as other. E1 is strongest; an invalid tier is never at least anything.
func (t Tier) AtLeast(other Tier) bool {
	a, b := tierRank(t), tierRank(other)
	return a > 0 && b > 0 && a >= b
}

func tierRank(t Tier) int {
	switch t {
	case TierE1:
		return 4
	case TierE2:
		return 3
	case TierE3:
		return 2
	case TierE4:
		return 1
	}
	return 0
}

// Valid reports whether s is one of the four severities.
func (s Severity) Valid() bool { return severityRank(s) > 0 }

// AtLeast reports whether s is as severe as other. An invalid severity is never at least anything.
func (s Severity) AtLeast(other Severity) bool {
	a, b := severityRank(s), severityRank(other)
	return a > 0 && b > 0 && a >= b
}

func severityRank(s Severity) int {
	switch s {
	case SeverityLow:
		return 1
	case SeverityMedium:
		return 2
	case SeverityHigh:
		return 3
	case SeverityCritical:
		return 4
	}
	return 0
}

// Weight is the confidence's contribution to a consensus score (TECH_SPEC 11.2b): high 1.0, medium 0.5, low 0.2.
func (c Confidence) Weight() float64 {
	switch c {
	case ConfidenceHigh:
		return 1.0
	case ConfidenceMedium:
		return 0.5
	case ConfidenceLow:
		return 0.2
	}
	return 0
}

// CanBlock reports whether a finding in this state can be part of the blocking set (TECH_SPEC 11.1): open, disputed,
// escalated, upheld, fix_claimed and verifying. Whether it does also depends on its tier and severity.
func (s State) CanBlock() bool {
	switch s {
	case StateOpen, StateDisputed, StateEscalated, StateUpheld, StateFixClaimed, StateVerifying:
		return true
	case StateProposed, StateUnverified, StateWithdrawn, StateDismissed, StateSpecChanged, StateResolved:
		return false
	}
	return false
}

// Terminal reports whether no further transition leaves this state.
func (s State) Terminal() bool {
	switch s {
	case StateWithdrawn, StateDismissed, StateSpecChanged, StateResolved:
		return true
	case StateProposed, StateOpen, StateUnverified, StateDisputed, StateEscalated, StateUpheld, StateFixClaimed, StateVerifying:
		return false
	}
	return false
}
