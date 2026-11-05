package e2e

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// The product-quality checks of MVP_SCENARIOS section 4. They are what separates a product from wiring that
// technically passes, and they run on every transcript, check and card a scenario captures.

// Compiled once. Each is one family of internals a person must never see.
var (
	reID         = regexp.MustCompile(`\b(?:fnd|run|case|tsk|chg|rev)_[0-9A-Za-z]{10,}\b`)
	reULID       = regexp.MustCompile(`\b[0-9A-HJKMNP-TV-Z]{26}\b`)
	reEvent      = regexp.MustCompile(`\b(?:change|room|branch|task|question|tests|ticket|pr|slop|reviewers|review|finding|challenge|case|verdict|decision|approval|ci|gate)\.(?:triggered|opened|ensured|assigned|acked|timed_out|failed|asked|answered|submitted|verified|rejected|committed|approved|changed|pushed|linked|closed|evaluated|picked|unavailable|waived|filed|state_changed|applied|escalated|recorded|invalidated|reported|merged)\b`)
	reState      = regexp.MustCompile(`\b(?:ROOM_OPEN|WRITING_TESTS|VERIFYING_TESTS|AWAITING_TEST_SIGNOFF|TESTS_READY|PR_OPEN|SLOP_REJECTED|PICKING_REVIEWERS|IN_REVIEW|DELIBERATING|ESCALATED|VERIFYING_FIX|READY_FOR_APPROVAL|UNLINKED_PR|TRIGGERED|BUILDING|FIXING|APPROVED|MERGED|REJECTED)\b`)
	reReason     = regexp.MustCompile(`\b(?:tests_changed|policy_changed|consensus_hold|agents_unreachable|verification_pending|policy_invalid|injection_suspected|no_approval|open_findings|stale_approval)\b`)
	reStack      = regexp.MustCompile(`goroutine \d+ \[|\bpanic: |runtime error|Traceback \(most recent|\.go:\d+ \+0x[0-9a-f]+`)
	reJSONObject = regexp.MustCompile(`\{\s*"[A-Za-z_][A-Za-z0-9_]*"\s*:`)
	reJSONArray  = regexp.MustCompile(`\[\s*"[^"\n]*"\s*[,\]]`)
	reNull       = regexp.MustCompile(`(?i)\bnull\b`)
	reHole       = regexp.MustCompile(`\{\{|\}\}|<no value>|<nil>|%!\w|%!\(`)
	// reGoError is the text of a Go or infrastructure failure, which a person must never be shown.
	reGoError = regexp.MustCompile(`connection refused|context deadline exceeded|\bsql: |\bpq: |\bpanic: |goroutine |runtime error|nil pointer`)
)

func rawPatterns() []*regexp.Regexp {
	return []*regexp.Regexp{reID, reULID, reEvent, reState, reReason, reStack, reJSONObject, reJSONArray, reNull, reHole, reGoError}
}

// RawInternals returns the fragments of text a person must never see: ids, event types, state names, reason
// codes, stack traces, JSON, null and template holes (check 1, "human words only").
func RawInternals(text string) []string {
	var out []string
	for _, re := range rawPatterns() {
		out = append(out, re.FindAllString(text, -1)...)
	}
	return out
}

var unblockWord = regexp.MustCompile(`(?i)\b(need|needs|waiting|wait|until|before|add|remove|push|approve|approval|run|link|ask|decide|decision|resolve|fix|retry)\b`)

// GateProblems says what is wrong with how a gate explains itself (check 2, "every red says why"): a failure or
// in-progress gate needs a summary and at least one reason, and each reason names what unblocks it.
func GateProblems(g GateState) []string {
	if g.State != "failure" && g.State != "in_progress" {
		return nil
	}
	var out []string
	if strings.TrimSpace(g.Summary) == "" {
		out = append(out, fmt.Sprintf("a %s gate has no summary", g.State))
	}
	if len(g.Reasons) == 0 {
		out = append(out, fmt.Sprintf("a %s gate gives no reason", g.State))
	}
	for _, r := range g.Reasons {
		switch {
		case strings.TrimSpace(r.Text) == "":
			out = append(out, fmt.Sprintf("reason %q has no text", r.Code))
		case !unblockWord.MatchString(r.Text):
			out = append(out, fmt.Sprintf("reason %q does not say what unblocks it: %q", r.Code, r.Text))
		}
	}
	return out
}

// FindingView is a finding as a person sees it. FileLines is the number of lines the file has at the head
// commit, read by the scenario from GitHub (0 when the file is not in the tree).
type FindingView struct {
	ID          string
	File        string
	Line        int
	FileLines   int
	Tier        string
	Claim       string
	EvidenceRef string
}

var shortFindingID = regexp.MustCompile(`^F\d+$`)

// FindingProblems says what makes a displayed finding uncheckable (check 3).
func FindingProblems(f FindingView) []string {
	var out []string
	add := func(format string, args ...any) {
		out = append(out, fmt.Sprintf("finding %s: "+format, append([]any{f.ID}, args...)...))
	}
	if !shortFindingID.MatchString(f.ID) {
		add("the id must be F1, F2, ... not an internal id")
	}
	switch {
	case f.File == "":
		add("no file")
	case f.FileLines == 0:
		add("%s is not in the head tree", f.File)
	case f.Line < 1 || f.Line > f.FileLines:
		add("line %d is outside %s (%d lines)", f.Line, f.File, f.FileLines)
	}
	if !validTier(f.Tier) {
		add("tier %q is not E1 to E4", f.Tier)
	}
	if strings.TrimSpace(f.Claim) == "" {
		add("no claim")
	}
	if strings.TrimSpace(f.EvidenceRef) == "" {
		add("no link or reference to its evidence")
	}
	return out
}

// Budget is how many messages a scenario's product may post: one per milestone, plus questions and decisions.
type Budget struct{ Milestones, Questions, Decisions int }

// QuietProblems checks check 4: only the product's own messages count; progress chatter is never allowed, the
// total stays inside the budget, and a replayed step never posts a duplicate.
func QuietProblems(msgs []Message, b Budget) []string {
	var out []string
	counts := map[MessageKind]int{}
	seen := map[string]bool{}
	for _, m := range msgs {
		if !m.FromAgent {
			continue
		}
		key := m.Room + "\x00" + m.From + "\x00" + m.Text
		if seen[key] {
			out = append(out, fmt.Sprintf("duplicate message from %s: %q", m.From, m.Text))
		}
		seen[key] = true
		if m.Kind == KindProgress {
			out = append(out, fmt.Sprintf("progress chatter from %s: %q", m.From, m.Text))
			continue
		}
		if m.Kind == KindOther {
			counts[KindMilestone]++
			continue
		}
		counts[m.Kind]++
	}
	for _, c := range []struct {
		kind    MessageKind
		allowed int
	}{{KindMilestone, b.Milestones}, {KindQuestion, b.Questions}, {KindDecision, b.Decisions}} {
		if counts[c.kind] > c.allowed {
			out = append(out, fmt.Sprintf("%d %s messages, budget %d", counts[c.kind], c.kind, c.allowed))
		}
	}
	return out
}

// AddressingProblems checks check 5: people are @-mentioned only for a question or a decision that is theirs, a
// question or decision mentions someone, and nothing else pings.
func AddressingProblems(msgs []Message) []string {
	var out []string
	for _, m := range msgs {
		if !m.FromAgent {
			continue
		}
		asks := m.Kind == KindQuestion || m.Kind == KindDecision
		switch {
		case asks && len(m.Mentions) == 0:
			out = append(out, fmt.Sprintf("a %s from %s mentions nobody, so it reaches nobody: %q", m.Kind, m.From, m.Text))
		case !asks && len(m.Mentions) > 0:
			out = append(out, fmt.Sprintf("a %s from %s pings %s: %q", m.Kind, m.From, strings.Join(m.Mentions, ", "), m.Text))
		}
	}
	return out
}

// ReplayProblems checks check 6: after the poller runs again over the same data, no message, room or check
// run appeared or vanished.
func ReplayProblems(before, after Counts) []string {
	var out []string
	out = append(out, diffCounts("messages", before.Messages, after.Messages)...)
	out = append(out, diffCounts("check runs", before.Checks, after.Checks)...)
	return out
}

func diffCounts(what string, before, after map[string]int) []string {
	keys := map[string]bool{}
	for k := range before {
		keys[k] = true
	}
	for k := range after {
		keys[k] = true
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	var out []string
	for _, k := range sorted {
		if before[k] != after[k] {
			out = append(out, fmt.Sprintf("replay changed %s for %s: %d before, %d after", what, k, before[k], after[k]))
		}
	}
	return out
}

// CostProblems checks check 8: a scripted step must show $0, since a scripted run that spent tokens woke a real agent.
func CostProblems(scripted bool, usd float64) []string {
	if scripted && usd > 0 {
		return []string{fmt.Sprintf("a scripted step cost $%.4f; scripted runs must cost $0", usd)}
	}
	return nil
}

var findingMention = regexp.MustCompile(`\bF\d+\b|\b[\w./-]+\.\w+:\d+\b`)

// AgentLeakProblems checks check 9, agents stay out of the room's decisions: no agent post carries a canary,
// and none shows a finding before every reviewer has submitted (allIn; the zero time means never).
func AgentLeakProblems(msgs []Message, canaries []string, allIn time.Time) []string {
	var out []string
	for _, m := range msgs {
		if !m.FromAgent {
			continue
		}
		for _, c := range canaries {
			if c != "" && strings.Contains(m.Text, c) {
				out = append(out, fmt.Sprintf("%s posted a canary: %q", m.From, c))
			}
		}
		if (allIn.IsZero() || m.At.Before(allIn)) && findingMention.MatchString(m.Text) {
			out = append(out, fmt.Sprintf("%s showed a finding before all reviewers had submitted: %q", m.From, m.Text))
		}
	}
	return out
}
