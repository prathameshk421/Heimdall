package gate

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// The merge report: the reasons the gate.evaluated event carries and the summary the heimdall/gate check shows on the
// pull request (TECH_SPEC 12.4). Both are pure functions of the unmet clauses.

// MaxReason bounds one reason in the event.
const MaxReason = 256

// Reasons are the unmet clauses as the gate.evaluated event carries them: "<clause>: <reason>", each clipped to MaxReason.
func Reasons(unmet []Clause) []string {
	if len(unmet) == 0 {
		return nil
	}
	reasons := make([]string, 0, len(unmet))
	for _, u := range unmet {
		reasons = append(reasons, clip(fmt.Sprintf("%s: %s", u.ID, u.Reason), MaxReason))
	}
	return reasons
}

// reportRow names the ten merge rules in the order TECH_SPEC 12.4 states them, in words a reviewer reads.
type reportRow struct {
	id    ClauseID
	label string
}

func reportRows() []reportRow {
	return []reportRow{
		{ClauseTicketLinked, "Ticket linked"},
		{ClauseSlopPassed, "Scope and size"},
		{ClauseTestset, "Acceptance tests verified"},
		{ClauseTestTreePinned, "Tests unchanged since verified"},
		{ClauseTestDiffScoped, "No other ticket's tests touched"},
		{ClauseCI, "CI and heimdall/acceptance"},
		{ClauseReviews, "Code reviews"},
		{ClauseBlocking, "No blocking findings"},
		{ClauseApproval, "Approval from an approver"},
		{ClausePolicy, "Policy unchanged"},
	}
}

// waitingClause says whether an unmet clause is a normal step still to come, not a problem.
func waitingClause(u Clause) bool {
	if u.ID == ClauseReviews || u.ID == ClauseApproval {
		return true
	}
	return u.ID == ClauseCI && strings.Contains(u.Reason, "has not reported")
}

// Summary is the merge report on the pull request: a verdict line, then one row per merge rule.
func Summary(ok bool, pending bool, unmet []Clause, limit int) string {
	byID := make(map[ClauseID]Clause, len(unmet))
	blocked := false
	for _, u := range unmet {
		byID[u.ID] = u
		if !waitingClause(u) {
			blocked = true
		}
	}
	var b strings.Builder
	switch {
	case ok:
		b.WriteString("### ✅ Ready to merge\nEvery merge rule passes on this commit.\n\n")
	case blocked:
		b.WriteString("### ⛔ Blocked\nFix the rows marked ❌, then push.\n\n")
	case pending:
		b.WriteString("### ⏳ Not ready yet\nThe rows marked ⏳ are still running or waiting for a person.\n\n")
	default:
		b.WriteString("### ⏳ Not ready yet\n\n")
	}
	b.WriteString("| Rule | Status |\n|---|---|\n")
	for _, row := range reportRows() {
		u, unmetRow := byID[row.id]
		switch {
		case !unmetRow:
			fmt.Fprintf(&b, "| %s | ✅ |\n", row.label)
		case waitingClause(u):
			fmt.Fprintf(&b, "| %s | ⏳ %s |\n", row.label, tableText(u.Reason, 140))
		default:
			fmt.Fprintf(&b, "| %s | ❌ %s |\n", row.label, tableText(u.Reason, 140))
		}
	}
	return clip(b.String(), limit)
}

// tableText keeps a reason on one table row: no pipes or line breaks, at most limit bytes.
func tableText(s string, limit int) string {
	s = strings.NewReplacer("|", "/", "\n", " ").Replace(s)
	return clip(s, limit)
}

// clip shortens s to at most limit bytes on a rune boundary, with an ellipsis inside the bound.
func clip(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit - 3
	if cut < 0 {
		cut = 0
	}
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}
