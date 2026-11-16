package change

import (
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// Need is one thing the change still needs, in plain words (AGENT_INTERACTION_ARCH F1 rule 3). Step is the workflow
// row; Clause is set only for the gate's clauses; Subject is the reviewer, finding, check or criterion it is about.
type Need struct {
	Step    string        `json:"step"`
	Role    workflow.Role `json:"role"`
	Clause  string        `json:"clause"`
	Subject string        `json:"subject"`
	Text    string        `json:"text"`
}

// Missing lists what the change still needs: each workflow row whose after rows are done and which is not done itself,
// in the order of the workflow, then the merge rule's clauses the view can decide (TECH_SPEC 12.4). The rules and
// words are the spec in testdata/plan/view/README.md.
func Missing(v View, p policy.Policy) []Need {
	rows := workflow.Default().Steps
	table := rowTable()
	done := map[string]bool{}
	for _, r := range rows {
		rule, ok := table[r.ID]
		done[r.ID] = !ok || rule.done(v) // mutation, deliberate and gate are never row needs (view README, Q1)
	}
	out := []Need{}
	for _, r := range rows {
		if done[r.ID] || !allDone(done, r.After) {
			continue
		}
		for _, n := range table[r.ID].needs(v) {
			n.Step, n.Role = r.ID, r.Role
			out = append(out, n)
		}
	}
	return append(out, gateNeeds(v, p)...)
}

func allDone(done map[string]bool, ids []string) bool {
	for _, id := range ids {
		if !done[id] {
			return false
		}
	}
	return true
}

// rowRule is when one workflow row is done and what it needs when it is not.
type rowRule struct {
	done  func(View) bool
	needs func(View) []Need
}

// rowTable is the done rule and the need of every workflow row that can be a need (view README, "Missing").
func rowTable() map[string]rowRule {
	say := func(text string) func(View) []Need { return func(View) []Need { return []Need{{Text: text}} } }
	return map[string]rowRule{
		"open_room":   {func(v View) bool { return v.Room != "" }, say("the change's room is opened")},
		"branch":      {func(v View) bool { return v.Branch != nil }, say("the ticket branch is created")},
		"write_tests": {func(v View) bool { return v.Tests != nil }, say("the test author submits tests")},
		"test_qa":     {func(v View) bool { return v.Tests != nil && v.Tests.Verified != nil }, say("the sandbox verifies the tests")},
		"signoff":     {signedOff, say("a person approves the tests (sensitive domain)")},
		"commit":      {func(v View) bool { return v.Tests != nil && v.Tests.Committed != nil }, say("the tests are committed to the branch")},
		"wait_pr": {func(v View) bool { return v.PR != nil }, func(v View) []Need {
			return []Need{{Text: "a pull request for " + string(v.Trigger.Ticket) + " is opened"}}
		}},
		"slop":   {slopPassedOnHead, func(v View) []Need { return []Need{slopNeed(v.PR)} }},
		"pick":   {func(v View) bool { return v.PR != nil && v.PR.Picked != nil }, say("reviewers are picked")},
		"review": {func(v View) bool { return v.PR != nil && v.PR.Picked != nil && len(unreviewed(v)) == 0 }, reviewNeeds},
		"verify": {func(v View) bool { return len(unchecked(v)) == 0 }, verifyNeeds},
	}
}

// signedOff: the sign-off row is done when the verified tests need none (its `when` does not hold) or have one.
func signedOff(v View) bool {
	t := v.Tests
	return t != nil && t.Verified != nil && (!t.Verified.NeedsSignoff || t.ApprovedBy != "")
}

func slopPassedOnHead(v View) bool {
	return v.PR != nil && v.PR.Slop != nil && v.PR.Slop.SHA == v.PR.HeadSHA && v.PR.Slop.Pass
}

func reviewNeeds(v View) []Need {
	var out []Need
	for _, r := range unreviewed(v) {
		out = append(out, Need{Subject: r, Text: r + " reviews " + short(string(v.PR.HeadSHA))})
	}
	return out
}

func verifyNeeds(v View) []Need {
	var out []Need
	for _, ref := range unchecked(v) {
		out = append(out, Need{Subject: ref, Text: ref + " evidence is verified (" + string(v.Findings[ref].Tier) + ")"})
	}
	return out
}

func slopNeed(pr *PRInfo) Need {
	h := short(string(pr.HeadSHA))
	if pr.Slop != nil && pr.Slop.SHA == pr.HeadSHA {
		return Need{Text: "the slop rules pass on " + h + " (failing: " + strings.Join(pr.Slop.FailingRules, ", ") + ")"}
	}
	return Need{Text: "the slop rules are evaluated on " + h}
}

// unreviewed are the picked reviewers with no review on the current head, in pick order.
func unreviewed(v View) []string {
	var out []string
	for _, r := range v.PR.Picked.People {
		if v.Reviews[string(r)].HeadSHA != v.PR.HeadSHA {
			out = append(out, string(r))
		}
	}
	return out
}

// unchecked are the E1 to E3 findings no verifier has checked, by ref number. E4 has no verification.
func unchecked(v View) []string {
	var out []string
	for _, ref := range findingRefs(v) {
		f := v.Findings[ref]
		if !f.Checked && f.Tier != finding.TierE4 {
			out = append(out, ref)
		}
	}
	return out
}

// findingRefs are the finding refs in number order (F2 before F10).
func findingRefs(v View) []string {
	refs := make([]string, 0, len(v.Findings))
	for ref := range v.Findings {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool { return refNumber(refs[i]) < refNumber(refs[j]) })
	return refs
}

func refNumber(ref string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(ref, "F"))
	return n
}

// gateNeeds are the merge rule's clauses (TECH_SPEC 12.4) that the view and policy can decide. There is no head before
// a pull request, so there are none then.
func gateNeeds(v View, p policy.Policy) []Need {
	if v.PR == nil {
		return nil
	}
	h := short(string(v.PR.HeadSHA))
	var out []Need
	if v.PR.Ticket == "" {
		out = append(out, gateNeed("ticket", "", "the pull request is linked to a ticket"))
	}
	out = append(out, uncoveredCriteria(v)...)
	for _, check := range append([]string{"heimdall/acceptance"}, p.CIRequired...) {
		if c := v.PR.CI[v.PR.HeadSHA][check]; c == "" {
			out = append(out, gateNeed("ci", check, check+" succeeds on "+h))
		} else if c != CIConclusion("success") {
			out = append(out, gateNeed("ci", check, check+" succeeds on "+h+" (it reported "+string(c)+")"))
		}
	}
	for _, ref := range findingRefs(v) {
		if f := v.Findings[ref]; blocks(f, p.Blocking) {
			out = append(out, gateNeed("blocking", ref, ref+" is resolved ("+string(f.Tier)+" "+string(f.Severity)+", "+string(f.State)+")"))
		}
	}
	if !approvedOnHead(v, p) {
		out = append(out, gateNeed("approval", "", "an approver who is not the author approves "+h))
	}
	return out
}

func gateNeed(clause, subject, text string) Need {
	return Need{Step: "gate", Clause: clause, Subject: subject, Text: text}
}

// uncoveredCriteria are the ticket's criteria no submitted test covers.
func uncoveredCriteria(v View) []Need {
	if v.Tests == nil || v.Trigger == nil {
		return nil
	}
	var out []Need
	for _, ac := range v.Trigger.Criteria {
		if len(v.Tests.ACMap[ac]) == 0 {
			out = append(out, gateNeed("ac_covered", ac, "a test covers "+ac))
		}
	}
	return out
}

// blocks is the blocking set of TECH_SPEC 11.1: a live state, a tier no weaker than min_tier, a severity no lower
// than min_severity. An E4 finding never blocks (invariant 3), because min_tier is at most E3.
func blocks(f FindingInfo, b policy.Blocking) bool {
	live := []finding.State{finding.StateOpen, finding.StateDisputed, finding.StateEscalated, finding.StateUpheld,
		finding.StateFixClaimed, finding.StateVerifying}
	return slices.Contains(live, f.State) && f.Tier.AtLeast(b.MinTier) && f.Severity.AtLeast(b.MinSeverity)
}

// approvedOnHead: an approval of the head, by a login that is not the author and is the GitHub login of a person
// named in any approvers list (which domains a change touches is in no event: view README Q6).
func approvedOnHead(v View, p policy.Policy) bool {
	approvers := map[string]bool{}
	for _, names := range p.Approvers {
		for _, n := range names {
			if gh := p.People[n].GitHub; gh != "" {
				approvers[gh] = true
			}
		}
	}
	for _, a := range v.Approvals {
		if a.CommitSHA == v.PR.HeadSHA && a.Login != v.PR.Author && approvers[string(a.Login)] {
			return true
		}
	}
	return false
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
