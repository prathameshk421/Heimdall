package reviewlearn

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/rulecard"
)

// Proposal is a rule card a person's decision suggests. The card is not active: its owner must accept it.
type Proposal struct {
	Repo    change.Repo
	BaseSHA id.SHA
	Login   string
	Name    string
	Text    string
}

const (
	maxClaimChars = 240
	maxCheckChars = rulecard.MaxCheckChars
)

// propose builds the cards that the human decisions of one change suggest. Only decisions of a human actor count. A
// decision by an agent never reaches this code, and a decision by any other actor is dropped, so a model cannot write
// its own rules.
func (f *changeFold) propose() []Proposal {
	var out []Proposal
	for _, d := range f.decisions {
		if d.event.Actor.Kind != change.ActorHuman {
			continue
		}
		if p, ok := f.proposalFor(d); ok {
			out = append(out, p)
		}
	}
	return out
}

func (f *changeFold) proposalFor(d decision) (Proposal, bool) {
	var card cardText
	switch d.payload.Kind {
	case change.DecisionDismiss:
		r, ok := f.findings[d.payload.Target]
		if !ok || strings.TrimSpace(d.payload.Reason) == "" {
			return Proposal{}, false
		}
		card = judgementCard(r, d.payload.Reason)
	case change.DecisionRemember:
		card = f.noteCard(d.payload, rulecard.KindInvariant)
	case change.DecisionIncident:
		card = f.noteCard(d.payload, rulecard.KindIncident)
	case change.DecisionReview, change.DecisionUphold, change.DecisionSpecChange, change.DecisionOverride,
		change.DecisionWaiveReviewer, change.DecisionReject, change.DecisionApproveTests, change.DecisionStart,
		change.DecisionChallenge, change.DecisionForget, change.DecisionResume, change.DecisionAnswer:
		return Proposal{}, false
	}
	if card.body == "" {
		return Proposal{}, false
	}
	source := fmt.Sprintf("decision %s on %s", shortID(string(d.event.ID)), f.id)
	return Proposal{Repo: f.repo, BaseSHA: f.base, Login: string(d.payload.Login), Name: cardName(d.event.ID), Text: card.render(source)}, true
}

type cardText struct {
	kind      string
	appliesTo []string
	check     string
	body      string
}

func judgementCard(r *row, reason string) cardText {
	claim := strings.TrimRight(clip(oneLine(r.claim), maxClaimChars), ".")
	var files []string
	if r.file != "" {
		files = []string{r.file}
	}
	return cardText{
		kind:      rulecard.KindJudgement,
		appliesTo: files,
		check:     clip("Is this the kind of finding the owner dismissed: "+claim+"?", maxCheckChars),
		body:      "I do not want this flagged: " + claim + ". Reason: " + strings.TrimRight(oneLine(reason), ".") + ".",
	}
}

// noteCard turns a remember or incident decision into a card scoped to the files of the change's findings.
func (f *changeFold) noteCard(d change.DecisionRecorded, kind string) cardText {
	text := oneLine(d.Target)
	if r, ok := f.findings[d.Target]; ok {
		text = oneLine(r.claim)
	}
	if text == "" {
		return cardText{}
	}
	body := strings.TrimRight(clip(text, maxClaimChars), ".")
	if reason := oneLine(d.Reason); reason != "" {
		body += ". Reason: " + strings.TrimRight(reason, ".") + "."
	}
	return cardText{kind: kind, appliesTo: f.findingFiles(), check: clip("Does this change break this? "+clip(text, maxClaimChars), maxCheckChars), body: body}
}

func (f *changeFold) findingFiles() []string {
	var files []string
	for _, r := range f.findings {
		if r.file != "" && !slices.Contains(files, r.file) {
			files = append(files, r.file)
		}
	}
	slices.Sort(files)
	return files
}

// render writes the card file in the format of the rule card parser, with status proposed.
func (c cardText) render(source string) string {
	var b strings.Builder
	b.WriteString("---\n")
	if len(c.appliesTo) > 0 {
		b.WriteString("applies_to: " + jsonList(c.appliesTo) + "\n")
	}
	b.WriteString("check: " + jsonString(c.check) + "\n")
	b.WriteString("kind: " + c.kind + "\n")
	b.WriteString("source: " + jsonString(source) + "\n")
	b.WriteString("status: proposed\n")
	b.WriteString("---\n")
	b.WriteString(c.body + "\n")
	return b.String()
}

// cardName is deterministic, so the same decision always proposes the same file.
func cardName(eventID change.EventID) string {
	sum := sha256.Sum256([]byte(eventID))
	return "learned-" + hex.EncodeToString(sum[:])[:12] + ".md"
}

func shortID(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// jsonString quotes s as a JSON string, which is also a valid YAML double quoted string.
func jsonString(s string) string { return jsonText(s) }

func jsonList(items []string) string { return jsonText(items) }

func jsonText(v any) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return `""`
	}
	return strings.TrimSpace(b.String())
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func clip(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	return string([]rune(s)[:limit-3]) + "..."
}

func sortProposals(ps []Proposal) {
	sort.SliceStable(ps, func(i, j int) bool { return ps[i].Name < ps[j].Name })
}
