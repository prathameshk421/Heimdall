package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/cardsfs"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/policyyaml"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/postgres"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/knowledge"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/reviewlearn"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/rulecard"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/verify"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// cardRecords returns how findings from each card have turned out for one reviewer.
type cardRecords func(ctx context.Context, agent id.AgentName) (map[string]rulecard.Record, error)

// cardEvidence answers the E3 re-query for a memory_ref that cites a rule card, and passes every other ref to team
// memory. It runs when a finding is filed and again on every verification, so a card that was edited, muted or moved
// out of its owner's approval stops blocking on the next push. The caller gives the merge base: a change cannot edit
// its own approver list to escape a card or to gain one.
type cardEvidence struct {
	next    memoryRecheck
	store   *cardsfs.Store
	scopes  knowledge.PolicyScopes
	records cardRecords
}

// newCardEvidence wraps next. Without a cards directory no card can be cited, and next is returned as it is.
func newCardEvidence(d deps, ad adapters, history reviewlearn.History, clk ports.Clock, next memoryRecheck) (memoryRecheck, error) {
	if d.configuration.CardsDir == "" {
		return next, nil
	}
	store, err := cardsfs.Open(d.configuration.CardsDir)
	if err != nil {
		return nil, fmt.Errorf("open personal cards for evidence: %w", err)
	}
	learning, err := reviewlearn.NewService(history, clk, learningTTL)
	if err != nil {
		return nil, fmt.Errorf("build review history for evidence: %w", err)
	}
	records := func(ctx context.Context, agent id.AgentName) (map[string]rulecard.Record, error) {
		snapshot, err := learning.Snapshot(ctx)
		if err != nil {
			return nil, err
		}
		return snapshot.CardRecords(agent), nil
	}
	return cardEvidence{next: next, store: store, scopes: knowledge.PolicyScopes{Code: ad.vcs, Decode: policyyaml.Decode}, records: records}, nil
}

// newEvidenceRecheck builds the E3 re-query the verify effect uses: team memory, plus confirmed cards.
func newEvidenceRecheck(d deps, ad adapters, store *postgres.Store, clk ports.Clock) (memoryRecheck, error) {
	search, err := newKnowledgeSearch(d, ad, clk, store)
	if err != nil {
		return nil, err
	}
	return newCardEvidence(d, ad, store, clk, search)
}

// Recheck reports a cited card as confirmed only when rulecard.CanBlock allows it. A read failure is an error, so the
// outbox retries it and no failure can confirm a finding.
func (c cardEvidence) Recheck(ctx context.Context, agent id.AgentName, repo change.Repo, base id.SHA, ref string) (verify.MemoryState, error) {
	name, file, isCard := finding.CardEvidence(ref)
	if !isCard {
		return c.next.Recheck(ctx, agent, repo, base, ref)
	}
	person, pol, err := c.scopes.PersonForAgent(ctx, agent, repo, base)
	if err != nil {
		return verify.MemoryState{}, fmt.Errorf("resolve the person of card evidence: %w", err)
	}
	text, err := c.store.Read(person, name)
	if errors.Is(err, cardsfs.ErrNotFound) {
		return verify.MemoryState{}, nil
	}
	if err != nil {
		return verify.MemoryState{}, fmt.Errorf("read card evidence: %w", err)
	}
	records, err := c.records(ctx, agent)
	if err != nil {
		return verify.MemoryState{}, fmt.Errorf("read the record of card evidence: %w", err)
	}
	facts := rulecard.BlockFacts{File: file, Confirmed: c.store.Confirmed(person, name), Approver: pol.ApproverOf(person, file), Record: records[name]}
	return verify.MemoryState{Found: true, Confirmed: cardHolds(name, text, facts)}, nil
}

// cardHolds parses the card and applies the rule. A card that no longer parses has no force.
func cardHolds(name, text string, facts rulecard.BlockFacts) bool {
	card, err := cardsfs.Parse(name, text)
	if err != nil {
		return false
	}
	facts.Card = card
	return rulecard.CanBlock(facts) == nil
}
