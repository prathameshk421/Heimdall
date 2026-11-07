package rulecard

import (
	"errors"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

// BlockFacts is what decides whether a card may stand as E3 evidence for one file. Every value comes from the server:
// the card store, the policy at the merge base and the event history. None comes from the reviewer.
type BlockFacts struct {
	Card      Card
	File      string // the file the finding names
	Confirmed bool   // the owner confirmed exactly this card text
	Approver  bool   // the owner may approve this file in the policy at the merge base
	Record    Record // how findings from this card have turned out for this reviewer
}

// CanBlock reports whether the card may stand as E3 evidence, or why not. A person's rule gains force only for code
// that person may already approve, so a card can never give someone power over code they do not own. A rule that keeps
// being dismissed loses its force by itself, and a person can always dismiss or challenge the finding it led to.
func CanBlock(f BlockFacts) error {
	if err := Confirmable(f.Card); err != nil {
		return err
	}
	switch {
	case !f.Confirmed:
		return errors.New("the owner has not confirmed this text of the card")
	case !appliesToFile(f.Card, f.File):
		return errors.New("the card does not apply to this file")
	case !f.Approver:
		return errors.New("the owner of the card may not approve this file")
	case f.Record.Muted():
		return errors.New("past findings from this card were mostly dismissed")
	}
	return nil
}

// Confirmable reports whether a person may confirm the card at all. Only a scoped, checkable rule can block: a general
// card could hold every change, and a card with no check gives a reviewer nothing to settle. A convention belongs in a
// linter, and an ownership note states no rule.
func Confirmable(card Card) error {
	switch {
	case card.Status != StatusActive:
		return errors.New("the card is not active")
	case card.Kind != KindInvariant && card.Kind != KindIncident && card.Kind != KindJudgement:
		return errors.New("only an invariant, incident or judgement card can block")
	case len(card.AppliesTo) == 0:
		return errors.New("the card needs applies_to, so that it cannot hold every change")
	case card.Check == "":
		return errors.New("the card needs a check, the one question that settles it")
	}
	return nil
}

func appliesToFile(card Card, file string) bool {
	for _, glob := range card.AppliesTo {
		if policy.MatchGlob(glob, file) {
			return true
		}
	}
	return false
}
