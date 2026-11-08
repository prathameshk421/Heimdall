package rulecard

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func blockable() BlockFacts {
	return BlockFacts{
		Card: Card{Name: "cap.md", Body: "rule", Kind: KindInvariant, Status: StatusActive, AppliesTo: []string{"internal/pay/**"}, Check: "Does the cap count past refunds?"},
		File: "internal/pay/refund.go", Confirmed: true, Approver: true,
	}
}

func TestCanBlockNeedsEveryCondition(t *testing.T) {
	require.NoError(t, CanBlock(blockable()))
	for name, edit := range map[string]func(*BlockFacts){
		"not confirmed":       func(f *BlockFacts) { f.Confirmed = false },
		"not an approver":     func(f *BlockFacts) { f.Approver = false },
		"file out of scope":   func(f *BlockFacts) { f.File = "web/app.ts" },
		"muted by its record": func(f *BlockFacts) { f.Record = Record{Noise: 5} },
		"proposed":            func(f *BlockFacts) { f.Card.Status = StatusProposed },
		"rejected":            func(f *BlockFacts) { f.Card.Status = StatusRejected },
		"general":             func(f *BlockFacts) { f.Card.AppliesTo = nil },
		"no check":            func(f *BlockFacts) { f.Card.Check = "" },
		"a convention":        func(f *BlockFacts) { f.Card.Kind = KindConvention },
		"an ownership note":   func(f *BlockFacts) { f.Card.Kind = KindOwnership },
		"no kind":             func(f *BlockFacts) { f.Card.Kind = "" },
	} {
		facts := blockable()
		edit(&facts)
		assert.Error(t, CanBlock(facts), name)
	}
}

func TestATriggerCannotWidenWhatACardCanBlock(t *testing.T) {
	facts := blockable()
	facts.Card.Triggers = []string{"refund("}
	facts.File = "web/refund.ts"
	assert.Error(t, CanBlock(facts), "a trigger wakes a card for review, but only applies_to scopes its force")
}
