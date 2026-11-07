package reviewbrief

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/rulecard"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Caps of the sections, so the brief never crowds out the diff.
const (
	changeMapBytes   = 6 << 10
	obligationBytes  = 12 << 10
	memoryBytes      = 4 << 10
	maxPriorFindings = 6
	maxPriorChars    = 300
	maxMemoryItems   = 8
	maxMemoryChars   = 400
	maxQuestionBytes = 1024
	maxEntities      = 20
	maxEntityBytes   = 128
	maxQuerySymbols  = 12
	maxQueryFiles    = 8
)

const (
	graphUnavailable  = "Code graph unavailable: use read_code."
	cardsUnavailable  = "Personal cards unavailable: use read_card."
	priorUnavailable  = "Prior findings unavailable."
	memoryUnavailable = "Team memory unavailable: carry on without it."
	noMemory          = "Team memory has nothing on these symbols. Silence is not a pass."
	obligationsFooter = "Answer each one in rule_checks: violated, holds or not_applicable. A card says where to look. It is not evidence."
)

func changeMapSection(src sources) string {
	if src.graphFailed {
		return graphUnavailable
	}
	if src.impact == nil {
		return ""
	}
	return src.impact.Render(changeMapBytes)
}

// obligations ranks the person's cards for this change and renders them. The bool is false when the cards failed.
func (c *Compiler) obligations(ctx context.Context, req Request, src sources) (string, []string, bool) {
	if c.cfg.Cards == nil {
		return "", nil, true
	}
	if req.Person == "" {
		return cardsUnavailable, nil, false
	}
	cards, err := c.cfg.Cards.All(req.Person)
	if err != nil {
		c.warn(ctx, "personal cards unavailable for brief", req, err)
		return cardsUnavailable, nil, false
	}
	var records map[string]rulecard.Record
	if src.history != nil {
		records = src.history.CardRecords(req.Agent)
	}
	ranked := rulecard.Rank(cards, changeFacts(req, src.impact), records, obligationBytes)
	for i := range ranked {
		ranked[i].Confirmed = c.cfg.Cards.Confirmed(req.Person, ranked[i].Card.Name)
	}
	text, names := renderObligations(ranked, src.impact, obligationBytes)
	return text, names, true
}

func changeFacts(req Request, impact Impact) rulecard.ChangeFacts {
	facts := rulecard.ChangeFacts{Files: req.Task.Inputs.FilesChanged, AddedText: addedText(req.Diff)}
	if impact != nil {
		facts.Dependents = impact.DependentFiles()
		facts.Symbols = impact.Symbols()
	}
	return facts
}

// addedText returns the added lines of a unified diff, without their plus sign.
func addedText(diff string) string {
	var b strings.Builder
	for line := range strings.SplitSeq(diff, "\n") {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			b.WriteString(line[1:])
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// renderObligations renders the ranked cards inside maxBytes in rank order, and returns the names of the cards it shows.
// A card that does not fit ends the list, and the text says how many it left out.
func renderObligations(ranked []rulecard.Ranked, impact Impact, maxBytes int) (string, []string) {
	if len(ranked) == 0 {
		return "", nil
	}
	var b strings.Builder
	var names []string
	shown := 0
	for _, item := range ranked {
		card := renderCard(item, impact)
		if b.Len()+len(card)+len(obligationsFooter)+omittedNoteBytes > maxBytes {
			break
		}
		b.WriteString(card)
		shown++
		if !informational(item.Card) {
			names = append(names, item.Card.Name)
		}
	}
	if left := len(ranked) - shown; left > 0 {
		fmt.Fprintf(&b, "%d more cards did not fit. Call read_card to list them.\n\n", left)
	}
	b.WriteString(obligationsFooter)
	return b.String(), names
}

// omittedNoteBytes reserves room for the note about the cards that did not fit.
const omittedNoteBytes = 64

func renderCard(item rulecard.Ranked, impact Impact) string {
	card := item.Card
	var b strings.Builder
	fmt.Fprintf(&b, "## %s", card.Name)
	if card.Kind != "" {
		fmt.Fprintf(&b, " (%s)", card.Kind)
	}
	if card.Source != "" {
		fmt.Fprintf(&b, " [from: %s]", card.Source)
	}
	b.WriteByte('\n')
	if stale(card, impact) {
		fmt.Fprintf(&b, "POSSIBLY STALE: none of %s has a definition at head. Check that the rule still applies.\n", strings.Join(card.Symbols, ", "))
	}
	if len(item.Reasons) > 0 {
		fmt.Fprintf(&b, "WHY: %s\n", strings.Join(item.Reasons, "; "))
	}
	if informational(card) {
		b.WriteString("NOTE: background only, nothing to answer.\n")
	} else {
		fmt.Fprintf(&b, "CHECK: %s\n", orDefault(card.Check, "state what would violate this"))
		if card.Proof != "" {
			fmt.Fprintf(&b, "PROOF: %s\n", card.Proof)
		}
		if item.Confirmed {
			fmt.Fprintf(&b, "CONFIRMED: if you show a violation in a file this card applies to, cite memory_ref card:%s#<that file>. It can block.\n", card.Name)
		}
	}
	b.WriteString(card.Body + "\n\n")
	return b.String()
}

// informational reports that a card gives background with no rule to check, such as who owns what.
func informational(card rulecard.Card) bool {
	return card.Check == "" && (card.Kind == rulecard.KindOwnership || card.Kind == rulecard.KindUnknown)
}

// stale reports that a card names symbols and none of them is defined at head. It needs a complete index, because a
// truncated index cannot show that a definition is absent.
func stale(card rulecard.Card, impact Impact) bool {
	if impact == nil || impact.Partial() || len(card.Symbols) == 0 {
		return false
	}
	for _, symbol := range card.Symbols {
		if impact.Defined(symbol) {
			return false
		}
	}
	return true
}

func orDefault(text, fallback string) string {
	if strings.TrimSpace(text) == "" {
		return fallback
	}
	return text
}

func priorSection(src sources, req Request) string {
	if src.learnFailed {
		return priorUnavailable
	}
	if src.history == nil {
		return ""
	}
	ref := req.Task.Change
	found := src.history.PriorFindings(change.Repo(ref.Repo), req.Task.Inputs.FilesChanged, id.ChangeID(ref.ID), maxPriorFindings)
	return renderPrior(found)
}

func renderPrior(found []PriorFinding) string {
	if len(found) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Past findings on these files from other changes. They are hints, not evidence.\n")
	b.WriteString("Do not file a dismissed one again without new evidence. Check an upheld one for a regression.\n")
	for i, f := range found {
		if i == maxPriorFindings {
			break
		}
		fmt.Fprintf(&b, "- %s %s on %s (%s, %s): %s", f.Change, f.Ref, f.File, f.Category, f.Outcome, clip(f.Claim, maxPriorChars))
		if f.Reason != "" {
			fmt.Fprintf(&b, " Reason given: %s", clip(f.Reason, maxPriorChars))
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// memory prefetches team memory for the top symbols and files. The bool is false when memory failed.
func (c *Compiler) memory(ctx context.Context, req Request, impact Impact) (string, bool) {
	if c.cfg.Memory == nil {
		return "", true
	}
	query, ok := memoryQuery(req, impact)
	if !ok {
		return "", true
	}
	ctx, cancel := context.WithTimeout(ctx, c.cfg.MemoryWait)
	defer cancel()
	ref := req.Task.Change
	items, err := c.cfg.Memory.Ask(ctx, req.Agent, req.Task.TaskID, change.Repo(ref.Repo), ref.BaseSHA, query)
	if err != nil {
		c.warn(ctx, "team memory unavailable for brief", req, err)
		return memoryUnavailable, false
	}
	if len(items) == 0 {
		return noMemory, true
	}
	return renderMemory(items), true
}

// memoryQuery builds the question and the entities inside the limits that ports.MemoryQuery and ask_knowledge state.
// The caller never sets the groups: the knowledge service resolves them. The bool is false when there is nothing to ask.
func memoryQuery(req Request, impact Impact) (ports.MemoryQuery, bool) {
	var names []string
	if impact != nil {
		names = firstN(impact.Symbols(), maxQuerySymbols)
	}
	names = append(names, firstN(req.Task.Inputs.FilesChanged, maxQueryFiles)...)
	entities := make([]string, 0, maxEntities)
	seen := map[string]bool{}
	for _, name := range names {
		if name = strings.TrimSpace(name); name == "" || len(name) > maxEntityBytes || seen[name] || len(entities) == maxEntities {
			continue
		}
		seen[name] = true
		entities = append(entities, name)
	}
	if len(entities) == 0 {
		return ports.MemoryQuery{}, false
	}
	question := "What decisions, incidents and constraints apply to " + strings.Join(entities, ", ")
	question = clipBytes(question, maxQuestionBytes)
	return ports.MemoryQuery{Question: question, Entities: entities, Groups: []string{}, Limit: maxMemoryItems}, true
}

func firstN(names []string, n int) []string {
	return names[:min(n, len(names))]
}

func renderMemory(items []ports.MemoryItem) string {
	var b strings.Builder
	b.WriteString("Only a ref labelled confirmed can be cited as memory_ref (E3). Any other label is a hint.\n")
	for i, item := range items {
		line := fmt.Sprintf("- %s (%s): %s\n", item.Ref, item.Label, clip(item.Text, maxMemoryChars))
		if i == maxMemoryItems || b.Len()+len(line) > memoryBytes {
			break
		}
		b.WriteString(line)
	}
	return strings.TrimRight(b.String(), "\n")
}

func recordSection(history History, agent id.AgentName) string {
	if history == nil {
		return ""
	}
	cal := history.Calibration(agent)
	if cal.Useful+cal.Noise+cal.Ignored == 0 {
		return ""
	}
	line := fmt.Sprintf("Your record: %d useful, %d dismissed, %d ignored, %.0f%% resolved.", cal.Useful, cal.Noise, cal.Ignored, cal.ResolutionRate*100)
	if len(cal.NoisyCategories) == 0 {
		return line + "\nNo category of yours is noisy."
	}
	return line + "\nNoisy categories: " + strings.Join(cal.NoisyCategories, ", ") + ". Be stricter before filing these."
}

// clip returns text on one line, cut to at most limit characters.
func clip(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	return string([]rune(text)[:limit]) + "..."
}

// clipBytes cuts text to at most limit bytes at a character boundary.
func clipBytes(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}
