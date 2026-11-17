package change

import (
	"slices"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/slop"
)

// GateFacts turns a view and the outside facts into what the merge rule reads (TECH_SPEC 12.4). It is the one place the
// log is read for the gate: Missing and the gate row both call it. A fact the view cannot give stays zero, and a zero
// fact fails the clause that needs it. A view with no pull request has no facts.
func GateFacts(v View, o Outside) gate.Facts {
	if v.PR == nil {
		return gate.Facts{}
	}
	f := gate.Facts{
		AuthorLogin:    string(v.PR.Author),
		HeadSHA:        v.PR.HeadSHA,
		TicketLinked:   v.PR.Ticket != "",
		Slop:           slopResult(v.PR.Slop),
		Testset:        testset(v),
		CI:             ciOnHead(v),
		ReviewsKnown:   v.PR.Picked != nil && len(v.PR.Picked.People) > 0,
		ReviewRequests: reviewRequests(v),
		BlockingKnown:  true,
		HumanPending:   humanPending(v),
		Approvals:      approvalsOnHead(v),
	}
	f.HeadTestTreeHash = o.HeadTestTreeHash
	f.TestDiffKnown = o.TestDiffKnown
	f.TestDiffOutsideKey = o.TestDiffOutsideKey
	f.CIRequired = o.CIRequired
	f.ApproversForChangedDomains = o.ApproversForChangedDomains
	f.PolicyKnown = o.PolicyKnown
	f.PolicyAtBase = o.PolicyAtBase
	f.PolicyAtHead = o.PolicyAtHead
	f.PolicyOwnerApproved = o.PolicyOwnerApproved
	f.Blocking = gate.BlockingSet(findingStates(v, o.PolicyAtBase.Consensus), blockingFor(o))
	return f
}

// GatePending reports whether the tests or the slop rule have not reported yet. Such a check is in progress, not red; a
// slop rejection is final for its head.
func GatePending(v View) bool {
	if v.PR == nil {
		return false
	}
	if v.PR.Slop != nil && !v.PR.Slop.Pass {
		return false
	}
	t := v.Tests
	return v.PR.Slop == nil || t == nil || t.Verified == nil || t.Committed == nil
}

// slopResult is the slop rule's result as the gate reads it. The event carries rule names only, so each failure's
// message says the fact: the rule failed on this head.
func slopResult(s *SlopInfo) slop.Result {
	if s == nil {
		return slop.Result{}
	}
	r := slop.Result{Pass: s.Pass}
	if s.Pass {
		return r
	}
	for _, name := range s.FailingRules {
		r.Failures = append(r.Failures, slop.Failure{Rule: slop.Rule(name), Message: "the slop rule failed on this head"})
	}
	return r
}

func testset(v View) gate.Testset {
	t := v.Tests
	if t == nil || t.Verified == nil {
		return gate.Testset{ACCovered: t != nil && len(t.ACMap) > 0}
	}
	out := gate.Testset{
		Verified:  t.Committed != nil && len(t.Rejected) == 0,
		ACCovered: len(t.ACMap) > 0,
		Sensitive: t.Verified.NeedsSignoff,
		Approved:  t.ApprovedBy != "",
	}
	if t.Committed != nil {
		out.TreeHash = string(t.Committed.TreeHash)
	}
	return out
}

func ciOnHead(v View) map[string]string {
	out := map[string]string{}
	for name, c := range v.PR.CI[v.PR.HeadSHA] {
		out[name] = string(c)
	}
	return out
}

// reviewRequests are the picked seats: covered when the seat submitted on the current head, waived when a person waived
// it. A seat that was unavailable is neither: it blocks until someone waives it.
func reviewRequests(v View) []gate.ReviewRequest {
	if v.PR.Picked == nil {
		return nil
	}
	out := make([]gate.ReviewRequest, 0, len(v.PR.Picked.People))
	for _, p := range v.PR.Picked.People {
		r := v.Reviews[string(p)]
		out = append(out, gate.ReviewRequest{Reviewer: string(p),
			Covered: r.Outcome == ReviewSubmittedOutcome && r.HeadSHA == v.PR.HeadSHA,
			Waived:  r.Outcome == ReviewWaivedOutcome})
	}
	return out
}

func humanPending(v View) []id.CaseID {
	var out []id.CaseID
	for c, k := range v.Cases {
		if k.Open || k.Escalated {
			out = append(out, c)
		}
	}
	slices.Sort(out)
	return out
}

// approvalsOnHead are the approvals of the current head by someone who is not the author, once per login.
func approvalsOnHead(v View) []gate.Approval {
	var out []gate.Approval
	for _, a := range v.Approvals {
		if a.CommitSHA != v.PR.HeadSHA || strings.EqualFold(string(a.Login), string(v.PR.Author)) {
			continue
		}
		dup := false
		for _, x := range out {
			dup = dup || strings.EqualFold(x.Login, string(a.Login))
		}
		if !dup {
			out = append(out, gate.Approval{Login: string(a.Login), Commit: a.CommitSHA, Approved: true})
		}
	}
	return out
}

// findingStates are the findings in filing order, with the consensus flag set (see withConsensus).
func findingStates(v View, rules policy.Consensus) []gate.FindingState {
	refs := findingRefs(v)
	states := make([]gate.FindingState, 0, len(refs))
	for _, ref := range refs {
		f := v.Findings[ref]
		states = append(states, gate.FindingState{ID: f.ID, Tier: f.Tier, Severity: f.Severity, State: f.State,
			Fingerprint: f.Fingerprint, Reporter: string(f.By), Confidence: f.Confidence})
	}
	return withConsensus(states, rules)
}

// withConsensus sets the consensus flag on each finding whose fingerprint enough distinct reporters hold at high
// confidence (TECH_SPEC 11.2b, D40). Only a live report counts: a dismissed, withdrawn or resolved report is a rejected
// claim, not a second opinion. The weighted score is Later and must not decide a hold today.
func withConsensus(states []gate.FindingState, rules policy.Consensus) []gate.FindingState {
	reporters := map[string]map[string]bool{}
	for _, s := range states {
		if s.Fingerprint == "" || s.Reporter == "" || s.Confidence != finding.ConfidenceHigh || !s.State.CanBlock() {
			continue
		}
		if reporters[s.Fingerprint] == nil {
			reporters[s.Fingerprint] = map[string]bool{}
		}
		reporters[s.Fingerprint][s.Reporter] = true
	}
	out := append([]gate.FindingState(nil), states...)
	for i := range out {
		if out[i].Fingerprint != "" {
			out[i].Consensus = len(reporters[out[i].Fingerprint]) >= rules.MinHighReporters
		}
	}
	return out
}

// blockingFor returns the blocking thresholds of the policy at the merge base (invariant 5), or the strict default when
// the policy is not known or a threshold is one the validator would refuse.
func blockingFor(o Outside) policy.Blocking {
	b := o.PolicyAtBase.Blocking
	if !o.PolicyKnown || b.MinTier == "" || b.MinTier == finding.TierE4 || b.MinSeverity == "" {
		return policy.Blocking{MinSeverity: finding.SeverityLow, MinTier: finding.TierE4}
	}
	return b
}
