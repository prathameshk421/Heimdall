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

// Missing lists what the change still needs (HARNESS_DESIGN B2): the Text of every action of every open row, in table
// order; then the merge rule's clauses the view can decide (TECH_SPEC 12.4); then one need per job that failed for good.
// There is no second table: the rows are the ones the reconciler runs. The words are the spec in
// testdata/plan/view/README.md.
func Missing(in In) []Need {
	out := []Need{}
	for _, ra := range Next(in) {
		if ra.Text == "" {
			continue // the need is told elsewhere (the gate's clause)
		}
		out = append(out, Need{Step: ra.Row, Role: roleOf(ra.Action), Subject: subjectOf(ra), Text: ra.Text})
	}
	out = append(out, gateNeeds(in.View, in.Policy)...)
	return append(out, deadNeeds(in)...)
}

// roleOf is the role that holds the work of a task action, and none for any other action.
func roleOf(a Action) workflow.Role {
	if a.Task == nil {
		return ""
	}
	for _, k := range workflow.Kinds() {
		if k.Name == a.Task.Kind {
			return k.Holder
		}
	}
	return ""
}

// subjectOf is who or what a need is about: the seat of a review, the finding of a verification.
func subjectOf(ra RowAction) string {
	switch {
	case ra.Row == "review" && ra.Task != nil:
		return ra.Task.Subject
	case ra.Row == "verify" && ra.Job != nil:
		if p, ok := ra.Job.Payload.(VerifyEvidencePayload); ok {
			return p.FindingRef.String()
		}
	}
	return ""
}

// rowOfJob is the row whose job each effect kind is; `heimdall resume <change> <row>` runs it again.
func rowOfJob(k EffectKind) string {
	switch k {
	case EffectRoomOpen:
		return "open_room"
	case EffectGitHubBranch:
		return "branch"
	case EffectTestQuality:
		return "test_qa"
	case EffectGitHubCommitTests:
		return "commit"
	case EffectRunChecks:
		return "checks"
	case EffectVerifyEvidence:
		return "verify"
	case EffectAcceptance:
		return "acceptance"
	case EffectGitHubCheck:
		return "gate"
	case EffectGitHubComment, EffectRoomPost, EffectTrackerWrite, EffectMemoryEpisode, EffectTaskAssign:
		return ""
	}
	return ""
}

// deadNeeds are the jobs that failed for good or are in doubt. Nothing retries them by itself: a person decides.
func deadNeeds(in In) []Need {
	var out []Need
	for _, d := range in.Dead {
		row := rowOfJob(d.Kind)
		text := string(d.Kind) + " failed " + strconv.Itoa(d.Attempts) + " times: " + d.LastError
		if d.InDoubt {
			text = string(d.Kind) + " may or may not have run (it was interrupted)"
		}
		if row != "" {
			text += "; `heimdall resume " + string(in.Change) + " " + row + "` runs it again"
		}
		out = append(out, Need{Step: row, Text: text})
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
