package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/cardsfs"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/cardgen"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/rulecard"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/pack"
)

const longCardChars = 1500

// cardReport is the result of checking one card. A problem makes the check fail. A warning does not.
type cardReport struct {
	problems []string
	warnings []string
}

// cardFields are the front matter fields the checks and the status rewrite read.
type cardFields struct {
	AppliesTo []string `yaml:"applies_to"`
	Symbols   []string `yaml:"symbols"`
	Triggers  []string `yaml:"triggers"`
	Check     string   `yaml:"check"`
	Proof     string   `yaml:"proof"`
	Kind      string   `yaml:"kind"`
	Source    string   `yaml:"source"`
	Status    string   `yaml:"status"`
}

// splitFrontMatter cuts a card into its YAML block and body. ok is false when the card has no block.
func splitFrontMatter(text string) (block, body string, ok bool) {
	rest, found := strings.CutPrefix(text, "---\n")
	if !found {
		return "", text, false
	}
	block, body, found = strings.Cut(rest, "\n---\n")
	if !found {
		if trimmed, closed := strings.CutSuffix(strings.TrimRight(rest, "\n"), "\n---"); closed {
			return trimmed, "", true
		}
		return "", text, false
	}
	return block, body, true
}

func readCardFields(text string) (cardFields, error) {
	var f cardFields
	block, _, ok := splitFrontMatter(text)
	if !ok {
		return f, nil
	}
	if err := yaml.Unmarshal([]byte(block), &f); err != nil {
		return f, fmt.Errorf("front matter: %w", err)
	}
	return f, nil
}

// checkCardText runs every local check on one card. The server repeats the format and redaction checks.
func checkCardText(name, text string) cardReport {
	var r cardReport
	if len(text) > cardsfs.MaxCardBytes {
		r.problems = append(r.problems, fmt.Sprintf("card is %d bytes, at most %d", len(text), cardsfs.MaxCardBytes))
		return r
	}
	if strings.TrimSpace(text) == "" {
		r.problems = append(r.problems, "card is empty")
		return r
	}
	if _, err := cardsfs.Parse(name, text); err != nil {
		r.problems = append(r.problems, err.Error())
		return r
	}
	if _, found := pack.RedactReport(text); len(found) > 0 {
		kinds := make([]string, 0, len(found))
		for _, f := range found {
			kinds = append(kinds, f.Kind)
		}
		r.problems = append(r.problems, "looks like it holds a credential or personal data ("+strings.Join(kinds, ", ")+")")
	}
	f, err := readCardFields(text)
	if err != nil {
		r.problems = append(r.problems, err.Error())
		return r
	}
	card := rulecard.Card{AppliesTo: f.AppliesTo, Symbols: f.Symbols, Triggers: f.Triggers, Check: f.Check, Proof: f.Proof,
		Kind: f.Kind, Source: f.Source, Status: rulecard.Status(firstNonEmpty(f.Status, string(rulecard.StatusActive)))}
	if err := card.Validate(); err != nil {
		r.problems = append(r.problems, err.Error())
	}
	r.warnings = cardWarnings(text, f, card)
	return r
}

func cardWarnings(text string, f cardFields, card rulecard.Card) []string {
	var w []string
	if f.Check == "" && card.General() {
		w = append(w, "vague card: no check and no scope (applies_to, symbols or triggers)")
	}
	if (f.Kind == rulecard.KindInvariant || f.Kind == rulecard.KindIncident) && (f.Check == "" || (len(f.Triggers) == 0 && len(f.Symbols) == 0)) {
		w = append(w, f.Kind+" card needs a check and triggers or symbols")
	}
	if len(text) > longCardChars*2 {
		w = append(w, fmt.Sprintf("card is %d characters, aim for under %d", len(text), longCardChars))
	}
	return w
}

var statusLine = regexp.MustCompile(`^status:.*$`)

// setStatus rewrites the status line of a card, or adds it. The rest of the card stays as it was.
func setStatus(text, status string) string {
	block, body, ok := splitFrontMatter(text)
	if !ok {
		return "---\nstatus: " + status + "\n---\n" + text
	}
	lines := strings.Split(strings.TrimRight(block, "\n"), "\n")
	replaced := false
	for i, l := range lines {
		if statusLine.MatchString(l) {
			lines[i], replaced = "status: "+status, true
		}
	}
	if !replaced {
		lines = append(lines, "status: "+status)
	}
	return "---\n" + strings.Join(lines, "\n") + "\n---\n" + body
}

func readScanState(path string) (scanState, error) {
	data, err := readInDir(filepath.Dir(path), filepath.Base(path))
	if errors.Is(err, os.ErrNotExist) {
		return scanState{}, fmt.Errorf("%s not found: run heimdall cards scan first", path)
	}
	if err != nil {
		return scanState{}, fmt.Errorf("read scan state: %w", err)
	}
	var state scanState
	if err := json.Unmarshal(data, &state); err != nil {
		return scanState{}, fmt.Errorf("parse scan state: %w", err)
	}
	return state, nil
}

func buildPromptText(state scanState, req promptRequest, person, absDir string) (string, error) {
	if person == "" {
		return "", errors.New("no person: pass --person or set git user.name")
	}
	signals, err := filepath.Abs(filepath.Join(req.work, signalsFileName))
	if err != nil {
		return "", fmt.Errorf("resolve signals path: %w", err)
	}
	existing, _ := cardFileNames(absDir)
	return cardgen.BuildPrompt(cardgen.Input{Person: person, RepoName: state.RepoName, RepoRoot: state.Repo, OutDir: absDir,
		Identity: state.Identity, Docs: state.Docs, Sessions: state.Sessions, Leads: state.Leads, Existing: existing,
		Interview: req.interview, Signals: signals, PRLinks: state.PRLinks})
}
