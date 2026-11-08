package rulecard

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func active(name string, edit func(*Card)) Card {
	card := Card{Name: name, Body: "rule " + name, Kind: KindInvariant, Status: StatusActive}
	if edit != nil {
		edit(&card)
	}
	return card
}

func names(ranked []Ranked) []string {
	out := make([]string, 0, len(ranked))
	for _, item := range ranked {
		out = append(out, item.Card.Name)
	}
	return out
}

func TestRankWakesACardByTriggerWhenTheRiskyCodeLandsOutsideItsGlobs(t *testing.T) {
	card := active("refund.md", func(c *Card) {
		c.AppliesTo = []string{"internal/payments/**"}
		c.Triggers = []string{"Refund("}
	})
	got := Rank([]Card{card}, ChangeFacts{Files: []string{"internal/newsvc/x.go"}, AddedText: "func f() { pay.refund(a) }"}, nil, 1000)
	require.Equal(t, []string{"refund.md"}, names(got))
	assert.Contains(t, got[0].Reasons[0], "Refund(")
}

func TestRankLeavesOutACardWithNoMatch(t *testing.T) {
	card := active("refund.md", func(c *Card) { c.AppliesTo = []string{"internal/payments/**"} })
	assert.Empty(t, Rank([]Card{card}, ChangeFacts{Files: []string{"README.md"}}, nil, 1000))
}

func TestRankWakesACardWhenADependentFileMatchesItsGlobs(t *testing.T) {
	card := active("ledger.md", func(c *Card) { c.AppliesTo = []string{"internal/ledger/**"} })
	got := Rank([]Card{card}, ChangeFacts{Files: []string{"api/types.go"}, Dependents: []string{"internal/ledger/read.go"}}, nil, 1000)
	require.Equal(t, []string{"ledger.md"}, names(got))
	assert.Contains(t, got[0].Reasons[0], "depends on this change")
}

func TestRankPutsAnExactPathAboveAWideGlobAboveAGeneralCard(t *testing.T) {
	wide := active("wide.md", func(c *Card) { c.AppliesTo = []string{"**"} })
	exact := active("exact.md", func(c *Card) { c.AppliesTo = []string{"api/openapi.yaml"} })
	general := active("general.md", nil)
	got := Rank([]Card{general, wide, exact}, ChangeFacts{Files: []string{"api/openapi.yaml"}}, nil, 1000)
	assert.Equal(t, []string{"exact.md", "wide.md", "general.md"}, names(got))
}

func TestRankPrefersAnIncidentOverAnOwnershipNoteWithTheSameScope(t *testing.T) {
	note := active("a-owner.md", func(c *Card) { c.AppliesTo = []string{"x/**"}; c.Kind = KindOwnership })
	scar := active("b-scar.md", func(c *Card) { c.AppliesTo = []string{"x/**"}; c.Kind = KindIncident })
	got := Rank([]Card{note, scar}, ChangeFacts{Files: []string{"x/y.go"}}, nil, 1000)
	assert.Equal(t, []string{"b-scar.md", "a-owner.md"}, names(got))
}

func TestRankSkipsACardTooLargeForTheBudgetAndKeepsSmallerOnes(t *testing.T) {
	huge := active("a-huge.md", func(c *Card) {
		c.AppliesTo = []string{"x/y.go"}
		c.Body = strings.Repeat("x", 900)
	})
	small := active("b-small.md", func(c *Card) { c.AppliesTo = []string{"x/**"} })
	got := Rank([]Card{huge, small}, ChangeFacts{Files: []string{"x/y.go"}}, nil, 100)
	assert.Equal(t, []string{"b-small.md"}, names(got), "the old selector stopped at the first card over budget")
}

func TestRankIgnoresProposedAndRejectedCardsUntilTheOwnerAccepts(t *testing.T) {
	proposed := active("p.md", func(c *Card) { c.Status = StatusProposed })
	rejected := active("r.md", func(c *Card) { c.Status = StatusRejected })
	assert.Empty(t, Rank([]Card{proposed, rejected}, ChangeFacts{Files: []string{"a.go"}}, nil, 1000))
}

func TestRankMovesACardByItsRecordAndDropsAMutedCard(t *testing.T) {
	proven := active("b-proven.md", func(c *Card) { c.AppliesTo = []string{"x/**"} })
	noisy := active("a-noisy.md", func(c *Card) { c.AppliesTo = []string{"x/**"} })
	records := map[string]Record{"b-proven.md": {Useful: 5}, "a-noisy.md": {Useful: 1, Noise: 2}}
	got := Rank([]Card{noisy, proven}, ChangeFacts{Files: []string{"x/y.go"}}, records, 1000)
	assert.Equal(t, []string{"b-proven.md", "a-noisy.md"}, names(got))

	records["a-noisy.md"] = Record{Noise: 5}
	got = Rank([]Card{noisy, proven}, ChangeFacts{Files: []string{"x/y.go"}}, records, 1000)
	assert.Equal(t, []string{"b-proven.md"}, names(got))
}

func TestRecordDoesNotMuteAfterTwoDismissals(t *testing.T) {
	assert.False(t, Record{Noise: 2}.Muted(), "two dismissals are too few to silence what a person wrote")
	assert.True(t, Record{Noise: 4}.Muted())
	assert.False(t, Record{Useful: 6, Noise: 3}.Muted(), "a card with many useful findings stays even after dismissals")
}

func TestAnIgnoredFindingCountsLessThanADismissal(t *testing.T) {
	assert.Greater(t, Record{Ignored: 2}.Weight(), Record{Noise: 2}.Weight())
}

func TestRankIsCappedSoTheBriefStaysReadable(t *testing.T) {
	var cards []Card
	for _, letter := range "abcdefghijklmnopqrst" {
		cards = append(cards, active(string(letter)+".md", func(c *Card) { c.AppliesTo = []string{"**"} }))
	}
	assert.Len(t, Rank(cards, ChangeFacts{Files: []string{"a.go"}}, nil, 100000), maxObligations)
}

func TestValidateRefusesATriggerThatMatchesEverything(t *testing.T) {
	card := active("x.md", func(c *Card) { c.Triggers = []string{"if"} })
	require.Error(t, card.Validate())
}
