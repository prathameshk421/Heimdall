package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// The review report answers one question for a person: why can this change merge, or why can it not? It is a
// json-render spec: a flat map of elements that name components from the fixed catalog in web/review-report. No model
// writes it. The server builds it from the event log, so the same log always gives the same page. Text from a reviewer
// reaches the page only as string props, which the page shows as text.

// The components of the report catalog. web/review-report/src/catalog.ts must define each of them.
const (
	componentReport   = "Report"
	componentVerdict  = "Verdict"
	componentSection  = "Section"
	componentFinding  = "FindingCard"
	componentRules    = "RuleChecks"
	componentEmpty    = "EmptyNote"
	reportRootElement = "report"
)

// The verdict statuses of the report.
const (
	verdictBlocked = "blocked"
	verdictPassed  = "passed"
	verdictPending = "pending"
)

// Spec is a json-render spec.
type Spec struct {
	Root     string             `json:"root"`
	Elements map[string]Element `json:"elements"`
}

// Element is one node of a Spec.
type Element struct {
	Type     string   `json:"type"`
	Props    any      `json:"props"`
	Children []string `json:"children"`
}

// VerdictProps is the top of the report.
type VerdictProps struct {
	Status   string   `json:"status"`
	Headline string   `json:"headline"`
	Ticket   string   `json:"ticket"`
	Repo     string   `json:"repo"`
	SHA      string   `json:"sha"`
	Reasons  []string `json:"reasons"`
}

// SectionProps titles a group of elements.
type SectionProps struct {
	Title string `json:"title"`
	Note  string `json:"note"`
}

// FindingProps is one finding as a person reads it.
type FindingProps struct {
	Ref      string   `json:"ref"`
	Claim    string   `json:"claim"`
	Severity string   `json:"severity"`
	Tier     string   `json:"tier"`
	State    string   `json:"state"`
	Location string   `json:"location"`
	By       string   `json:"by"`
	Evidence string   `json:"evidence"`
	Cards    []string `json:"cards"`
	Decision string   `json:"decision"`
	Holds    bool     `json:"holds"`
}

// RuleChecksProps is how one reviewer answered the rules of its person.
type RuleChecksProps struct {
	Reviewer string    `json:"reviewer"`
	Rows     []RuleRow `json:"rows"`
}

// RuleRow is one answered rule.
type RuleRow struct {
	Card    string `json:"card"`
	Verdict string `json:"verdict"`
	Note    string `json:"note"`
}

// EmptyProps says that a section has nothing to show.
type EmptyProps struct {
	Text string `json:"text"`
}

// Report builds the review report of a change. The bool is false when the log has no such change.
func (s *Service) Report(ctx context.Context, changeID id.ChangeID) (Spec, bool, error) {
	evs, err := s.history.Load(ctx, changeID)
	if err != nil {
		return Spec{}, false, fmt.Errorf("load %s: %w", changeID, err)
	}
	if len(evs) == 0 {
		return Spec{}, false, nil
	}
	return foldReport(changeID, evs).spec(), true, nil
}

// reportFinding is the latest view of one finding.
type reportFinding struct {
	id       id.FindingID
	props    FindingProps
	severity finding.Severity
	memory   string
	proof    string
}

type reportFolder struct {
	ticket, repo string
	gateSeen     bool
	gate         change.GateEvaluated
	findings     []*reportFinding
	byRef        map[string]*reportFinding
	byID         map[id.FindingID]*reportFinding
	rules        map[string][]finding.RuleCheck
	reviewers    []string
}

func foldReport(changeID id.ChangeID, events []change.Event) *reportFolder {
	f := &reportFolder{ticket: string(changeID), byRef: map[string]*reportFinding{}, byID: map[id.FindingID]*reportFinding{}, rules: map[string][]finding.RuleCheck{}}
	for _, event := range events {
		payload, err := change.Decode(event)
		if err != nil {
			continue // the timeline on the change page names an unreadable event; the report reads what it can
		}
		f.add(event, payload)
	}
	return f
}

func (f *reportFolder) add(event change.Event, payload change.Payload) {
	switch p := payload.(type) {
	case change.Triggered:
		f.repo = string(p.Repo)
	case change.FindingFiled:
		f.file(change.FindingRecord(p))
	case change.FindingVerified:
		f.update(change.FindingRecord(p))
	case change.FindingStateChanged:
		f.update(change.FindingRecord(p))
	case change.DecisionRecorded:
		f.decide(event, p)
	case change.ReviewSubmitted:
		f.review(p)
	case change.GateEvaluated:
		f.gate, f.gateSeen = p, true
	}
}

func (f *reportFolder) file(rec change.FindingRecord) {
	item := &reportFinding{id: rec.FindingID, severity: rec.Severity}
	item.props = FindingProps{
		Ref: rec.Ref.String(), Claim: rec.Claim, Severity: string(rec.Severity), Tier: string(rec.Tier),
		State: string(rec.State), By: string(rec.By), Cards: slices.Clone(rec.CardRefs),
	}
	if rec.Location != nil {
		item.props.Location = locationText(*rec.Location)
	}
	if rec.MemoryRef != nil {
		item.memory = *rec.MemoryRef
	}
	switch {
	case rec.ReproTest != nil:
		item.proof = "repro"
	case rec.ToolResult != nil:
		item.proof = "tool"
	}
	f.findings = append(f.findings, item)
	f.byRef[item.props.Ref] = item
	f.byID[rec.FindingID] = item
}

func (f *reportFolder) update(rec change.FindingRecord) {
	item := f.byID[rec.FindingID]
	if item == nil {
		return
	}
	item.props.Tier, item.props.State = string(rec.Tier), string(rec.State)
}

func (f *reportFolder) decide(event change.Event, p change.DecisionRecorded) {
	item := f.byRef[p.Target]
	if item == nil || (p.Kind != change.DecisionUphold && p.Kind != change.DecisionDismiss) {
		return
	}
	verb := "upheld"
	if p.Kind == change.DecisionDismiss {
		verb = "dismissed"
	}
	item.props.Decision = fmt.Sprintf("%s %s this on %s.", p.Login, verb, event.OccurredAt.UTC().Format("2006-01-02"))
	if reason := strings.TrimSpace(p.Reason); reason != "" {
		item.props.Decision += " " + reason
	}
}

// review keeps the latest answers of a reviewer, because a re-review after a push replaces the earlier one.
func (f *reportFolder) review(p change.ReviewSubmitted) {
	name := string(p.Reviewer)
	if !slices.Contains(f.reviewers, name) {
		f.reviewers = append(f.reviewers, name)
	}
	f.rules[name] = p.RuleChecks
}

// evidenceText says in plain words what stands behind a finding, and so whether it can block.
func (item *reportFinding) evidenceText() string {
	switch item.props.Tier {
	case string(finding.TierE1):
		return "A test fails on this commit. Heimdall ran it."
	case string(finding.TierE2):
		return "A static check reports this. Heimdall ran it again."
	case string(finding.TierE3):
		if name, _, ok := finding.CardEvidence(item.memory); ok {
			return "A rule its owner confirmed: " + name + ". The owner may approve this file."
		}
		return "A confirmed team decision."
	}
	return "Reviewer reasoning only. This cannot block alone."
}

// spec lays the folded log out as elements. Keys are stable, so the same log gives the same bytes.
func (f *reportFolder) spec() Spec {
	b := specBuilder{elements: map[string]Element{}}
	sections := []string{b.add("verdict", componentVerdict, f.verdict(), nil)}
	sections = append(sections, f.findingSections(&b)...)
	sections = append(sections, f.ruleSection(&b))
	b.add(reportRootElement, componentReport, struct{}{}, sections)
	return Spec{Root: reportRootElement, Elements: b.elements}
}

type specBuilder struct{ elements map[string]Element }

func (b *specBuilder) add(key, component string, props any, children []string) string {
	if children == nil {
		children = []string{}
	}
	b.elements[key] = Element{Type: component, Props: props, Children: children}
	return key
}

func (f *reportFolder) verdict() VerdictProps {
	v := VerdictProps{Status: verdictPending, Ticket: f.ticket, Repo: f.repo, Headline: "The gate has not run yet.", Reasons: []string{}}
	if !f.gateSeen {
		return v
	}
	v.SHA = shortSHA(f.gate.SHA)
	if f.gate.OK {
		v.Status, v.Headline = verdictPassed, "The gate passed. This change can merge."
		return v
	}
	v.Status, v.Headline = verdictBlocked, "The gate is holding this merge."
	for _, reason := range f.gate.Reasons {
		v.Reasons = append(v.Reasons, f.withRefs(reason))
	}
	return v
}

// withRefs swaps finding ids for the names people use: a person never sees a ULID.
func (f *reportFolder) withRefs(text string) string {
	for _, item := range f.findings {
		text = strings.ReplaceAll(text, string(item.id), item.props.Ref)
	}
	return text
}

// holds reads the gate's own record, so the page cannot disagree with the gate about what blocked it.
func (f *reportFolder) holds(item *reportFinding) bool {
	if !f.gateSeen || f.gate.OK {
		return false
	}
	return slices.ContainsFunc(f.gate.Reasons, func(reason string) bool { return strings.Contains(reason, string(item.id)) })
}

func (f *reportFolder) findingSections(b *specBuilder) []string {
	var holding, open, closed []string
	for _, item := range f.ordered() {
		item.props.Holds = f.holds(item)
		item.props.Evidence = item.evidenceText()
		key := b.add("finding-"+item.props.Ref, componentFinding, item.props, nil)
		switch {
		case item.props.Holds:
			holding = append(holding, key)
		case finding.State(item.props.State).Terminal():
			closed = append(closed, key)
		default:
			open = append(open, key)
		}
	}
	var out []string
	if len(f.findings) == 0 {
		empty := b.add("no-findings", componentEmpty, EmptyProps{Text: "No reviewer has filed a finding."}, nil)
		return []string{b.add("section-findings", componentSection, SectionProps{Title: "Findings"}, []string{empty})}
	}
	add := func(key, title, note string, children []string) {
		if len(children) > 0 {
			out = append(out, b.add(key, componentSection, SectionProps{Title: title, Note: note}, children))
		}
	}
	add("section-holding", "What holds the merge", "The gate names these findings.", holding)
	add("section-open", "Open, not blocking", "A person should read these. They do not hold the merge.", open)
	add("section-closed", "Settled", "Fixed, dismissed or withdrawn.", closed)
	return out
}

// ordered puts the most severe findings first, then keeps the order they were filed in.
func (f *reportFolder) ordered() []*reportFinding {
	out := slices.Clone(f.findings)
	slices.SortStableFunc(out, func(a, b *reportFinding) int {
		switch {
		case a.severity == b.severity:
			return 0
		case a.severity.AtLeast(b.severity):
			return -1
		}
		return 1
	})
	return out
}

func (f *reportFolder) ruleSection(b *specBuilder) string {
	var children []string
	for _, name := range f.reviewers {
		checks := f.rules[name]
		if len(checks) == 0 {
			continue
		}
		rows := make([]RuleRow, 0, len(checks))
		for _, check := range checks {
			rows = append(rows, RuleRow{Card: check.Card, Verdict: string(check.Verdict), Note: check.Note})
		}
		children = append(children, b.add("rules-"+name, componentRules, RuleChecksProps{Reviewer: name, Rows: rows}, nil))
	}
	if len(children) == 0 {
		children = []string{b.add("no-rules", componentEmpty, EmptyProps{Text: "No reviewer answered a personal rule on this change."}, nil)}
	}
	return b.add("section-rules", componentSection, SectionProps{Title: "Personal rules checked", Note: "Each reviewer speaks for one person and answers that person's rules."}, children)
}

// locationText writes a path and its lines the way people cite them: one line as file:12, a range as file:12-19.
func locationText(l finding.Location) string {
	if l.Lines[0] == l.Lines[1] {
		return fmt.Sprintf("%s:%d", l.File, l.Lines[0])
	}
	return fmt.Sprintf("%s:%d-%d", l.File, l.Lines[0], l.Lines[1])
}

func shortSHA(sha id.SHA) string {
	if len(sha) > 7 {
		return string(sha[:7])
	}
	return string(sha)
}

// MarshalReport returns the bytes the page reads.
func MarshalReport(spec Spec) ([]byte, error) {
	raw, err := json.Marshal(spec)
	if err != nil {
		return nil, fmt.Errorf("encode report: %w", err)
	}
	return raw, nil
}
