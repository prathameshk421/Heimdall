package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/bench"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testintegrity"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// overlaySandbox is the sandbox port with the overlay extension (app/effects OverlaySandbox has the same shape).
type overlaySandbox interface {
	ports.Sandbox
	RunOverlay(ctx context.Context, j ports.SandboxJob, overlays []ports.File) (ports.SandboxResult, error)
}

// replay is one case replayed as a pull request: base is the change's parent and head is the change. The fixed tree
// is the tree where the suspected lines are fixed: the fix commit for a bug case, and the base (the whole change
// reverted) for a clean case.
type replay struct {
	c         bench.Case
	rp        *repo
	sandbox   overlaySandbox
	head      string
	base      string
	fixed     string
	headTree  string
	fixedTree string
	workdir   string           // the head worktree; the reviewer's tools read only this
	changes   []bench.FileDiff // base to head, zero context
	patch     string           // base to head, three lines of context
	truth     *bench.TruthMap  // bug cases: the truth mapped onto the head tree
	proposals []proposal
	cp        *caseProgress // the progress lines of the case; nil in a test that does not read them
}

// prepareReplay fetches the commits, checks the parents, computes the diff, the trees and the mapped truth, and
// checks the head out for the tools. The caller calls close.
func prepareReplay(ctx context.Context, rp *repo, sb overlaySandbox, c bench.Case) (*replay, error) {
	r := &replay{c: c, rp: rp, sandbox: sb, head: c.Change.SHA, base: c.Change.Parent, fixed: c.Change.Parent}
	shas := []string{r.head, r.base}
	if c.Kind == bench.KindBug {
		r.fixed = c.Fix.SHA
		shas = append(shas, c.Fix.SHA, c.Fix.Parent)
	}
	if err := rp.fetch(ctx, shas...); err != nil {
		return nil, err
	}
	if err := rp.checkParent(ctx, r.head, r.base); err != nil {
		return nil, err
	}
	var err error
	if r.changes, err = rp.diff(ctx, r.base, r.head); err != nil {
		return nil, err
	}
	patch, err := rp.git(ctx, "", "--git-dir", rp.gitDir, "diff", "-U3", "--no-color", "--no-renames", "--no-ext-diff", r.base, r.head)
	if err != nil {
		return nil, err
	}
	r.patch = string(patch)
	if r.headTree, err = rp.tree(ctx, r.head); err != nil {
		return nil, err
	}
	if r.fixedTree, err = rp.tree(ctx, r.fixed); err != nil {
		return nil, err
	}
	if c.Kind == bench.KindBug {
		// Truth lines are on the fix parent tree; the reviewer sees the head tree (BENCH.md, the mapping caveat).
		toFixParent, err := rp.diff(ctx, r.head, c.Fix.Parent)
		if err != nil {
			return nil, err
		}
		m := bench.MapTruth(c.Truth, toFixParent)
		r.truth = &m
	}
	if r.workdir, err = rp.worktree(ctx, r.head, c.ID+"-review"); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *replay) close(ctx context.Context) {
	if r.workdir != "" {
		_ = r.rp.remove(context.WithoutCancel(ctx), r.workdir) // a leftover worktree is pruned by the next run
	}
}

func (rp *repo) tree(ctx context.Context, sha string) (string, error) {
	out, err := rp.git(ctx, "", "--git-dir", rp.gitDir, "rev-parse", sha+"^{tree}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// prove runs the proof rule on one proposal: two separate runs on the head and, when both fail, one run on the fixed
// tree. testintegrity.Prove decides. Each run is a fresh worktree, so the two head runs share nothing.
func (r *replay) prove(ctx context.Context, n int, p proposal) bench.ScoredFinding {
	f := bench.ScoredFinding{File: p.File, Lines: p.Lines, Severity: p.Severity, Claim: p.Claim, TestPath: p.TestPath, TestName: p.TestName}
	facts := testintegrity.ProofFacts{HeadTree: r.headTree, FixedTree: r.fixedTree}
	plan := []struct{ label, sha, tree string }{
		{"head-1", r.head, r.headTree}, {"head-2", r.head, r.headTree}, {"fixed-1", r.fixed, r.fixedTree},
	}
	for i, step := range plan {
		if i == 2 && (facts.Runs[0].Outcome != testintegrity.OutcomeFail || facts.Runs[1].Outcome != testintegrity.OutcomeFail) {
			break // Prove reads the fixed tree only after two red head runs
		}
		res, err := r.sandboxRun(ctx, step.sha, p.TestPath, p.TestSource)
		if err != nil {
			f.NotRun = "sandbox: " + err.Error()
			return f
		}
		facts.Runs = append(facts.Runs, testintegrity.Run{ID: fmt.Sprintf("F%d-%s", n, step.label), Tree: step.tree,
			Outcome: outcomeOf(res, p.TestName)})
	}
	f.Runs = facts.Runs
	f.Proof = testintegrity.Prove(facts)
	return f
}

// outcomeOf reads the named test's outcome from a sandbox result. A build failure is a build error. A skip, or a test
// that did not run, is an outcome Prove does not know, and Prove counts it as a build error.
func outcomeOf(res ports.SandboxResult, name string) testintegrity.RunOutcome {
	for _, t := range res.Tests {
		if t.Name != name {
			continue
		}
		switch t.Outcome {
		case ports.OutcomePassed:
			return testintegrity.OutcomePass
		case ports.OutcomeFailure:
			return testintegrity.OutcomeFail
		case ports.OutcomeError:
			return testintegrity.OutcomeBuildError
		case ports.OutcomeSkipped:
			return testintegrity.RunOutcome("skipped")
		}
	}
	if res.Errored > 0 {
		return testintegrity.OutcomeBuildError
	}
	return testintegrity.RunOutcome("did_not_run")
}
