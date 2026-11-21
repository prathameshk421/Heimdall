package change

import (
	"sort"
	"strconv"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
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
// A policy that cannot be used is the one need besides the dead jobs: no row runs on it.
// There is no second table: the rows are the ones the reconciler runs. The words are the spec in
// testdata/plan/view/README.md.
func Missing(in In) []Need {
	out := []Need{}
	if in.PolicyProblem != "" {
		out = append(out, Need{Step: "policy", Text: "heimdall.yaml cannot be used (" + in.PolicyProblem +
			"); nothing runs on this change until the policy is fixed on the base branch"})
		return append(out, deadNeeds(in)...)
	}
	for _, ra := range Next(in) {
		if ra.Text == "" {
			continue // the need is told elsewhere (the gate's clause)
		}
		out = append(out, Need{Step: ra.Row, Role: roleOf(ra.Action), Subject: subjectOf(ra), Text: ra.Text})
	}
	out = append(out, gateNeeds(in)...)
	out = append(out, providerNeeds(in.View)...)
	return append(out, deadNeeds(in)...)
}

// providerNeeds says so when the model provider refuses requests and an agent holds a task: no agent can answer until it
// does, and waking them again would not help.
func providerNeeds(v View) []Need {
	if v.Provider == nil || v.Provider.Health != "refusing" {
		return nil
	}
	for _, t := range v.Tasks {
		if t.Live() {
			return []Need{{Text: "the model provider refuses requests, so agents cannot answer until it recovers"}}
		}
	}
	return nil
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
	case EffectProfileTrial:
		return "profile"
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

// gateNeeds are the merge rule's unmet clauses (TECH_SPEC 12.4), as the rule words them. There is none before a pull
// request, since there is no head.
func gateNeeds(in In) []Need {
	if in.View.PR == nil || in.View.Over() {
		return nil
	}
	var out []Need
	for _, u := range EvaluateGate(in.View, in.Outside).Unmet {
		if u.ID == gate.ClauseReviews || u.ID == gate.ClauseSlopPassed {
			continue // the review and slop rows already say what they wait for
		}
		if u.ID == gate.ClauseBlocking {
			out = append(out, blockingNeeds(in.View, in.Outside)...) // people read F1, never an internal id
			continue
		}
		out = append(out, Need{Step: "gate", Clause: string(u.ID), Text: u.Reason})
	}
	return out
}

// blockingNeeds names each blocking finding by its short ref, with its tier, severity and state.
func blockingNeeds(v View, o Outside) []Need {
	blocking := map[id.FindingID]bool{}
	for _, fid := range gate.BlockingSet(findingStates(v, o.PolicyAtBase.Consensus), blockingFor(o)) {
		blocking[fid] = true
	}
	var out []Need
	for _, ref := range findingRefs(v) {
		if f := v.Findings[ref]; blocking[f.ID] {
			out = append(out, Need{Step: "gate", Clause: string(gate.ClauseBlocking), Subject: ref,
				Text: ref + " is resolved (" + string(f.Tier) + " " + string(f.Severity) + ", " + string(f.State) + ")"})
		}
	}
	return out
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
