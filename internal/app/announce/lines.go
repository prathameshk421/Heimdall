// Package announce tells people in chat what happened to a change, one short plain line per milestone. The lines are
// the product's voice in Discord, Telegram and Mattermost: they name the ticket, say what happened in words a
// developer would use, and, when a person must act, say what to type.
package announce

import (
	"fmt"
	"slices"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// Audience says which feed a line belongs to.
type Audience string

const (
	// AudienceTests is the story of the tests: written, checked, committed.
	AudienceTests Audience = "tests"
	// AudienceReview is the story of the pull request: CI, reviewers, findings, the merge check.
	AudienceReview Audience = "review"
	// AudienceBoth is for a line that matters to every feed, such as a change that was closed.
	AudienceBoth Audience = "both"
)

// Line is one announcement.
type Line struct {
	Audience Audience
	Text     string
}

// The narrator reads only the few fields it names. The domain payload types validate every field on decode, and a
// line must never be lost because a field it does not use is odd.
type (
	taskAssigned   struct{ Agent, Role string }
	testsSubmitted struct {
		Refs  []string            `json:"refs"`
		ACMap map[string][]string `json:"ac_map"`
	}
	testsRejected struct{ Reasons []string }
	testsVerified struct {
		NeedsSignoff bool `json:"needs_signoff"`
	}
	testsCommitted  struct{ SHA string }
	stepEscalated   struct{ Step, Reason string }
	branchEnsured   struct{ Branch string }
	prOpened        struct{ Number int }
	ciReported      struct{ Check, SHA, Conclusion string }
	reviewersPicked struct{ People []string }
	reviewSubmitted struct {
		Reviewer    string
		FindingRefs []string `json:"finding_refs"`
	}
	findingFiled struct {
		Ref      string
		Severity string
	}
	findingState struct {
		Ref   string
		State string
		By    string
	}
	approvalRecorded struct{ Login string }
	gateEvaluated    struct {
		OK      bool
		Reasons []string
	}
)

const (
	maxReasonChars = 160
	maxGateReasons = 5
)

// narrator turns one event into its line.
type narrator func(history []change.Event, ev change.Event, k string) (Line, bool)

// narrators maps each event that has a line to the function that writes it. It is built per call so the package
// holds no mutable state.
func narrators() map[change.EventType]narrator {
	return map[change.EventType]narrator{
		change.EventTaskAssigned:        narrateAssigned,
		change.EventTestsSubmitted:      narrateSubmitted,
		change.EventTestsRejected:       narrateRejected,
		change.EventTestsVerified:       narrateVerified,
		change.EventTestsCommitted:      narrateCommitted,
		change.EventStepEscalated:       narrateEscalated,
		change.EventChangeRejected:      narrateClosed,
		change.EventPROpened:            narratePROpened,
		change.EventCIReported:          narrateCI,
		change.EventReviewersPicked:     narratePicked,
		change.EventReviewSubmitted:     narrateReviewDone,
		change.EventFindingStateChanged: narrateFindingState,
		change.EventApprovalRecorded:    narrateApproval,
		change.EventGateEvaluated:       narrateGate,
		change.EventChangeMerged:        narrateMerged,
	}
}

// Narrate returns the line for ev, or false when ev needs none. history holds the events of the change before ev.
func Narrate(history []change.Event, ev change.Event) (Line, bool) {
	write, ok := narrators()[ev.Type]
	if !ok {
		return Line{}, false
	}
	return write(history, ev, string(ev.ChangeID))
}

func narrateSubmitted(_ []change.Event, ev change.Event, k string) (Line, bool) {
	var p testsSubmitted
	if !decode(ev, &p) {
		return Line{}, false
	}
	return tests("🔎 %s: tests submitted (%s, covering %s). I'm running them on the current code to check that they fail for the right reason.",
		k, count(len(p.Refs), "file"), joinNames(sortedKeys(p.ACMap))), true
}

func narrateRejected(_ []change.Event, ev change.Event, k string) (Line, bool) {
	var p testsRejected
	if !decode(ev, &p) {
		return Line{}, false
	}
	return tests("❌ %s: these tests did not pass my check. %s The Test Agent will try again.", k, humanReasons(p.Reasons)), true
}

func narrateVerified(_ []change.Event, ev change.Event, k string) (Line, bool) {
	var p testsVerified
	if !decode(ev, &p) {
		return Line{}, false
	}
	if p.NeedsSignoff {
		return tests("⏸ %s: tests verified. A person has to approve them before I commit. Reply: heimdall approve-tests", k), true
	}
	return tests("✅ %s: tests verified. They fail on the current code, as they should, and gave the same result on every run.", k), true
}

func narrateCommitted(history []change.Event, ev change.Event, k string) (Line, bool) {
	var p testsCommitted
	if !decode(ev, &p) {
		return Line{}, false
	}
	return tests("✅ %s: tests committed to branch %s (commit %s).", k, branchOf(history), short(p.SHA)), true
}

func narrateEscalated(_ []change.Event, ev change.Event, k string) (Line, bool) {
	var p stepEscalated
	if !decode(ev, &p) {
		return Line{}, false
	}
	return tests("⚠️ %s: I'm stuck and need a person. %s. When it is fixed, reply: heimdall resume %s", k, strings.TrimSuffix(p.Reason, "."), p.Step), true
}

func narrateClosed(_ []change.Event, _ change.Event, k string) (Line, bool) {
	return Line{Audience: AudienceBoth, Text: fmt.Sprintf("🚫 %s: this change was closed.", k)}, true
}

func narrateAssigned(history []change.Event, ev change.Event, k string) (Line, bool) {
	var p taskAssigned
	if !decode(ev, &p) {
		return Line{}, false
	}
	// A task assigned again after a retry is already explained by the rejection line.
	for _, h := range history {
		var prev taskAssigned
		if h.Type == change.EventTaskAssigned && decode(h, &prev) && prev.Role == p.Role {
			return Line{}, false
		}
	}
	lines := map[string]Line{
		string(workflow.RoleTestAuthor): tests("🧪 %s is in progress. The Heimdall Test Agent is writing failing tests for the ticket's acceptance criteria.", k),
		string(workflow.RoleReviewer):   review("🔍 %s: %s is reviewing the pull request now.", k, friendly(p.Agent)),
		string(workflow.RoleResolver):   review("⚖️ %s: %s is weighing a challenged finding.", k, friendly(p.Agent)),
	}
	line, ok := lines[p.Role]
	return line, ok
}

func narratePROpened(_ []change.Event, ev change.Event, k string) (Line, bool) {
	var p prOpened
	if !decode(ev, &p) {
		return Line{}, false
	}
	return review("📬 %s: pull request #%d is open. I'm waiting for CI and the reviewers.", k, p.Number), true
}

func narrateCI(_ []change.Event, ev change.Event, k string) (Line, bool) {
	var p ciReported
	if !decode(ev, &p) {
		return Line{}, false
	}
	if p.Conclusion == "success" {
		return review("✅ %s: CI passed on %s.", k, short(p.SHA)), true
	}
	if p.Conclusion == "failure" {
		return review("❌ %s: CI failed on %s (check: %s).", k, short(p.SHA), p.Check), true
	}
	return Line{}, false
}

func narratePicked(_ []change.Event, ev change.Event, k string) (Line, bool) {
	var p reviewersPicked
	if !decode(ev, &p) || len(p.People) == 0 {
		return Line{}, false
	}
	return review("🔍 %s: reviewers picked: %s.", k, joinNames(p.People)), true
}

func narrateApproval(_ []change.Event, ev change.Event, k string) (Line, bool) {
	var p approvalRecorded
	if !decode(ev, &p) {
		return Line{}, false
	}
	return review("✅ %s: %s approved the pull request.", k, p.Login), true
}

func narrateMerged(_ []change.Event, _ change.Event, k string) (Line, bool) {
	return review("🎉 %s is merged.", k), true
}

func narrateReviewDone(history []change.Event, ev change.Event, k string) (Line, bool) {
	var p reviewSubmitted
	if !decode(ev, &p) {
		return Line{}, false
	}
	if len(p.FindingRefs) == 0 {
		return review("🔍 %s: %s finished the review and found no issues.", k, friendly(p.Reviewer)), true
	}
	severities := map[string]string{}
	for _, h := range history {
		var f findingFiled
		if h.Type == change.EventFindingFiled && decode(h, &f) {
			severities[f.Ref] = f.Severity
		}
	}
	items := make([]string, 0, len(p.FindingRefs))
	for _, n := range p.FindingRefs {
		if sev, ok := severities[n]; ok {
			items = append(items, fmt.Sprintf("%s (%s)", n, sev))
		} else {
			items = append(items, n)
		}
	}
	return review("🔍 %s: %s finished the review and found %s: %s.", k, friendly(p.Reviewer), count(len(items), "issue"), joinNames(items)), true
}

func narrateFindingState(_ []change.Event, ev change.Event, k string) (Line, bool) {
	var p findingState
	if !decode(ev, &p) {
		return Line{}, false
	}
	if finding.State(p.State) == finding.StateUpheld {
		return review("⚖️ %s: %s was upheld by %s. It must be fixed before merge.", k, p.Ref, p.By), true
	}
	if finding.State(p.State) == finding.StateDismissed {
		return review("⚖️ %s: %s was dismissed by %s.", k, p.Ref, p.By), true
	}
	return Line{}, false
}

func narrateGate(_ []change.Event, ev change.Event, k string) (Line, bool) {
	var p gateEvaluated
	if !decode(ev, &p) {
		return Line{}, false
	}
	if p.OK {
		return review("✅ %s: the merge check passed. This change is ready to merge.", k), true
	}
	var b strings.Builder
	fmt.Fprintf(&b, "⛔ %s: the merge check is blocked.", k)
	for i, reason := range p.Reasons {
		if i == maxGateReasons {
			fmt.Fprintf(&b, "\n• and %d more", len(p.Reasons)-maxGateReasons)
			break
		}
		fmt.Fprintf(&b, "\n• %s", reason)
	}
	return Line{Audience: AudienceReview, Text: b.String()}, true
}

// humanReasons turns the check's reasons into sentences a developer reads without knowing the internals.
func humanReasons(reasons []string) string {
	var out []string
	add := func(s string) {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	for _, r := range reasons {
		switch {
		case strings.Contains(r, "compile or setup error"):
			add("They did not compile on the current code.")
		case strings.Contains(r, "passed without the feature"):
			add(fmt.Sprintf("%s already passes without the new feature, so it proves nothing.", firstWord(r)))
		case strings.Contains(r, "is flaky"):
			add(fmt.Sprintf("%s gave different results on repeated runs.", firstWord(r)))
		case strings.Contains(r, "imports forbidden"):
			add("A test uses a package that tests may not use.")
		default:
			add(sentence(r))
		}
	}
	if len(out) == 0 {
		return "No reason was given."
	}
	return strings.Join(out, " ")
}

func sentence(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > maxReasonChars {
		s = s[:maxReasonChars] + "..."
	}
	if s == "" || strings.HasSuffix(s, ".") || strings.HasSuffix(s, "...") {
		return s
	}
	return s + "."
}

func firstWord(s string) string {
	if i := strings.IndexByte(s, ' '); i > 0 {
		return s[:i]
	}
	return s
}

func branchOf(history []change.Event) string {
	for i := len(history) - 1; i >= 0; i-- {
		var p branchEnsured
		if history[i].Type == change.EventBranchEnsured && decode(history[i], &p) {
			return p.Branch
		}
	}
	return "the test branch"
}

func tests(format string, args ...any) Line {
	return Line{Audience: AudienceTests, Text: fmt.Sprintf(format, args...)}
}

func review(format string, args ...any) Line {
	return Line{Audience: AudienceReview, Text: fmt.Sprintf(format, args...)}
}

func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// joinNames writes a list the way a person says it: "a", "a and b", "a, b and c".
func joinNames(names []string) string {
	switch len(names) {
	case 0:
		return "no one"
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

func sortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// friendly names an agent by its role, the way people see it in chat: "reviewer-2" is "Heimdall Code Reviewer 2".
func friendly(agent string) string {
	switch {
	case agent == "test-author":
		return "the Heimdall Test Agent"
	case agent == "resolver":
		return "the Heimdall Resolver"
	case strings.HasPrefix(agent, "reviewer-"):
		return "Heimdall Code Reviewer " + strings.TrimPrefix(agent, "reviewer-")
	}
	return agent
}
