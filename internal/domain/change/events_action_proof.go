package change

import (
	"errors"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
)

// ProofReported is the payload of proof.reported: a proof run finished (ACTION_MODE 5.2, 6.3, 7). It carries the
// outcome of proof.Evaluate (Kind, Reason, Counts, Cost) and what became of each candidate finding that has a name in
// the log (Results). Candidates dropped before they became findings, as outdated or with no location, are only in
// the counts. A budget hit, a timeout or a superseded run is a Kind of unknown with its Reason, not an event of its own.
// The Model is empty for a run that made no model call. WorkflowRun is the run's GitHub Actions id.
type ProofReported struct {
	WorkflowRun int64             `json:"workflow_run"`
	Head        id.SHA            `json:"head"`
	Base        id.SHA            `json:"base"`
	PolicyHash  string            `json:"policy_hash"`
	Model       string            `json:"model,omitempty"`
	Kind        proof.Kind        `json:"kind"`
	Reason      proof.Reason      `json:"reason,omitempty"`
	Counts      ProofCounts       `json:"counts"`
	Cost        Cost              `json:"cost"`
	Results     []CandidateResult `json:"results,omitempty"`
}

// CandidateResult is what became of one candidate finding (ACTION_MODE 6.3): proven, dropped with a reason, or not
// checked with a reason. Blocking is set only for a proven finding, by the caller who applied gate.BlockingSet.
type CandidateResult struct {
	Ref      id.FindingRef    `json:"ref"`
	Class    proof.Class      `json:"class"`
	Dropped  proof.DropReason `json:"dropped,omitempty"`
	Reason   proof.Reason     `json:"reason,omitempty"`
	Blocking bool             `json:"blocking,omitempty"`
}

// Validate returns every problem with the payload.
func (p ProofReported) Validate() error {
	var v problems
	v.add("workflow_run", checkWorkflowRun(p.WorkflowRun))
	v.add("head", checkSHA(p.Head))
	v.add("base", checkSHA(p.Base))
	v.add("policy_hash", checkPolicyHash(p.PolicyHash))
	if p.Model != "" {
		v.add("model", checkText(p.Model))
	}
	v.add("kind", checkEnum(p.Kind, validKind(p.Kind)))
	v.add("counts", p.Counts.Validate())
	v.add("cost", p.Cost.Validate())
	p.checkOutcome(&v)
	p.checkResults(&v)
	return v.join()
}

func validKind(k proof.Kind) bool {
	return oneOf(k, proof.KindSkippedTrivial, proof.KindProven, proof.KindBlocked, proof.KindNotReproduced, proof.KindUnknown)
}

// checkOutcome checks that the kind, the reason, the counts and the cost agree, as proof.Evaluate makes them agree.
func (p ProofReported) checkOutcome(v *problems) {
	if !validKind(p.Kind) {
		return
	}
	if p.Kind == proof.KindUnknown {
		v.require("reason", p.Reason.CanStop(), "an unknown run needs the reason that stopped it")
	} else {
		v.require("reason", p.Reason == "", "only an unknown run has a reason")
	}
	switch p.Kind {
	case proof.KindBlocked:
		v.require("kind", p.Counts.Blocking > 0, "a blocked run needs a blocking finding")
	case proof.KindProven, proof.KindNotReproduced:
		v.require("kind", p.Counts.Blocking == 0, "a run with a blocking finding is blocked or unknown")
	case proof.KindSkippedTrivial:
		v.require("counts", p.Counts == ProofCounts{}, "a skipped run has no candidates")
		v.require("cost", p.Cost == Cost{}, "a skipped run costs nothing")
	case proof.KindUnknown:
		// An unknown run may hold any counts: it stopped wherever it stopped.
	}
}

func (p ProofReported) checkResults(v *problems) {
	v.require("results", len(p.Results) <= MaxProofResults, fmt.Sprintf("must hold at most %d items", MaxProofResults))
	v.require("results", len(p.Results) <= p.Counts.In, "has more results than the counts have candidates")
	seen := map[id.FindingRef]bool{}
	for i, r := range p.Results {
		at := fmt.Sprintf("results[%d]", i)
		v.add(at+".ref", checkFindingRef(r.Ref))
		v.require(at+".ref", !seen[r.Ref], "appears twice")
		seen[r.Ref] = true
		r.check(at, v)
	}
}

// check reports a result whose class, drop reason, not-checked reason and blocking flag do not fit each other.
func (r CandidateResult) check(at string, v *problems) {
	switch r.Class {
	case proof.ClassProven:
		v.add(at, noExtras(r.Dropped == "" && r.Reason == "", "a proven finding has no drop reason and no not-checked reason"))
	case proof.ClassDropped:
		v.add(at+".dropped", checkEnum(r.Dropped, validDrop(r.Dropped)))
		v.add(at, noExtras(r.Reason == "" && !r.Blocking, "a dropped finding has no not-checked reason and does not block"))
	case proof.ClassNotChecked:
		v.require(at+".reason", notCheckedReason(r.Reason), fmt.Sprintf("%q is not a reason to leave a candidate not checked", r.Reason))
		v.add(at, noExtras(r.Dropped == "" && !r.Blocking, "a finding that was not checked has no drop reason and does not block"))
	default:
		v.add(at+".class", checkEnum(r.Class, false))
	}
}

// noExtras is nil when ok, and an error with msg otherwise.
func noExtras(ok bool, msg string) error {
	if ok {
		return nil
	}
	return errors.New(msg)
}

func validDrop(d proof.DropReason) bool {
	return oneOf(d, proof.DropRefuted, proof.DropNoProof, proof.DropOutdated, proof.DropNoLocation)
}

// notCheckedReason is the list of reasons a candidate may carry when it is not checked (ACTION_MODE 6.3): by design
// (trivial PR, a style claim) or for lack of budget, time, a key or isolation.
func notCheckedReason(r proof.Reason) bool {
	return oneOf(r, proof.ReasonTrivial, proof.ReasonNotProvable, proof.ReasonBudget, proof.ReasonDeadline, proof.ReasonForkNoKey,
		proof.ReasonCannotIsolate)
}
