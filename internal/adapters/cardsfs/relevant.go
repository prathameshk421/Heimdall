package cardsfs

import (
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/rulecard"
)

// Card is one parsed personal context card. See rulecard for the fields. A card without scope is general.
type Card = rulecard.Card

type frontMatter struct {
	AppliesTo []string `yaml:"applies_to"`
	Symbols   []string `yaml:"symbols"`
	Triggers  []string `yaml:"triggers"`
	Check     string   `yaml:"check"`
	Proof     string   `yaml:"proof"`
	Kind      string   `yaml:"kind"`
	Source    string   `yaml:"source"`
	Status    string   `yaml:"status"`
}

// Parse reads a card file. A markdown card may open with a "---" YAML block naming the paths it applies to
// (repository-root globs, as in heimdall.yaml), its kind and where the knowledge came from. Without that block the
// whole text is the body of a general card. A block that does not parse, or a glob the policy rules refuse, is an error.
func Parse(name, text string) (Card, error) {
	card := Card{Name: name, Status: rulecard.StatusActive}
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		card.Body = strings.TrimSpace(text)
		return card, card.Validate()
	}
	block, body, found := strings.Cut(rest, "\n---\n")
	if !found {
		if trimmed, ok := strings.CutSuffix(strings.TrimRight(rest, "\n"), "\n---"); ok {
			block, body, found = trimmed, "", true
		}
	}
	if !found {
		return Card{}, errors.New("card front matter is not closed with ---")
	}
	var fm frontMatter
	if err := yaml.Unmarshal([]byte(block), &fm); err != nil {
		return Card{}, fmt.Errorf("card front matter: %w", err)
	}
	for _, glob := range fm.AppliesTo {
		if err := policy.ValidateGlob(glob); err != nil {
			return Card{}, fmt.Errorf("card applies_to: %w", err)
		}
	}
	card.Body, card.AppliesTo, card.Kind, card.Source = strings.TrimSpace(body), fm.AppliesTo, fm.Kind, fm.Source
	card.Symbols, card.Triggers, card.Check, card.Proof = fm.Symbols, fm.Triggers, fm.Check, fm.Proof
	if fm.Status != "" {
		card.Status = rulecard.Status(fm.Status)
	}
	if err := card.Validate(); err != nil {
		return Card{}, fmt.Errorf("card %s: %w", name, err)
	}
	return card, nil
}

// All returns every card of the person that reads and parses and has a body, in name order. A bad file is skipped,
// so one broken card never costs a reviewer the rest.
func (s *Store) All(person string) ([]Card, error) {
	names, err := s.List(person)
	if err != nil {
		return nil, err
	}
	var out []Card
	for _, name := range names {
		text, err := s.Read(person, name)
		if err != nil {
			continue
		}
		card, err := Parse(name, text)
		if err != nil || card.Body == "" {
			continue
		}
		out = append(out, card)
	}
	return out, nil
}

// Relevant returns the person's active cards ranked for a change that touches files, inside budget bytes of body
// text. Cards that name symbols or triggers need the code graph, so the caller that has it uses All with rulecard.Rank.
func (s *Store) Relevant(person string, files []string, budget int) ([]Card, error) {
	cards, err := s.All(person)
	if err != nil {
		return nil, err
	}
	ranked := rulecard.Rank(cards, rulecard.ChangeFacts{Files: files}, nil, budget)
	out := make([]Card, 0, len(ranked))
	for _, r := range ranked {
		out = append(out, r.Card)
	}
	return out, nil
}
