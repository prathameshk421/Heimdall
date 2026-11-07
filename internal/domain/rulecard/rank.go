package rulecard

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

// ChangeFacts is what the ranker knows about one change. Every field comes from the diff or the code graph, so a
// ranking can be replayed from stored inputs.
type ChangeFacts struct {
	Files      []string // files the change touches
	Dependents []string // files outside the change that depend on it, nearest first
	Symbols    []string // symbols the change touches, removes, or calls
	AddedText  string   // the text of the added lines
}

// Record is how a card's past use turned out. Useful findings were upheld or fixed, noise was dismissed by a person,
// and ignored findings were still open when the change merged.
type Record struct {
	Useful, Noise, Ignored int
}

// Ranked is one selected card with its score and the reasons a reviewer sees.
type Ranked struct {
	Card    Card
	Score   float64
	Reasons []string
}

// Muted reports that the record shows a card is mostly noise. A person can see this and keep or fix the card.
func (r Record) Muted() bool {
	return r.Noise >= muteAfterNoise && r.posterior() < mutePosterior
}

// Weight scales a card by its record, from about 0.3 for a noisy card to about 1.4 for a proven card. An unused card
// has weight 1, so a new card neither gains nor loses.
func (r Record) Weight() float64 { return r.posterior() / priorMean }

// posterior is the mean of a Beta distribution with a mildly optimistic prior. An ignored finding counts as half a
// negative, because a merge with an open low-tier finding is weak evidence.
func (r Record) posterior() float64 {
	good := priorGood + float64(r.Useful)
	bad := priorBad + float64(r.Noise) + float64(r.Ignored)/2
	return good / (good + bad)
}

const (
	priorGood      = 2.0
	priorBad       = 1.0
	priorMean      = priorGood / (priorGood + priorBad)
	muteAfterNoise = 3
	mutePosterior  = 0.34

	pathWeight       = 4.0
	dependentWeight  = 1.5
	symbolWeight     = 3.0
	triggerWeight    = 2.5
	generalWeight    = 0.5 // below the widest glob, 0.2 times pathWeight, so any scoped match outranks a general card
	maxSymbolHits    = 3
	maxTriggerHits   = 3
	maxObligations   = 12
	maxReasonsOnCard = 4
)

// Rank scores the active cards against one change and returns the ones that apply, best first, inside budget bytes of
// body text. A card too large for what is left is skipped, and smaller cards still get in. A muted card is left out.
func Rank(cards []Card, facts ChangeFacts, records map[string]Record, budget int) []Ranked {
	added := strings.ToLower(facts.AddedText)
	symbols := symbolSet(facts.Symbols)
	var scored []Ranked
	for _, card := range cards {
		record := records[card.Name]
		if card.Status != StatusActive || record.Muted() {
			continue
		}
		base, reasons := relevance(card, facts, symbols, added)
		if base <= 0 {
			continue
		}
		score := base * kindPrior(card.Kind) * record.Weight()
		scored = append(scored, Ranked{Card: card, Score: score, Reasons: withRecord(reasons, record)})
	}
	slices.SortStableFunc(scored, func(a, b Ranked) int {
		if n := cmp.Compare(b.Score, a.Score); n != 0 {
			return n
		}
		return strings.Compare(a.Card.Name, b.Card.Name)
	})
	return withinBudget(scored, budget)
}

func withinBudget(scored []Ranked, budget int) []Ranked {
	var out []Ranked
	used := 0
	for _, item := range scored {
		if len(out) == maxObligations {
			break
		}
		if used+len(item.Card.Body) > budget {
			continue
		}
		used += len(item.Card.Body)
		out = append(out, item)
	}
	return out
}

func relevance(card Card, facts ChangeFacts, symbols map[string]string, added string) (float64, []string) {
	var score float64
	var reasons []string
	if file, specificity := bestPathMatch(card.AppliesTo, facts.Files); file != "" {
		score += pathWeight * specificity
		reasons = append(reasons, "the change touches "+file)
	} else if file, _ := bestPathMatch(card.AppliesTo, facts.Dependents); file != "" {
		score += dependentWeight
		reasons = append(reasons, file+" depends on this change")
	}
	hits := symbolHits(card.Symbols, symbols)
	if len(hits) > 0 {
		score += symbolWeight * float64(len(hits))
		reasons = append(reasons, "the change involves "+strings.Join(hits, ", "))
	}
	triggers := triggerHits(card.Triggers, added)
	if len(triggers) > 0 {
		score += triggerWeight * float64(len(triggers))
		reasons = append(reasons, "the added lines contain "+strings.Join(triggers, ", "))
	}
	if score == 0 && card.General() {
		return generalWeight, []string{"it applies to every change"}
	}
	return score, reasons
}

// bestPathMatch returns the first file that the most specific glob matches, and that glob's specificity from 0 to 1.
func bestPathMatch(globs, files []string) (string, float64) {
	var bestFile string
	var best float64
	for _, glob := range globs {
		specificity := globSpecificity(glob)
		if specificity <= best {
			continue
		}
		if at := slices.IndexFunc(files, func(f string) bool { return policy.MatchGlob(glob, f) }); at >= 0 {
			bestFile, best = files[at], specificity
		}
	}
	return bestFile, best
}

// globSpecificity gives "**" a small value and an exact path the value 1, in steps of the literal path segments.
func globSpecificity(glob string) float64 {
	if !strings.ContainsAny(glob, "*?[") {
		return 1
	}
	literal := 0
	segments := strings.Split(glob, "/")
	for _, segment := range segments {
		if !strings.ContainsAny(segment, "*?[") {
			literal++
		}
	}
	return min(0.9, 0.2+0.7*float64(literal)/float64(len(segments)))
}

func symbolSet(names []string) map[string]string {
	set := make(map[string]string, len(names))
	for _, name := range names {
		set[strings.ToLower(name)] = name
	}
	return set
}

func symbolHits(wanted []string, symbols map[string]string) []string {
	var hits []string
	for _, name := range wanted {
		if found, ok := symbols[strings.ToLower(name)]; ok && len(hits) < maxSymbolHits {
			hits = append(hits, found)
		}
	}
	return hits
}

func triggerHits(triggers []string, added string) []string {
	var hits []string
	for _, trigger := range triggers {
		if t := strings.ToLower(strings.TrimSpace(trigger)); t != "" && strings.Contains(added, t) && len(hits) < maxTriggerHits {
			hits = append(hits, trigger)
		}
	}
	return hits
}

// kindPrior favours knowledge a generic reviewer cannot have over notes that only inform.
func kindPrior(kind string) float64 {
	switch kind {
	case KindIncident, KindInvariant:
		return 1.2
	case KindJudgement:
		return 1
	case KindConvention:
		return 0.7
	case KindOwnership:
		return 0.5
	case KindUnknown:
		return 0.3
	}
	return 0.8
}

func withRecord(reasons []string, record Record) []string {
	if record.Useful+record.Noise+record.Ignored > 0 {
		reasons = append(reasons, fmt.Sprintf("past findings from this card: %d useful, %d dismissed, %d ignored", record.Useful, record.Noise, record.Ignored))
	}
	if len(reasons) > maxReasonsOnCard {
		reasons = reasons[:maxReasonsOnCard]
	}
	return reasons
}
