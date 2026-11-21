package action

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testintegrity"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// proveAll proves each proposal, most severe first (ACTION_MODE 6.3), and records every candidate the run had: the
// proposals it proved or dropped, and the claims it never reached.
func (r *proofRun) proveAll(ctx context.Context) {
	for i, p := range orderedProposals(r.proposals) {
		r.res.Candidates = append(r.res.Candidates, r.proveOne(ctx, i, p))
	}
	r.addUnreached()
}

// orderedProposals sorts the proposals by severity, highest first, and keeps the model's order within one severity. The
// most severe claim is proved while the budget lasts (ACTION_MODE 6.3). The sort is an insertion sort because the list is
// at most maxProposals long and the order must be stable.
func orderedProposals(ps []proposal) []proposal {
	out := slices.Clone(ps)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && severityRank(out[j].Severity) > severityRank(out[j-1].Severity); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// severityRank orders severities as the domain does, highest first. A value the domain does not know sorts last, so an
// unknown severity never pushes a real one out of the budget.
func severityRank(s finding.Severity) int {
	switch s {
	case finding.SeverityCritical:
		return 4
	case finding.SeverityHigh:
		return 3
	case finding.SeverityMedium:
		return 2
	case finding.SeverityLow:
		return 1
	}
	return 0
}

// proveOne applies testintegrity.Prove to one proposal: two runs on the head tree and, when both are red, one run on the
// tree with the suspected lines reverted to the base version. Every run is a separate sandbox job, so the two head runs
// share nothing (ACTION_MODE 6.3, A11).
//
// A missing fact stops the whole run unknown: the base version of the lines could not be read, or the sandbox could not
// run. The candidate is then dropped with no proof. It is never proven, and it is never called false.
func (r *proofRun) proveOne(ctx context.Context, n int, p proposal) CandidateOutcome {
	oc := r.candidateOf(p)
	reverted, err := r.revertedOverlay(ctx, p)
	if err != nil {
		r.stop = proof.ReasonInputMissing
		return dropNoProof(oc, err)
	}
	head, fixed := string(r.in.Head), treeID(r.in.Head, reverted)
	if fixed == head {
		// The base version of the lines is the head's own text, so there is nothing to revert and Prove would compare a
		// tree with itself. A test that fails on both proves nothing.
		return dropNoProof(oc, fmt.Errorf("%s lines %d-%d are the same at the base", p.File, p.First, p.Last))
	}
	facts := testintegrity.ProofFacts{HeadTree: head, FixedTree: fixed}
	for i, step := range proofSteps(r.in.Head, r.in.Base, head, fixed, reverted) {
		if r.skipStep(facts, i) {
			break
		}
		res, runErr := r.sandboxRunAt(ctx, step.sha, p.TestPath, p.Test, step.over)
		if runErr != nil {
			r.stop = proof.ReasonSandbox
			return dropNoProof(oc, runErr)
		}
		facts.Runs = append(facts.Runs, testintegrity.Run{ID: fmt.Sprintf("F%d-%s", n+1, step.label),
			Tree: step.tree, Outcome: testOutcomeOf(res, p.TestName)})
	}
	result := testintegrity.Prove(facts)
	oc.Why, oc.Detail = result.Why, result.Reason
	r.mapProof(&oc, result)
	return oc
}

// proofStep is one sandbox run of a proof: which commit, which tree name, and which overlays go in.
type proofStep struct {
	label string
	sha   id.SHA
	tree  string
	over  []ports.File
}

// proofSteps are the three runs of a proof: the first head run, the second, then the reverted tree.
func proofSteps(head, base id.SHA, headTree, fixedTree string, reverted []ports.File) []proofStep {
	return []proofStep{
		{"head-1", head, headTree, nil},
		{"head-2", head, headTree, nil},
		{"fixed-1", base, fixedTree, reverted},
	}
}

// skipStep reports whether the run already knows the answer, so the next step is not worth a sandbox run. It is the same
// order testintegrity.Prove reads the runs in: a head run that did not fail ends the proof, and the fixed tree is read
// only after two red ones.
func (r *proofRun) skipStep(facts testintegrity.ProofFacts, i int) bool {
	switch i {
	case 1:
		return facts.Runs[0].Outcome != testintegrity.OutcomeFail
	case 2:
		return !twoRed(facts.Runs)
	}
	return false
}

// dropNoProof is the outcome of a candidate the run could not test. "No proof" is a drop, so the candidate neither shows
// nor blocks, and the run's own reason says which fact was missing (ACTION_MODE 6.3).
func dropNoProof(oc CandidateOutcome, cause error) CandidateOutcome {
	oc.Class, oc.Dropped, oc.Detail = proof.ClassDropped, proof.DropNoProof, cause.Error()
	return oc
}

// twoRed reports whether the first two head runs both failed with an assertion. Prove reads the fixed tree only then.
func twoRed(runs []testintegrity.Run) bool {
	return len(runs) >= 2 && runs[0].Outcome == testintegrity.OutcomeFail && runs[1].Outcome == testintegrity.OutcomeFail
}

// mapProof turns a testintegrity class into a proof outcome. Only proven shows a finding; not reproduced, flaky and a
// build error are drops with no proof, and none of them says the finding is false (ACTION_MODE 6.3, A11).
func (r *proofRun) mapProof(oc *CandidateOutcome, res testintegrity.ProofResult) {
	switch res.Class {
	case testintegrity.ClassProven:
		oc.Class = proof.ClassProven
		oc.Blocks = blocksUnder(r.in.Policy, oc.Severity)
	case testintegrity.ClassNotReproduced, testintegrity.ClassFlaky, testintegrity.ClassBuildError:
		oc.Class, oc.Dropped = proof.ClassDropped, proof.DropNoProof
	}
}

// testOutcomeOf reads the named test's outcome from a sandbox result. A build failure is a build error. A skip, or a
// test that did not run, is an outcome Prove does not know, and Prove counts it as a build error.
func testOutcomeOf(res ports.SandboxResult, name string) testintegrity.RunOutcome {
	for _, t := range res.Tests {
		if t.Name != name {
			continue
		}
		switch t.Outcome {
		case ports.OutcomePassed:
			return testintegrity.OutcomePass
		case ports.OutcomeFailure:
			return testintegrity.OutcomeFail
		case ports.OutcomeError, ports.OutcomeSkipped:
			return testintegrity.OutcomeBuildError
		}
		return testintegrity.OutcomeBuildError
	}
	if res.Errored > 0 {
		return testintegrity.OutcomeBuildError
	}
	return testintegrity.OutcomeBuildError
}

// sandboxRunAt runs one repro test at one commit with the overlays written in. The job names the head the run serves, so
// the runner cancels this job when a newer head arrives (ports.SandboxJob.Head).
func (r *proofRun) sandboxRunAt(ctx context.Context, sha id.SHA, testPath string, test []byte, over []ports.File) (ports.SandboxResult, error) {
	job := ports.SandboxJob{Kind: ports.SandboxRepro, Repo: r.in.Repo, SHA: sha, Dir: path.Dir(testPath),
		Timeout: unitTimeout, Head: r.in.Head, Change: r.in.ChangeID}
	if err := job.Validate(); err != nil {
		return ports.SandboxResult{}, fmt.Errorf("sandbox job for %s: %w", testPath, err)
	}
	overlays := append(slices.Clone(over), ports.File{Path: testPath, Content: test})
	return r.p.cfg.Sandbox.RunOverlay(ctx, job, overlays)
}

// revertedOverlay reads the base version of the file a proposal points at and returns the overlay that puts the
// proposal's lines back to their base text on the head tree. A line range the base cannot cover is refused: the fixed
// tree would then not exist, and the proof must say so rather than guess.
func (r *proofRun) revertedOverlay(ctx context.Context, p proposal) ([]ports.File, error) {
	head, err := r.headFileBytes(p.File)
	if err != nil {
		return nil, err
	}
	base, err := r.p.cfg.Code.ReadFile(ctx, r.in.Repo, r.in.Base, p.File)
	if err != nil {
		return nil, fmt.Errorf("base version of %s: %w", p.File, err)
	}
	merged, err := revertLines(string(head), string(base), p.First, p.Last)
	if err != nil {
		return nil, fmt.Errorf("revert %s lines %d-%d: %w", p.File, p.First, p.Last, err)
	}
	return []ports.File{{Path: p.File, Content: []byte(merged)}}, nil
}

// headFileBytes reads one file of the head checkout, with the same path rule the tools use.
func (r *proofRun) headFileBytes(p string) ([]byte, error) {
	rv := &reviewer{run: r, checkout: r.in.Checkout}
	return rv.headFile(p)
}

// revertLines returns head with the lines first to last replaced by the base text of the same lines. The base must have
// that many lines: a proposal on a line the base does not have names no suspected line to revert, and the caller reports
// the claim as not checked instead of proving it against a tree that never existed.
func revertLines(head, base string, first, last int) (string, error) {
	headLines := splitLines(head)
	baseLines := splitLines(base)
	if first < 1 || last < first || last > len(headLines) {
		return "", fmt.Errorf("want 1 <= first <= last <= %d lines at the head", len(headLines))
	}
	if last > len(baseLines) {
		return "", fmt.Errorf("the base has %d lines and the range ends at %d", len(baseLines), last)
	}
	out := make([]string, 0, len(headLines)-last+first+len(baseLines[first-1:last]))
	out = append(out, headLines[:first-1]...)
	out = append(out, baseLines[first-1:last]...)
	out = append(out, headLines[last:]...)
	return strings.Join(out, "\n"), nil
}

// splitLines splits text into lines, without the final empty line a trailing newline leaves. A file that ends with a
// newline is written back with one, so the reverted file differs from the head only in the reverted lines.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// addUnreached records every claim no proposal answered. A claim the model had its chance at and did not prove is
// dropped with no proof (ACTION_MODE 6.3: "no proof within the candidate's budget"). A claim the run never reached
// because it stopped for budget or time is not checked, and says which (the same section's "not checked: reason").
func (r *proofRun) addUnreached() {
	reached := map[int64]bool{}
	for _, c := range r.res.Candidates {
		reached[c.CommentID] = true
	}
	for _, claim := range r.in.Claims {
		if reached[claim.CommentID] {
			continue
		}
		oc := CandidateOutcome{CommentID: claim.CommentID, Location: claim.Location, Claim: claim.Claim,
			Severity: claim.Severity}
		if why, stopped := r.stoppedFor(); stopped {
			oc.Class, oc.Reason = proof.ClassNotChecked, why
		} else {
			oc.Class, oc.Dropped = proof.ClassDropped, proof.DropNoProof
		}
		r.res.Candidates = append(r.res.Candidates, oc)
	}
}

// stoppedFor is the reason the run ended before it could finish, and whether it did. Only a run that stopped for a
// missing fact leaves claims unproven rather than dropped.
func (r *proofRun) stoppedFor() (proof.Reason, bool) {
	switch r.stopReason() {
	case proof.ReasonBudget, proof.ReasonMonthlyCap, proof.ReasonDeadline, proof.ReasonInputMissing,
		proof.ReasonSandbox, proof.ReasonCannotIsolate, proof.ReasonForkNoKey:
		return r.stopReason(), true
	case proof.ReasonSuperseded:
		return proof.ReasonDeadline, true
	case "", proof.ReasonStateUnverified, proof.ReasonWorkflowChanged, proof.ReasonNoIntent, proof.ReasonPlanRefused,
		proof.ReasonModelFailure, proof.ReasonNoResult, proof.ReasonTrivial, proof.ReasonNotProvable:
		// A completed run gave every claim its chance, and a failure is reported on the run itself. A claim it never
		// reached had no proof, which is a drop and never a pass.
		return "", false
	}
	return "", false
}

// candidateArtifact is the artifact row for one candidate. The claim is clipped to the domain's bound, so a claim the
// model restated at length cannot push the artifact over its own limit.
func (r *proofRun) candidateArtifact(c CandidateOutcome) ArtifactCandidate {
	ac := ArtifactCandidate{CommentID: c.CommentID, File: c.Location.File,
		First: c.Location.Lines[0], Last: c.Location.Lines[1], Severity: string(c.Severity),
		Claim: clip(c.Claim, maxClaimRunes), Class: c.Class, Dropped: c.Dropped, Reason: c.Reason, Blocking: c.Blocks,
		Why: string(c.Why), Detail: clip(c.Detail, maxDetailBytes), Test: c.Test}
	if c.Ref > 0 {
		ac.Ref = c.Ref.String()
	}
	return ac
}

// artifactOf builds the artifact the gate validates. It carries the counts, the cost and each candidate, and never the
// model's conversation or the test sources: the artifact is bounded, so it holds what a check needs and no more.
func artifactOf(r *proofRun) (ProofArtifact, error) {
	cost, err := change.NewCost(r.res.Spend)
	if err != nil {
		// A spend that cannot be true must never be recorded as a free run: the caller turns this into a failure of the
		// run, so the gate gets no artifact and the sweep closes it as unknown: no result (ACTION_MODE 7, 5.4).
		return ProofArtifact{}, fmt.Errorf("cost of the run: %w", err)
	}
	a := ProofArtifact{Head: r.res.Head, Base: r.res.Base, PolicyHash: r.res.PolicyHash,
		WorkflowRun: r.res.WorkflowRun, Model: r.p.cfg.ModelName, Kind: r.res.Outcome.Kind,
		Reason: r.res.Outcome.Reason, Counts: change.NewProofCounts(r.res.Outcome.Counts), Cost: cost}
	for _, c := range r.res.Candidates {
		a.Candidates = append(a.Candidates, r.candidateArtifact(c))
	}
	return a, nil
}
