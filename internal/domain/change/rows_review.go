package change

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

// acceptanceCheck is the check Heimdall's own run of every Heimdall test reports under (TECH_SPEC 12.4, D150).
const acceptanceCheck = "heimdall/acceptance"

func reviewRows() []Row {
	return []Row{rowSlop(), rowChecks(), rowPick(), rowReview(), rowVerify(), rowDeliberate(), rowAcceptance(), rowGate()}
}

func rowSlop() Row {
	return Row{ID: "slop", Flow: FlowReview,
		Done: func(in In) bool { return slopPassedOnHead(in.View) },
		Next: func(in In) []Action { return []Action{say(slopText(in.View.PR))} }}
}

func slopPassedOnHead(v View) bool {
	return v.PR != nil && v.PR.Slop != nil && v.PR.Slop.SHA == v.PR.HeadSHA && v.PR.Slop.Pass
}

func slopText(pr *PRInfo) string {
	h := short(string(pr.HeadSHA))
	if pr.Slop != nil && pr.Slop.SHA == pr.HeadSHA {
		return "the slop rules pass on " + h + " (failing: " + strings.Join(pr.Slop.FailingRules, ", ") + ")"
	}
	return "the slop rules are evaluated on " + h
}

// applicableChecks are the ids of the profile checks whose `when` matches a changed file (all when `when` is empty).
func applicableChecks(in In) []string {
	var ids []string
	for _, c := range in.Profile.Checks {
		if len(c.When) == 0 || anyFileMatches(c.When, in.Outside.ChangedFiles) {
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
	return Row{ID: "checks", Flow: FlowReview, After: []string{"slop"},
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
	return Row{ID: "verify", Flow: FlowReview, After: []string{"review"},
		Done: func(in In) bool { return len(unchecked(in.View)) == 0 },
		Next: func(in In) []Action {
			pr := in.View.PR
			var out []Action
			for _, ref := range unchecked(in.View) {
				f := in.View.Findings[ref]
				p := VerifyEvidencePayload{Repo: pr.Repo, Ticket: in.Change, FindingID: f.ID, FindingRef: refOf(ref),
					Severity: f.Severity, HeadSHA: pr.HeadSHA}
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
		p.Check = &VerifyCheck{Name: checkNameFor(f.ToolResult.Tool), File: f.ToolResult.Location.File, Lines: f.ToolResult.Location.Lines}
	case f.ReproTest != nil:
		p.TestPath, p.TestName = f.ReproTest.Path, f.ReproTest.Name
		p.TestSource = base64Of(f.ReproTest.Content)
	}
}

// base64Of is the standard base64 text of s, which is how a repro test travels in a verify_evidence payload. encoding/json
// writes a byte slice exactly so, and the domain may import it where it may not import encoding/base64.
func base64Of(s string) string {
	b, err := json.Marshal([]byte(s))
	if err != nil || len(b) < 2 {
		return ""
	}
	return string(b[1 : len(b)-1])
}

// checkNameFor maps the tool a reviewer names in tool_result evidence to the fixed check the runner knows. It goes with
// the static job when profile checks replace it (HARNESS_DESIGN B7, H8).
func checkNameFor(tool string) string {
	if tool == "go vet" {
		return CheckGoVet
	}
	return tool
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
	return Row{ID: "acceptance", Flow: FlowReview, After: []string{"slop"},
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

// rowGate is never done while the PR is open. What it decides (the merge rule on the view) is added with the gate
// facts (HARNESS_DESIGN H6); until then Missing reads the gate's clauses from gateNeeds.
func rowGate() Row {
	return Row{ID: "gate", Flow: FlowReview,
		Done: func(In) bool { return false },
		Next: func(In) []Action { return nil }}
}
