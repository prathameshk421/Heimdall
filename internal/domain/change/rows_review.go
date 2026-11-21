package change

import (
	"encoding/base64"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/slop"
)

// acceptanceCheck is the check Heimdall's own run of every Heimdall test reports under (TECH_SPEC 12.4, D150).
const acceptanceCheck = "heimdall/acceptance"

func reviewRows() []Row {
	return []Row{rowSlop(), rowChecks(), rowPick(), rowReview(), rowVerify(), rowDeliberate(), rowAcceptance(), rowGate()}
}

func rowSlop() Row {
	return Row{ID: "slop", Flow: FlowReview,
		Done: func(in In) bool { return slopAllowsReview(in.View) },
		Next: func(in In) []Action { return []Action{say(slopText(in.View.PR))} }}
}

// slopAllowsReview is whether the slop rules let the review start on the head: they pass, or the only one failing is
// the ticket link (D174). A ticketless pull request is worth reviewing; the gate still reads the failed slop and the
// ticket clause, so it can never merge. Any other failure stops the review before it costs tokens.
func slopAllowsReview(v View) bool {
	if v.PR == nil || v.PR.Slop == nil || v.PR.Slop.SHA != v.PR.HeadSHA {
		return false
	}
	if v.PR.Slop.Pass {
		return true
	}
	return len(v.PR.Slop.FailingRules) > 0 && !slices.ContainsFunc(v.PR.Slop.FailingRules, func(r string) bool {
		return r != string(slop.RuleTicketLinked)
	})
}

func slopText(pr *PRInfo) string {
	h := short(string(pr.HeadSHA))
	if pr.Slop != nil && pr.Slop.SHA == pr.HeadSHA {
		return "the slop rules pass on " + h + " (failing: " + strings.Join(pr.Slop.FailingRules, ", ") + ")"
	}
	return "the slop rules are evaluated on " + h
}

// applicableChecks are the ids of the profile checks that run on this head.
func applicableChecks(in In) []string {
	return ApplicableChecks(in.Profile.Checks, in.Outside.ChangedFiles)
}

// ApplicableChecks are the ids of the checks whose `when` matches one of files (every check with an empty `when`). They
// are the checks offered on a head: run_checks runs them, and E2 evidence must cite one of them.
func ApplicableChecks(checks []ProfileCheck, files []string) []string {
	var ids []string
	for _, c := range checks {
		if len(c.When) == 0 || anyFileMatches(c.When, files) {
			ids = append(ids, c.ID)
		}
	}
	return ids
}

func anyFileMatches(globs, files []string) bool {
	for _, f := range files {
		for _, g := range globs {
			if policy.MatchGlob(g, f) {
				return true
			}
		}
	}
	return false
}

func rowChecks() Row {
	return Row{ID: "checks", Flow: FlowReview, After: []string{"slop"}, Jobs: []EffectKind{EffectRunChecks},
		Done: func(in In) bool { return in.View.Checks[in.View.PR.HeadSHA].Ran || len(applicableChecks(in)) == 0 },
		Next: func(in In) []Action {
			pr := in.View.PR
			p := RunChecksPayload{Repo: pr.Repo, Change: in.Change, Head: pr.HeadSHA, Base: pr.Base, CheckIDs: applicableChecks(in)}
			return []Action{{Text: "the profile checks run on " + short(string(pr.HeadSHA)),
				Job: &Job{Kind: EffectRunChecks, Key: in.jobKey("checks", string(pr.HeadSHA)), Target: string(pr.Repo), Payload: p}}}
		}}
}

func rowPick() Row {
	return Row{ID: "pick", Flow: FlowReview, After: []string{"slop"},
		Done: func(in In) bool { return in.View.PR.Picked != nil },
		Next: func(in In) []Action {
			pr := in.View.PR
			if in.Now.Before(pr.SlopAt.Add(in.Settings.PickWait)) {
				return []Action{{Ask: &Ask{Who: []ActorID{pr.Author}, What: "pick reviewers with `heimdall review`"}, Text: "reviewers are picked"}}
			}
			people := pr.Suggested
			if len(people) == 0 {
				people = in.Settings.Reviewers
			}
			return []Action{{Fact: ReviewersPicked{People: people, By: "heimdall", Method: PickDefault}, Text: "reviewers are picked"}}
		}}
}

// unreviewed are the picked seats with no outcome on the current head, in pick order.
func unreviewed(v View) []string {
	if v.PR.Picked == nil {
		return nil
	}
	var out []string
	for _, r := range v.PR.Picked.People {
		if v.Reviews[string(r)].HeadSHA != v.PR.HeadSHA {
			out = append(out, string(r))
		}
	}
	return out
}

func rowReview() Row {
	return Row{ID: "review", Flow: FlowReview, After: []string{"pick", "checks"},
		Done: func(in In) bool { return in.View.PR.Picked != nil && len(unreviewed(in.View)) == 0 },
		Next: func(in In) []Action {
			head := string(in.View.PR.HeadSHA)
			var out []Action
			for _, seat := range unreviewed(in.View) {
				out = append(out, Action{Task: &Task{Kind: "review", Subject: seat, Input: "head-" + head},
					Text: seat + " reviews " + short(head)})
			}
			return out
		}}
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

func rowVerify() Row {
	return Row{ID: "verify", Flow: FlowReview, After: []string{"review"}, Jobs: []EffectKind{EffectVerifyEvidence},
		Done: func(in In) bool { return len(unchecked(in.View)) == 0 },
		Next: func(in In) []Action {
			pr := in.View.PR
			var out []Action
			for _, ref := range unchecked(in.View) {
				f := in.View.Findings[ref]
				p := VerifyEvidencePayload{Repo: pr.Repo, Ticket: in.Change, FindingID: f.ID, FindingRef: refOf(ref),
					Severity: f.Severity, HeadSHA: pr.HeadSHA, BaseSHA: pr.Base, Reverify: f.Verified}
				attachProof(&p, f)
				suffix := fmt.Sprintf("%s/%s/k%d", ref, pr.HeadSHA, f.Rechecks)
				out = append(out, Action{Text: ref + " evidence is verified (" + string(f.Tier) + ")",
					Job: &Job{Kind: EffectVerifyEvidence, Key: in.jobKey("verify", suffix), Target: string(in.Change), Payload: p}})
			}
			return out
		}}
}

func refOf(ref string) id.FindingRef {
	r, _ := id.ParseFindingRef(ref)
	return r
}

// attachProof sets the evidence verification repeats: a repro test (E1), a profile check (E2) or a memory lookup (E3).
func attachProof(p *VerifyEvidencePayload, f FindingInfo) {
	switch {
	case f.Tier == finding.TierE3 && f.MemoryRef != nil:
		p.Memory = &VerifyMemory{Ref: *f.MemoryRef, Agent: string(f.By)}
	case f.Tier == finding.TierE2 && f.ToolResult != nil:
		p.Check = &VerifyCheck{ID: f.ToolResult.CheckID, File: f.ToolResult.Location.File, Lines: f.ToolResult.Location.Lines}
	case f.ReproTest != nil:
		p.TestPath, p.TestName = f.ReproTest.Path, f.ReproTest.Name
		p.TestSource = base64.StdEncoding.EncodeToString([]byte(f.ReproTest.Content))
	}
}

func rowDeliberate() Row {
	return Row{ID: "deliberate", Flow: FlowReview, After: []string{"review"},
		Done: func(in In) bool { return len(openCases(in.View)) == 0 },
		Next: func(in In) []Action {
			var out []Action
			for _, c := range openCases(in.View) {
				out = append(out, Action{Task: &Task{Kind: "verdict", Subject: string(c), Input: string(in.View.Cases[c].LastEvent)},
					Text: in.View.Cases[c].Finding + " is deliberated"})
			}
			return out
		}}
}

func openCases(v View) []id.CaseID {
	var out []id.CaseID
	for c, info := range v.Cases {
		if info.Open {
			out = append(out, c)
		}
	}
	slices.Sort(out)
	return out
}

func rowAcceptance() Row {
	return Row{ID: "acceptance", Flow: FlowReview, After: []string{"slop"}, Jobs: []EffectKind{EffectAcceptance},
		Done: func(in In) bool {
			return in.View.Trigger == nil || in.View.PR.CI[in.View.PR.HeadSHA][acceptanceCheck] != ""
		},
		Next: func(in In) []Action {
			pr := in.View.PR
			p := CheckPayload{Repo: pr.Repo, SHA: pr.HeadSHA, Name: acceptanceCheck, Status: CheckStatusQueued,
				Summary: "Heimdall runs every acceptance test on this head."}
			// No Text: the gate's ci clause already says "heimdall/acceptance succeeds on <head>", so Missing does not
			// say it twice.
			return []Action{{
				Job: &Job{Kind: EffectAcceptance, Key: in.jobKey("acceptance", string(pr.HeadSHA)), Target: string(pr.Repo), Payload: p}}}
		}}
}

// gateCheckName is the check the merge rule publishes on the pull request.
const gateCheckName = "heimdall/gate"

// rowGate is never done while the PR is open. It publishes the merge rule's decision whenever it differs from the last
// one recorded for the head: a fact (gate.evaluated) and the heimdall/gate check, keyed by what they say.
func rowGate() Row {
	return Row{ID: "gate", Flow: FlowReview, Jobs: []EffectKind{EffectGitHubCheck},
		Done: func(In) bool { return false },
		Next: func(in In) []Action {
			pr := in.View.PR
			g := EvaluateGate(in.View, in.Outside)
			if g.Same(in.View) {
				return nil
			}
			check := CheckPayload{Repo: pr.Repo, SHA: pr.HeadSHA, Name: gateCheckName, Status: g.Status, Conclusion: g.Conclusion, Summary: g.Summary}
			event := GateEvaluated{OK: g.OK, Reasons: g.Reasons, SHA: pr.HeadSHA, TreeHash: g.TreeHash, PolicyVersion: g.PolicyVersion}
			return []Action{
				{Fact: event},
				{Job: &Job{Kind: EffectGitHubCheck, Key: in.jobKey("gate", string(pr.HeadSHA)+"/"+g.digest()+"/n"+strconv.Itoa(pr.GateCount)), Target: string(pr.Repo), Payload: check}},
			}
		}}
}
