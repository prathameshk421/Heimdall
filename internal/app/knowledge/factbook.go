package knowledge

import (
	"context"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// factBook finds the facts of findings for the events of one Record call. It starts from the events it holds. For a
// change whose event needs a finding it does not hold, it loads the stored change once.
type factBook struct {
	events []change.Event
	loader ChangeLoader
	facts  map[id.ChangeID]FindingFacts
	loaded map[id.ChangeID]bool
}

func newFactBook(events []change.Event, loader ChangeLoader) *factBook {
	return &factBook{events: events, loader: loader, facts: map[id.ChangeID]FindingFacts{}, loaded: map[id.ChangeID]bool{}}
}

// factsFor returns the facts of the change of event. A failed load is not an error: the summary then uses what the event
// itself says.
func (b *factBook) factsFor(ctx context.Context, event change.Event) FindingFacts {
	facts, ok := b.facts[event.ChangeID]
	if !ok {
		facts = FactsFromEvents(event.ChangeID, b.events)
		b.facts[event.ChangeID] = facts
	}
	if b.loader == nil || b.loaded[event.ChangeID] || !needsFinding(event) || hasTarget(event, facts) {
		return facts
	}
	b.loaded[event.ChangeID] = true
	stored, err := b.loader.Load(ctx, event.ChangeID)
	if err != nil {
		return facts
	}
	for ref, fact := range FactsFromEvents(event.ChangeID, stored) {
		if _, held := facts[ref]; !held {
			facts[ref] = fact
		}
	}
	return facts
}

func needsFinding(event change.Event) bool {
	return event.Type == change.EventDecisionRecorded || event.Type == change.EventFindingStateChanged
}

// hasTarget reports whether facts already hold the finding that event is about.
func hasTarget(event change.Event, facts FindingFacts) bool {
	payload, err := change.Decode(event)
	if err != nil {
		return true
	}
	switch p := payload.(type) {
	case change.DecisionRecorded:
		_, ok := facts[p.Target]
		return ok || !isFindingName(p.Target)
	case change.FindingStateChanged:
		_, ok := facts[p.Ref.String()]
		return ok
	}
	return true
}
