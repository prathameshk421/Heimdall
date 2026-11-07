package announce

import (
	"fmt"
	"slices"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

// boardEvent reports whether an event changes what the pull request comment shows.
func boardEvent(t change.EventType) bool {
	return slices.Contains([]change.EventType{
		change.EventPROpened, change.EventPRPushed, change.EventCIReported, change.EventReviewersPicked,
		change.EventFindingFiled, change.EventReviewSubmitted, change.EventReviewUnavailable, change.EventFindingVerified,
		change.EventFindingStateChanged, change.EventApprovalRecorded, change.EventGateEvaluated, change.EventChangeMerged,
	}, t)
}

// maxBoardTest bounds the repro test shown in a comment. The comment is for reading; the whole test ran in the sandbox.
const maxBoardTest = 3000

type boardReproTest struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

type boardLocation struct {
	File  string `json:"file"`
	Lines [2]int `json:"lines"`
}

type boardFinding struct {
	Ref       string          `json:"ref"`
	Tier      string          `json:"tier"`
	Severity  string          `json:"severity"`
	State     string          `json:"state"`
	By        string          `json:"by"`
	Claim     string          `json:"claim"`
	Category  string          `json:"category"`
	Location  *boardLocation  `json:"location"`
	ReproTest *boardReproTest `json:"repro_test"`
}

// board is what the comment shows, folded from the events of one change.
type board struct {
	repo       string
	number     int
	head       string
	acceptance string
	reviewers  []string
	done       map[string]bool
	away       map[string]bool
	findings   []*boardFinding
	byRef      map[string]*boardFinding
	approver   string
	gateSeen   bool
	gateOK     bool
	reasons    []string
	merged     bool
}

func foldBoard(events []change.Event) board {
	b := board{done: map[string]bool{}, away: map[string]bool{}, byRef: map[string]*boardFinding{}}
	for _, ev := range events {
		b.apply(ev)
	}
	return b
}

func (b *board) apply(ev change.Event) {
	if fold, ok := boardFolds()[ev.Type]; ok {
		fold(b, ev)
	}
}

// boardFolds maps each event the comment reads to the function that folds it. It is built per call, so the package
// holds no mutable state.
func boardFolds() map[change.EventType]func(*board, change.Event) {
	return map[change.EventType]func(*board, change.Event){
		change.EventPROpened:            (*board).foldHead,
		change.EventPRPushed:            (*board).foldHead,
		change.EventCIReported:          (*board).foldCI,
		change.EventReviewersPicked:     (*board).foldPicked,
		change.EventReviewSubmitted:     (*board).foldReviewed,
		change.EventReviewUnavailable:   (*board).foldAway,
		change.EventFindingFiled:        (*board).foldFiled,
		change.EventFindingVerified:     (*board).foldState,
		change.EventFindingStateChanged: (*board).foldState,
		change.EventApprovalRecorded:    (*board).foldApproval,
		change.EventGateEvaluated:       (*board).foldGate,
		change.EventChangeMerged:        func(b *board, _ change.Event) { b.merged = true },
	}
}

func (b *board) foldHead(ev change.Event) {
	var p struct {
		Repo    string `json:"repo"`
		Number  int    `json:"number"`
		HeadSHA string `json:"head_sha"`
	}
	if decode(ev, &p) {
		b.repo, b.number, b.head = p.Repo, p.Number, p.HeadSHA
		b.acceptance = ""
	}
}

func (b *board) foldCI(ev change.Event) {
	var p ciReported
	if decode(ev, &p) && p.Check == "heimdall/acceptance" {
		b.acceptance = p.Conclusion
	}
}

func (b *board) foldPicked(ev change.Event) {
	var p struct {
		People []string `json:"people"`
	}
	if decode(ev, &p) {
		b.reviewers = p.People
	}
}

func (b *board) foldReviewed(ev change.Event) {
	var p reviewSubmitted
	if decode(ev, &p) {
		b.done[p.Reviewer] = true
	}
}

func (b *board) foldAway(ev change.Event) {
	var p struct {
		Reviewer string `json:"reviewer"`
	}
	if decode(ev, &p) {
		b.away[p.Reviewer] = true
	}
}

func (b *board) foldFiled(ev change.Event) {
	var f boardFinding
	if decode(ev, &f) {
		b.findings = append(b.findings, &f)
		b.byRef[f.Ref] = &f
	}
}

func (b *board) foldState(ev change.Event) {
	var f boardFinding
	if !decode(ev, &f) {
		return
	}
	if have, ok := b.byRef[f.Ref]; ok {
		have.State = f.State
		if f.Tier != "" {
			have.Tier = f.Tier
		}
	}
}

func (b *board) foldApproval(ev change.Event) {
	var p approvalRecorded
	if decode(ev, &p) {
		b.approver = p.Login
	}
}

func (b *board) foldGate(ev change.Event) {
	var p gateEvaluated
	if decode(ev, &p) {
		b.gateSeen, b.gateOK, b.reasons = true, p.OK, p.Reasons
	}
}

// glossary explains Heimdall's words to someone seeing the comment for the first time.
const glossary = "<details><summary>📖 <b>New to Heimdall? What these words mean</b></summary>\n\n" +
	"- **Finding**: one problem a reviewer reports. **F1**, **F2** are short names for findings, so people can refer to one in a command.\n" +
	"- **Proven by a failing test**: the reviewer wrote a test that shows the bug. Heimdall runs it itself in a sandbox, so the bug is shown, not just claimed. Proven findings block the merge.\n" +
	"- **Reviewer's reasoning**: an argument without a repeatable proof. People see it, but it never blocks a merge alone.\n" +
	"- **The gate**: the required check `heimdall/gate`. A pull request cannot merge until it is green.\n\n</details>\n"

// RenderBoard writes the pull request comment of a change: the verdict, the pipeline, every check and each finding with
// its evidence, in words a person new to Heimdall can follow. The bool is false until the change has a pull request.
func RenderBoard(ticket string, events []change.Event) (string, bool) {
	b := foldBoard(events)
	if b.number == 0 {
		return "", false
	}
	var s strings.Builder
	fmt.Fprintf(&s, "# 🛡️ Heimdall Review · %s\n\n", ticket)
	s.WriteString(b.verdict() + "\n\n")
	s.WriteString(b.pipeline() + "\n\n")
	s.WriteString("## 📋 Merge checklist\n\n| Check | What it means | Status |\n|---|---|---|\n")
	fmt.Fprintf(&s, "| 🧪 **Acceptance tests** | Tests written from the ticket before the code, run on this commit | %s |\n", b.acceptanceCell())
	fmt.Fprintf(&s, "| 🤖 **AI code review** | Independent reviewers read this change on their own | %s |\n", b.reviewersCell())
	fmt.Fprintf(&s, "| 🔎 **Findings** | Problems the reviewers reported | %s |\n", b.findingsCell())
	fmt.Fprintf(&s, "| 👤 **Human approval** | A person with authority over this code approves on GitHub | %s |\n\n", b.approvalCell())
	if len(b.findings) > 0 {
		b.writeFindings(&s)
	}
	b.writeWhy(&s)
	b.writeNext(&s)
	s.WriteString(glossary)
	return s.String(), true
}

func (b board) verdict() string {
	alert := func(kind, text string) string { return "> [!" + kind + "]\n> " + text }
	switch {
	case b.merged:
		return alert("TIP", "🎉 **Merged.** Every merge rule passed.")
	case b.gateSeen && b.gateOK:
		return alert("TIP", "✅ **Ready to merge.** Every merge rule passes.")
	case b.confirmed() > 0:
		return alert("CAUTION", fmt.Sprintf("⛔ **Merge blocked: %s.** Heimdall ran the reviewer's test itself, and it fails on this commit.", count(b.confirmed(), "confirmed bug")))
	case b.blocking() > 0:
		return alert("WARNING", fmt.Sprintf("🔬 **%s with proof.** Heimdall is running each proof itself to confirm it.", count(b.blocking(), "bug reported")))
	case b.reviewing():
		return alert("NOTE", "🤖 **AI review in progress.** Independent reviewers are reading this change now. This comment updates itself.")
	case len(b.findings) > 0:
		return alert("WARNING", "⚠️ **Reviewers raised concerns without proof.** A person should read them before approving.")
	default:
		return alert("NOTE", "⏳ **Checks are running.** This comment updates itself as each one finishes.")
	}
}

func (b board) reviewing() bool {
	return len(b.reviewers) > 0 && len(b.done)+len(b.away) < len(b.reviewers)
}

// pipeline draws where the change stands, as a diagram GitHub renders.
func (b board) pipeline() string {
	reviewDone := len(b.reviewers) > 0 && !b.reviewing()
	class := func(done, bad bool) string {
		switch {
		case bad:
			return "bad"
		case done:
			return "done"
		}
		return "wait"
	}
	lines := []string{
		"```mermaid",
		"flowchart LR",
		`  T["🎫 Ticket"] --> A["🧪 Tests first"] --> P["📬 Pull request"] --> R["🤖 AI review"] --> V["🔬 Proof re-run"] --> H["👤 Human approval"] --> M["🚀 Merge"]`,
		"  classDef done fill:#1a7f37,stroke:#1a7f37,color:#fff",
		"  classDef wait fill:#6e7781,stroke:#6e7781,color:#fff",
		"  classDef bad fill:#cf222e,stroke:#cf222e,color:#fff",
		"  class T,P done",
		"  class A " + class(b.acceptance == "success", b.acceptance == "failure"),
		"  class R " + class(reviewDone, false),
		"  class V " + class(reviewDone && b.blocking() == 0, b.confirmed() > 0),
		"  class H " + class(b.approver != "", false),
		"  class M " + class(b.merged, false),
		"```",
	}
	return strings.Join(lines, "\n")
}

// confirmed counts the findings whose proof Heimdall ran itself, or that a person upheld.
func (b board) confirmed() int {
	n := 0
	for _, f := range b.findings {
		if f.State == "verified" || f.State == "upheld" {
			n++
		}
	}
	return n
}

// blocking counts the findings backed by a repeatable proof that nobody has dismissed.
func (b board) blocking() int {
	n := 0
	for _, f := range b.findings {
		if (f.Tier == "E1" || f.Tier == "E2" || f.Tier == "E3") && f.State != "dismissed" && f.State != "refuted" && f.State != "resolved" {
			n++
		}
	}
	return n
}

func (b board) acceptanceCell() string {
	switch b.acceptance {
	case "":
		return "⏳ Running on this commit"
	case "success":
		return "✅ All pass"
	default:
		return "❌ " + titleCase(b.acceptance)
	}
}

func (b board) reviewersCell() string {
	if len(b.reviewers) == 0 {
		return "⏳ Being picked"
	}
	marks := make([]string, 0, len(b.reviewers))
	for _, r := range b.reviewers {
		switch {
		case b.done[r]:
			marks = append(marks, "✅ "+friendly(r))
		case b.away[r]:
			marks = append(marks, "💤 "+friendly(r)+" (did not answer)")
		default:
			marks = append(marks, "⏳ "+friendly(r))
		}
	}
	return strings.Join(marks, "<br>")
}

func (b board) findingsCell() string {
	if len(b.findings) == 0 {
		if len(b.reviewers) > 0 && !b.reviewing() {
			return "✅ None"
		}
		return "⏳ None yet"
	}
	return fmt.Sprintf("🔎 %s, %d with proof, %d confirmed", count(len(b.findings), "finding"), b.blocking(), b.confirmed())
}

func (b board) approvalCell() string {
	if b.approver != "" {
		return "✅ " + b.approver
	}
	return "⏳ Waiting for an approver"
}

// findingTitle is the first sentence of the claim, short enough for a heading or a table row.
func findingTitle(f *boardFinding) string {
	claim := strings.Join(strings.Fields(f.Claim), " ")
	if claim == "" {
		return "Finding " + f.Ref
	}
	if i := strings.Index(claim, ". "); i > 0 {
		claim = claim[:i]
	}
	return shorten(strings.TrimSuffix(claim, "."), 110)
}

func (b board) writeFindings(s *strings.Builder) {
	s.WriteString("## 🔎 What the reviewers found\n\n| | Finding | Problem | Where | Proof |\n|---|---|---|---|---|\n")
	for _, f := range b.findings {
		where := "-"
		if f.Location != nil {
			where = b.locationLink(*f.Location)
		}
		fmt.Fprintf(s, "| %s | **%s** | %s | %s | %s |\n", severityMark(f.Severity), f.Ref, strings.ReplaceAll(findingTitle(f), "|", "\\|"), where, shortEvidence(f.Tier))
	}
	s.WriteString("\n")
	for _, f := range b.findings {
		fmt.Fprintf(s, "### %s %s: %s\n\n", severityMark(f.Severity), f.Ref, findingTitle(f))
		s.WriteString(b.findingBody(f))
	}
}

func (b board) writeWhy(s *strings.Builder) {
	if !b.gateSeen || b.gateOK {
		return
	}
	waiting, blocked := gateItems(b.reasons)
	if len(waiting)+len(blocked) == 0 {
		return
	}
	s.WriteString("## 🚦 Why this cannot merge yet\n\n")
	for _, item := range blocked {
		fmt.Fprintf(s, "- ❌ %s\n", item)
	}
	for _, item := range waiting {
		fmt.Fprintf(s, "- ⏳ Waiting for %s\n", item)
	}
	s.WriteString("\n")
}

func (b board) writeNext(s *strings.Builder) {
	if b.merged || (b.gateSeen && b.gateOK) || b.blocking() == 0 {
		return
	}
	ref := b.findings[0].Ref
	s.WriteString("## 🧭 What happens next\n\n1. The author fixes the problems and pushes. Heimdall runs every check again on the new commit.\n")
	fmt.Fprintf(s, "2. If a finding is wrong, a person decides by commenting `/heimdall dismiss %s <reason>`, or keeps it with `/heimdall uphold %s`.\n", ref, ref)
	s.WriteString("3. A person approves on GitHub, and the gate turns green.\n\n")
}

// findingBody is the full story of one finding, shared by the summary comment and the comment on the line.
func (b board) findingBody(f *boardFinding) string {
	var s strings.Builder
	if f.Claim != "" {
		fmt.Fprintf(&s, "%s\n\n", f.Claim)
	}
	s.WriteString("| | |\n|---|---|\n")
	fmt.Fprintf(&s, "| **Severity** | %s %s |\n", severityMark(f.Severity), titleCase(f.Severity))
	if f.Category != "" {
		fmt.Fprintf(&s, "| **Type** | %s |\n", titleCase(f.Category))
	}
	if f.Location != nil {
		fmt.Fprintf(&s, "| **Where** | %s |\n", b.locationLink(*f.Location))
	}
	fmt.Fprintf(&s, "| **Evidence** | %s |\n| **Status** | %s |\n| **Found by** | 🤖 %s |\n\n", evidenceLabel(f.Tier), stateLabel(f.State), friendly(f.By))
	if f.ReproTest != nil {
		content := f.ReproTest.Content
		if len(content) > maxBoardTest {
			content = strings.ToValidUTF8(content[:maxBoardTest], "") + "\n// ... cut for this comment"
		}
		fmt.Fprintf(&s, "<details><summary>🧪 <b>See the failing test that proves it</b> (<code>%s</code>)</summary>\n\n```go\n%s\n```\n\n</details>\n\n", f.ReproTest.Name, content)
	}
	return s.String()
}

// InlineFinding is one finding placed on its line of the diff.
type InlineFinding struct {
	Body, Repo, Head, Path string
	Number, Line           int
}

// RenderInline is the comment on the line of one finding. The bool is false when the finding has no text or place.
func RenderInline(events []change.Event, ref string) (InlineFinding, bool) {
	b := foldBoard(events)
	f, found := b.byRef[ref]
	if !found || f.Location == nil || f.Claim == "" || b.number == 0 {
		return InlineFinding{}, false
	}
	return InlineFinding{
		Body:   fmt.Sprintf("### %s Heimdall finding %s: %s\n\n", severityMark(f.Severity), f.Ref, findingTitle(f)) + b.findingBody(f),
		Repo:   b.repo,
		Head:   b.head,
		Path:   f.Location.File,
		Number: b.number,
		Line:   f.Location.Lines[0],
	}, true
}

func (b board) locationLink(l boardLocation) string {
	text := fmt.Sprintf("`%s:%d`", l.File, l.Lines[0])
	if b.repo == "" || b.head == "" {
		return text
	}
	return fmt.Sprintf("[%s](https://github.com/%s/blob/%s/%s#L%d-L%d)", text, b.repo, b.head, l.File, l.Lines[0], l.Lines[1])
}

func severityMark(s string) string {
	switch s {
	case "critical":
		return "🚨"
	case "high":
		return "🔴"
	case "medium":
		return "🟠"
	default:
		return "🟡"
	}
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func shortEvidence(tier string) string {
	switch tier {
	case "E1":
		return "🧪 Failing test"
	case "E2":
		return "🔧 Tool run"
	case "E3":
		return "📚 Team memory"
	default:
		return "💭 Reasoning"
	}
}

func evidenceLabel(tier string) string {
	switch tier {
	case "E1":
		return "🧪 **Proven by a failing test**"
	case "E2":
		return "🔧 **Proven by a tool run**"
	case "E3":
		return "📚 **Backed by team memory**"
	default:
		return "💭 Reviewer's reasoning, not proven"
	}
}

func stateLabel(state string) string {
	switch state {
	case "verified":
		return "✅ Heimdall ran the proof itself and confirmed it"
	case "upheld":
		return "⚖️ Upheld by a person"
	case "dismissed":
		return "🚫 Dismissed by a person"
	case "refuted":
		return "🚫 Heimdall ran the proof and it did not hold"
	case "unverified":
		return "❔ Not verified"
	default:
		return "🔄 Open: Heimdall is running the proof"
	}
}

func chatEvidence(tier string) string {
	switch tier {
	case "E1":
		return "🧪 **Proof:** the reviewer wrote a failing test. Heimdall now runs it in the sandbox."
	case "E2":
		return "🔧 **Proof:** a tool run that Heimdall repeats."
	case "E3":
		return "📚 **Proof:** a decision in team memory."
	default:
		return "💭 **No proof yet:** this is the reviewer's reasoning, so it cannot block the merge alone."
	}
}

// findingBullets is the chat form of the findings a reviewer just filed. It is empty when no finding carries its text.
func findingBullets(history []change.Event, refs []string) []string {
	b := foldBoard(history)
	var out []string
	for _, ref := range refs {
		f, ok := b.byRef[ref]
		if !ok || f.Claim == "" {
			continue
		}
		where := ""
		if f.Location != nil {
			where = fmt.Sprintf("\n📍 `%s:%d`", f.Location.File, f.Location.Lines[0])
		}
		kind := ""
		if f.Category != "" {
			kind = " · " + titleCase(f.Category)
		}
		out = append(out, fmt.Sprintf("\n%s **Finding %s · %s severity%s**%s\n> %s\n%s", severityMark(f.Severity), f.Ref, titleCase(f.Severity), kind, where, shorten(f.Claim, 300), chatEvidence(f.Tier)))
	}
	return out
}

func shorten(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return strings.ToValidUTF8(text[:limit], "") + "…"
}

// prLink is the address of the change's pull request, or empty before it has one.
func prLink(history []change.Event) string {
	b := foldBoard(history)
	if b.number == 0 {
		return ""
	}
	return fmt.Sprintf("https://github.com/%s/pull/%d", b.repo, b.number)
}
