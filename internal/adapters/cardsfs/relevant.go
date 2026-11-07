package cardsfs

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

// Card is one parsed personal context card. A card without applies_to is general and applies to every change.
type Card struct {
	Name      string
	Body      string
	AppliesTo []string
	Kind      string
	Source    string
}

type frontMatter struct {
	AppliesTo []string `yaml:"applies_to"`
	Kind      string   `yaml:"kind"`
	Source    string   `yaml:"source"`
}

// Parse reads a card file. A markdown card may open with a "---" YAML block naming the paths it applies to
// (repository-root globs, as in heimdall.yaml), its kind and where the knowledge came from. Without that block the
// whole text is the body of a general card. A block that does not parse, or a glob the policy rules refuse, is an error.
func Parse(name, text string) (Card, error) {
	card := Card{Name: name}
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		card.Body = strings.TrimSpace(text)
		return card, nil
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
	return card, nil
}

func (c Card) matches(files []string) bool {
	for _, glob := range c.AppliesTo {
		if slices.ContainsFunc(files, func(file string) bool { return policy.MatchGlob(glob, file) }) {
			return true
		}
	}
	return false
}

// Relevant returns the person's cards for a change that touches files: cards whose applies_to matches first, then
// general cards, each group in name order, stopping before the body bytes would pass budget. A card that does not
// parse is skipped, so one bad file never costs a reviewer the rest.
func (s *Store) Relevant(person string, files []string, budget int) ([]Card, error) {
	names, err := s.List(person)
	if err != nil {
		return nil, err
	}
	var specific, general []Card
	for _, name := range names {
		text, err := s.Read(person, name)
		if err != nil {
			continue
		}
		card, err := Parse(name, text)
		if err != nil || card.Body == "" {
			continue
		}
		switch {
		case len(card.AppliesTo) == 0:
			general = append(general, card)
		case card.matches(files):
			specific = append(specific, card)
		}
	}
	var out []Card
	used := 0
	for _, card := range append(specific, general...) {
		if used+len(card.Body) > budget {
			break
		}
		used += len(card.Body)
		out = append(out, card)
	}
	return out, nil
}
