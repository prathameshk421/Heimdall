package main

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/collaboration"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// AnswerQuestion answers a mapped person's plain-words @heimdall question from the change's recorded facts (T1, U6).
func (r *commandRuntime) AnswerQuestion(ctx context.Context, target collaboration.Target, _ string, question string) (string, error) {
	if target.ChangeID == "" {
		return "Ask in a change's room so I can check its status.", nil
	}
	events, err := r.events.Load(ctx, target.ChangeID)
	if err != nil {
		return "", fmt.Errorf("load change %s for question: %w", target.ChangeID, err)
	}
	view, err := change.Fold(events)
	if err != nil {
		return "", fmt.Errorf("fold change %s for question: %w", target.ChangeID, err)
	}
	return formatQuestionAnswer(target.ChangeID, view, question), nil
}

// formatQuestionAnswer is the chat answer: one fact per line, in words a person reads without knowing the gate's
// clause ids. A question about CI leads with the check status.
func formatQuestionAnswer(cid id.ChangeID, view change.View, question string) string {
	lines := []string{statusSentence(cid, view)}
	if asksAboutChecks(question) {
		if check := checkSentence(view); check != "" {
			lines = append(lines, check)
		}
	}
	for _, reason := range gateProblems(view) {
		lines = append(lines, "Gate blocked: "+reason)
	}
	if waiting := gateWaiting(view); waiting != "" {
		lines = append(lines, "Still waiting for: "+waiting+".")
	}
	if open := openFindingRefs(view); len(open) > 0 {
		lines = append(lines, "Open findings: "+strings.Join(open, ", ")+".")
	}
	if active := activeTaskSummary(view); len(active) > 0 {
		lines = append(lines, "Active tasks: "+strings.Join(active, ", ")+".")
	}
	if view.Trigger != nil && view.Trigger.CriteriaEdited {
		lines = append(lines, "Acceptance criteria were edited; the tests must cover all current criteria.")
	}
	return strings.Join(lines, "\n")
}

// asksAboutChecks reports whether the question is about CI, a check or the tests, so the answer leads with them.
func asksAboutChecks(question string) bool {
	q := strings.ToLower(question)
	for _, word := range []string{"ci", "check", "test"} {
		if strings.Contains(q, word) {
			return true
		}
	}
	return false
}

func statusSentence(cid id.ChangeID, view change.View) string {
	name := cid.ChatLabel()
	switch {
	case view.Closed != nil && view.Closed.Merged:
		return fmt.Sprintf("%s is merged.", name)
	case view.Closed != nil:
		return fmt.Sprintf("%s is closed without a merge.", name)
	case view.PR != nil:
		return fmt.Sprintf("%s: the pull request is open and not merged yet.", name)
	default:
		return fmt.Sprintf("%s: no pull request is open yet.", name)
	}
}

// checkSentence says the latest result of each check on the current head, in readable words.
func checkSentence(view change.View) string {
	if view.PR == nil || len(view.PR.CI) == 0 {
		return ""
	}
	head := view.PR.HeadSHA
	results, ok := view.PR.CI[head]
	if !ok || len(results) == 0 {
		return "Checks: no result on the current head yet."
	}
	names := make([]string, 0, len(results))
	for name := range results {
		names = append(names, name)
	}
	slices.Sort(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s: %s", name, checkWord(results[name])))
	}
	return "Checks on " + head.Short() + ": " + strings.Join(parts, ", ") + "."
}

// checkWord turns a conclusion into a word a person reads.
func checkWord(c change.CIConclusion) string {
	switch c {
	case change.CISuccess:
		return "passed"
	case change.CIFailure:
		return "failed"
	case change.CITimedOut:
		return "timed out"
	case change.CICancelled:
		return "cancelled"
	case change.CISkipped:
		return "skipped"
	case change.CINeutral:
		return "neutral"
	case change.CIActionRequired:
		return "needs an action on GitHub"
	default:
		return string(c)
	}
}

// gateProblems are the gate's real problems in plain words, one per line. The clause id is dropped and its reason kept.
func gateProblems(view change.View) []string {
	if view.PR == nil || view.PR.Gate == nil || view.PR.Gate.OK {
		return nil
	}
	var out []string
	for _, reason := range view.PR.Gate.Reasons {
		clause, detail, _ := strings.Cut(reason, ":")
		detail = strings.TrimSpace(detail)
		if gate.ClauseID(clause) == gate.ClauseCI && strings.Contains(detail, "has not reported") {
			continue // still waiting, not a problem
		}
		if gate.ClauseID(clause) == gate.ClauseReviews || gate.ClauseID(clause) == gate.ClauseApproval {
			continue // still waiting, not a problem
		}
		if detail == "" {
			detail = reason
		}
		if len(out) == 0 || !slices.Contains(out, detail) {
			out = append(out, detail)
		}
	}
	return out
}

// gateWaiting names the normal steps the merge is still waiting for, in plain words.
func gateWaiting(view change.View) string {
	if view.PR == nil || view.PR.Gate == nil || view.PR.Gate.OK {
		return ""
	}
	var waiting []string
	for _, reason := range view.PR.Gate.Reasons {
		clause, detail, _ := strings.Cut(reason, ":")
		id := gate.ClauseID(clause)
		switch {
		case id == gate.ClauseCI && strings.Contains(detail, "has not reported"):
			waiting = appendUnique(waiting, "the acceptance tests to finish")
		case id == gate.ClauseReviews:
			waiting = appendUnique(waiting, "the code reviews")
		case id == gate.ClauseApproval:
			waiting = appendUnique(waiting, "an approval from an approver")
		}
	}
	return strings.Join(waiting, ", ")
}

func appendUnique(list []string, s string) []string {
	if slices.Contains(list, s) {
		return list
	}
	return append(list, s)
}

// openFindingRefs are the findings that still block the merge, in numeric order so F2 comes before F10.
func openFindingRefs(view change.View) []string {
	type refState struct {
		ref   id.FindingRef
		state finding.State
	}
	var open []refState
	for key, f := range view.Findings {
		if f.State.Terminal() {
			continue
		}
		n, err := id.ParseFindingRef(key)
		if err != nil {
			continue
		}
		open = append(open, refState{ref: n, state: f.State})
	}
	slices.SortFunc(open, func(a, b refState) int { return a.ref.Number() - b.ref.Number() })
	out := make([]string, 0, len(open))
	for _, f := range open {
		out = append(out, fmt.Sprintf("%s (%s)", f.ref.String(), stateWord(f.state)))
	}
	return out
}

// stateWord says a finding's state in words a person reads.
func stateWord(s finding.State) string {
	words := map[finding.State]string{
		finding.StateProposed:    "proposed",
		finding.StateOpen:        "open",
		finding.StateUnverified:  "not verified",
		finding.StateDisputed:    "disputed",
		finding.StateWithdrawn:   "withdrawn",
		finding.StateEscalated:   "needs a person",
		finding.StateUpheld:      "upheld, must be fixed",
		finding.StateDismissed:   "dismissed",
		finding.StateSpecChanged: "the spec changed",
		finding.StateFixClaimed:  "a fix is claimed",
		finding.StateVerifying:   "the proof is running",
		finding.StateResolved:    "resolved",
	}
	if word, ok := words[s]; ok {
		return word
	}
	return string(s)
}

func activeTaskSummary(view change.View) []string {
	var items []string
	for _, t := range view.Tasks {
		if t.Live() {
			items = append(items, fmt.Sprintf("%s (%s)", t.Kind, t.Agent))
		}
	}
	slices.Sort(items)
	return items
}
