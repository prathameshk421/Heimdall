package knowledge

import (
	"context"
	"fmt"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// ChangeLoader reads the stored events of one change. The ingestors use it to find the claim of a finding that is not in
// the events they were given.
type ChangeLoader interface {
	Load(context.Context, id.ChangeID) ([]change.Event, error)
}

// FindingFact is what people need to read about a finding: its name, claim and file.
type FindingFact struct {
	Ref   string
	Claim string
	File  string
}

// FindingFacts maps a finding name such as "F2" to its facts, for one change.
type FindingFacts map[string]FindingFact

// FactsFromEvents collects the facts of the findings filed in events. Events of other changes are skipped.
func FactsFromEvents(changeID id.ChangeID, events []change.Event) FindingFacts {
	facts := FindingFacts{}
	for _, event := range events {
		if event.ChangeID != changeID {
			continue
		}
		record, ok := findingRecordOf(event)
		if !ok || record.Claim == "" {
			continue
		}
		facts[record.Ref.String()] = factOf(record)
	}
	return facts
}

func factOf(record change.FindingRecord) FindingFact {
	fact := FindingFact{Ref: record.Ref.String(), Claim: oneLine(record.Claim)}
	if record.Location != nil {
		fact.File = record.Location.File
	}
	return fact
}

func findingRecordOf(event change.Event) (change.FindingRecord, bool) {
	payload, err := change.Decode(event)
	if err != nil {
		return change.FindingRecord{}, false
	}
	switch p := payload.(type) {
	case change.FindingFiled:
		return change.FindingRecord(p), true
	case change.FindingVerified:
		return change.FindingRecord(p), true
	case change.FindingStateChanged:
		return change.FindingRecord(p), true
	}
	return change.FindingRecord{}, false
}

// Narrate writes one plain sentence about an event: who did what, to which finding, on which file, why, and when. A
// graph that reads "uphold F2" learns nothing, so the episode carries this sentence first. The sentence uses only the
// event and the facts, so a replay gives the same text. A missing fact is left out of the sentence.
func Narrate(event change.Event, facts FindingFacts) string {
	date := event.OccurredAt.UTC().Format("2006-01-02")
	payload, err := change.Decode(event)
	if err != nil {
		return fmt.Sprintf("On %s, %s happened on change %s.", date, event.Type, event.ChangeID)
	}
	switch p := payload.(type) {
	case change.DecisionRecorded:
		return narrateDecision(date, p, facts)
	case change.FindingStateChanged:
		return narrateState(date, change.FindingRecord(p), facts)
	case change.VerdictApplied:
		return fmt.Sprintf("On %s, the resolver applied the verdict %s on case %s after %d re-checked citations.", date, p.Verdict, p.CaseID, len(p.Citations))
	case change.Triggered:
		return fmt.Sprintf("On %s, change %s started on %s and was assigned to %s.", date, p.Ticket, p.Repo, p.Assignee)
	case change.Merged:
		return fmt.Sprintf("On %s, change %s merged at commit %s.", date, event.ChangeID, p.SHA)
	case change.Rejected:
		return fmt.Sprintf("On %s, change %s was rejected by %s. Reason: %s.", date, event.ChangeID, event.Actor.ID, oneLine(p.Reason))
	}
	return fmt.Sprintf("On %s, %s happened on change %s.", date, event.Type, event.ChangeID)
}

func narrateDecision(date string, p change.DecisionRecorded, facts FindingFacts) string {
	who := string(p.Login)
	fact, onFinding := facts[p.Target]
	var head string
	if onFinding || isFindingName(p.Target) {
		head = fmt.Sprintf("On %s, %s %s finding %s", date, who, decisionVerb(p.Kind), p.Target)
		head += findingWhere(fact)
	} else {
		head = fmt.Sprintf("On %s, %s recorded the decision %s about %s", date, who, p.Kind, oneLine(p.Target))
	}
	if fact.Claim != "" {
		head += ": " + fact.Claim
	}
	return head + sentenceEnd(head) + reasonSuffix(p.Reason)
}

func narrateState(date string, record change.FindingRecord, facts FindingFacts) string {
	fact, ok := facts[record.Ref.String()]
	if !ok || fact.Claim == "" {
		fact = factOf(record)
	}
	text := fmt.Sprintf("On %s, finding %s%s reached the state %s", date, record.Ref, findingWhere(fact), record.State)
	if fact.Claim != "" {
		text += ": " + fact.Claim
	}
	return text + sentenceEnd(text)
}

func findingWhere(fact FindingFact) string {
	if fact.File == "" {
		return ""
	}
	return " on " + fact.File
}

func reasonSuffix(reason string) string {
	if strings.TrimSpace(reason) == "" {
		return ""
	}
	return " Reason: " + strings.TrimRight(oneLine(reason), ".") + "."
}

func sentenceEnd(text string) string {
	if strings.HasSuffix(text, ".") {
		return ""
	}
	return "."
}

func isFindingName(s string) bool {
	_, err := id.ParseFindingRef(s)
	return err == nil
}

func decisionVerb(kind change.DecisionKind) string {
	verbs := map[change.DecisionKind]string{
		change.DecisionUphold:     "upheld",
		change.DecisionDismiss:    "dismissed",
		change.DecisionSpecChange: "accepted a spec change for",
		change.DecisionOverride:   "overrode",
		change.DecisionChallenge:  "challenged",
		change.DecisionRemember:   "asked the team to remember",
		change.DecisionForget:     "asked the team to forget",
		change.DecisionIncident:   "recorded an incident for",
	}
	if verb, ok := verbs[kind]; ok {
		return verb
	}
	return "decided " + string(kind) + " on"
}

// oneLine joins the words of s with single spaces, so a multi line claim reads as one sentence.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
