// Package rulecard models a person's review knowledge as small checkable rules and decides which rules a change must
// answer to. Path globs alone miss a rule when the risky code lands in a new file, so a rule also names the symbols and
// the literal text that wake it, the one question that settles it, and how to prove a violation. The ranker also weighs
// how past findings from the rule turned out, so a rule that keeps being dismissed fades without an edit.
//
// A card is data written by or for a person. It never grants a tier, so it can never block a merge by itself: it only
// tells the reviewer what to look at. Proof still comes from a test, a tool or a confirmed decision.
package rulecard

import "fmt"

// The kinds of card.
const (
	KindInvariant  = "invariant"
	KindIncident   = "incident"
	KindOwnership  = "ownership"
	KindConvention = "convention"
	KindJudgement  = "judgement"
	KindUnknown    = "unknown"
)

// Status says whether a card takes part in reviews.
type Status string

// The states of a card. A learned card starts as proposed and acts only after its owner accepts it, so a dismissal
// reason or a remembered note in a chat room cannot steer a reviewer by itself. A rejected card stays on disk so the
// same proposal does not come back.
const (
	StatusActive   Status = "active"
	StatusProposed Status = "proposed"
	StatusRejected Status = "rejected"
)

// Limits of the fields that shape a ranking, so one card cannot match everything or fill a brief.
const (
	MaxSymbols      = 12
	MaxTriggers     = 12
	MaxCheckChars   = 300
	MaxProofChars   = 300
	MinTriggerChars = 3
)

// Card is one parsed rule. A card without AppliesTo, Symbols and Triggers is general and applies to every change.
type Card struct {
	Name      string
	Body      string
	AppliesTo []string
	Symbols   []string
	Triggers  []string
	Check     string // the one question that decides whether the rule holds
	Proof     string // how to turn a violation into a test or a tool result
	Kind      string
	Source    string
	Status    Status
}

// General reports that the card has no scope, so it applies to every change.
func (c Card) General() bool {
	return len(c.AppliesTo) == 0 && len(c.Symbols) == 0 && len(c.Triggers) == 0
}

// Validate returns the first problem with the fields that shape a ranking.
func (c Card) Validate() error {
	switch {
	case len(c.Symbols) > MaxSymbols:
		return fmt.Errorf("symbols has %d entries, at most %d", len(c.Symbols), MaxSymbols)
	case len(c.Triggers) > MaxTriggers:
		return fmt.Errorf("triggers has %d entries, at most %d", len(c.Triggers), MaxTriggers)
	case len(c.Check) > MaxCheckChars:
		return fmt.Errorf("check has %d characters, at most %d", len(c.Check), MaxCheckChars)
	case len(c.Proof) > MaxProofChars:
		return fmt.Errorf("proof has %d characters, at most %d", len(c.Proof), MaxProofChars)
	}
	for _, trigger := range c.Triggers {
		if len(trigger) < MinTriggerChars {
			return fmt.Errorf("trigger %q is shorter than %d characters and would match almost every change", trigger, MinTriggerChars)
		}
	}
	switch c.Status {
	case StatusActive, StatusProposed, StatusRejected:
		return nil
	}
	return fmt.Errorf("status %q is not active, proposed or rejected", c.Status)
}
