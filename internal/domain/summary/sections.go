package summary

import (
	"slices"
	"strconv"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/fastcheck"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
)

// unprovenBlock is the collapsed list of what no test or tool backed. The count shows without opening it. A block is
// never cut in the middle: the lists inside shrink, and the tags always close.
func (v view) unprovenBlock(l limits) []string {
	if len(v.unproven) == 0 {
		return []string{"Unproven, not shown: none."}
	}
	sources := countBySource(v.unproven)
	lines := []string{
		"<details>",
		"<summary>Unproven, not shown: " + plural(len(v.unproven), "finding", "findings") + " from " +
			plural(len(sources), "reviewer", "reviewers") + "</summary>",
		"",
		"No test or tool backed these, so they are not shown as findings. Unproven does not mean wrong.",
		"",
	}
	lines = append(lines, limitedFunc(len(sources), l.sources, func(i int) string {
		s := sources[i]
		return "- " + reviewerName(s.Reviewer) + " " + strconv.Itoa(s.Total) + ": " + labelCounts(s.Labels)
	}, "reviewers")...)
	lines = append(lines, "", "Findings:", "")
	lines = append(lines, limitedFunc(len(v.unproven), l.items, func(i int) string { return v.unprovenItem(v.unproven[i]) }, "findings")...)
	return append(lines, "", "</details>")
}

func labelCounts(ls []labelCount) string {
	var parts []string
	for _, c := range ls {
		parts = append(parts, strconv.Itoa(c.N)+" "+c.Label.Text)
	}
	return strings.Join(parts, ", ")
}

func (v view) unprovenItem(f Finding) string {
	claim := text(f.Claim, maxClaim)
	if claim == "" {
		claim = "(no claim text)"
	}
	return "- **" + idLabel(f.ID) + "** " + where(f) + " (" + raisedBy(f) + ", " + unprovenLabel(f).Text + ") " + claim
}

// fastLines writes one line per clause. An unmet or unknown clause says "blocks" in enforce mode and "would block" in
// shadow mode, with its reason.
func (v view) fastLines() []string {
	if len(v.clauses) == 0 {
		return []string{"- **unknown** The fast check did not report. Nothing was decided."}
	}
	blocks := "blocks"
	if v.mode == policy.ActionModeShadow {
		blocks = "would block"
	}
	var lines []string
	for _, c := range v.clauses {
		lines = append(lines, clauseLine(c, blocks))
	}
	return limited(lines, maxClauses, "clauses")
}

func clauseLine(c fastcheck.Clause, blocks string) string {
	id, reason := clauseCode(c.ID), text(c.Reason, maxReason)
	switch c.Status {
	case fastcheck.StatusMet:
		return "- met " + id + joinReason(": ", reason)
	case fastcheck.StatusNotChecked:
		return "- not checked " + id + joinReason(": ", reason)
	case fastcheck.StatusUnmet:
		return "- **unmet** " + id + ": " + blocks + joinReason(" because ", reason)
	case fastcheck.StatusUnknown:
		return "- **unknown** " + id + ": " + blocks + " until known" + joinReason(": ", reason)
	}
	return "- **unknown** " + id + ": " + blocks + " until known, the status is not one Heimdall defines"
}

// joinReason adds a reason after a joint, or nothing when there is no reason.
func joinReason(joint, reason string) string {
	if reason == "" {
		return ""
	}
	return joint + reason
}

// notCheckedLines says what Heimdall did not look at, so nobody reads silence as safety. The last line is always
// there.
func (v view) notCheckedLines(n int) []string {
	var lines []string
	for _, c := range clausesWith(v.clauses, fastcheck.StatusNotChecked) {
		lines = append(lines, "- "+clauseCode(c.ID)+": "+text(c.Reason, maxReason))
	}
	switch v.in.Proof.Kind {
	case proof.KindSkippedTrivial:
		lines = append(lines, "- Proof run: skipped, the change is trivial by rule. No test and no model looked at the code.")
	case proof.KindUnknown:
		lines = append(lines, "- Proof run: did not finish ("+v.in.Proof.Reason.Text()+
			"). What it had not reached was not checked.")
	case proof.KindProven, proof.KindBlocked, proof.KindNotReproduced:
	}
	lines = append(lines, v.candidatesNotChecked()...)
	lines = limited(lines, n, "items")
	return append(lines, "", "Heimdall judges only what is listed on this page. Whatever is not listed was not looked at, so silence is not safety.")
}

// candidatesNotChecked counts the candidate findings that were not checked, once per reason, in the order of proof.
func (v view) candidatesNotChecked() []string {
	type group struct {
		reason proof.Reason
		n      int
	}
	var groups []group
	for _, f := range v.unproven {
		if f.Result.Class != proof.ClassNotChecked {
			continue
		}
		i := slices.IndexFunc(groups, func(g group) bool { return g.reason == f.Result.Reason })
		if i < 0 {
			groups = append(groups, group{reason: f.Result.Reason})
			i = len(groups) - 1
		}
		groups[i].n++
	}
	slices.SortStableFunc(groups, func(a, b group) int { return notCheckedRank(a.reason) - notCheckedRank(b.reason) })
	var lines []string
	for _, g := range groups {
		lines = append(lines, "- "+plural(g.n, "candidate finding", "candidate findings")+" not checked: "+g.reason.Text())
	}
	return lines
}

// costLine is the cost of this run, the month to date when it is given, the tokens and the wall time. A skip costs
// $0 and says that no model ran. A spend that cannot be true is not printed as a number.
func (v view) costLine() string {
	dollars, toks := v.runCost()
	parts := []string{dollars}
	if u := v.in.Usage; u.MonthKnown && validMoney(u.MonthUSD) {
		parts = append(parts, money(u.MonthUSD)+" this month")
	}
	if toks != "" {
		parts = append(parts, toks)
	}
	if u := v.in.Usage; u.WallKnown && u.Wall >= 0 {
		parts = append(parts, wall(u.Wall))
	}
	return "Cost: " + strings.Join(parts, ", ") + "."
}

// runCost is what this run cost in dollars, and its tokens when a model ran and the spend is believable.
func (v view) runCost() (dollars, toks string) {
	s := v.in.Proof.Cost
	switch {
	case !validMoney(s.USD) || s.InputTokens < 0 || s.OutputTokens < 0:
		return "this run unknown, the measure is not valid", ""
	case v.in.Proof.Kind == proof.KindSkippedTrivial:
		return "$0.00 this run, no model call", ""
	}
	return money(s.USD) + " this run", tokens(s.InputTokens) + " input and " + tokens(s.OutputTokens) + " output tokens"
}
