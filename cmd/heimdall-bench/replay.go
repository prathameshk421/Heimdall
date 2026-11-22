package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
//
// The fixed tree follows bench.ProofRuleFor. A finding on the truth, and every finding of a clean case, uses the case's
// fixed tree (the fix commit, or the base). A bug case's finding away from the truth uses the product's rule: the head
// with the finding's own lines reverted to the base (testintegrity.RevertLines, as internal/app/action does), so a real
// bug that the fix commit does not touch can still be proven. It is then "proven elsewhere", not a hit.
func (r *replay) prove(ctx context.Context, n int, p proposal) bench.ScoredFinding {
	f := bench.ScoredFinding{File: p.File, Lines: p.Lines, Severity: p.Severity, Claim: p.Claim, TestPath: p.TestPath, TestName: p.TestName}
	var truth []bench.FileTruth
	if r.truth != nil {
		truth = r.truth.Truth
	}
	f.Rule = bench.ProofRuleFor(r.c.Kind, truth, p.File, p.Lines)
	fixedSHA, fixedTree := r.fixed, r.fixedTree
	var reverted []ports.File
	if f.Rule == bench.RuleRevertLines {
		var err error
		if reverted, err = r.revertedOverlay(ctx, p); err != nil {
			f.NotRun = "revert the suspected lines: " + err.Error()
			return f
		}
		fixedSHA, fixedTree = r.head, revertedTreeID(r.headTree, reverted)
	}
	facts := testintegrity.ProofFacts{HeadTree: r.headTree, FixedTree: fixedTree}
	plan := []struct {
		label, sha, tree string
		over             []ports.File
	}{
		{"head-1", r.head, r.headTree, nil}, {"head-2", r.head, r.headTree, nil}, {"fixed-1", fixedSHA, fixedTree, reverted},
	}
	for i, step := range plan {
		if facts.HeadTree == facts.FixedTree {
			break // the lines are the same at the base: Prove says there is no fixed tree, and no run can change that
		}
		if i == 2 && (facts.Runs[0].Outcome != testintegrity.OutcomeFail || facts.Runs[1].Outcome != testintegrity.OutcomeFail) {
			break // Prove reads the fixed tree only after two red head runs
		}
		res, err := r.sandboxRun(ctx, step.sha, p.TestPath, p.TestSource, step.over...)
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

// revertedOverlay is the proposal's file on the head with its lines fixed: either by applying fix_replacement (for bugs
// in newly added files or code) or by reverting its lines to the base. When the result equals head, the overlay is empty.
func (r *replay) revertedOverlay(ctx context.Context, p proposal) ([]ports.File, error) {
	head, err := r.headFile(p.File)
	if err != nil {
		return nil, err
	}
	if p.FixReplacement != "" {
		merged, err := replaceLines(string(head), p.Lines.Start, p.Lines.End, p.FixReplacement)
		if err != nil {
			return nil, fmt.Errorf("apply fix_replacement: %w", err)
		}
		if merged == string(head) {
			return nil, nil
		}
		return []ports.File{{Path: p.File, Content: []byte(merged)}}, nil
	}
	var base []byte
	has, err := r.rp.hasFile(ctx, r.base, p.File)
	if err != nil {
		return nil, err
	}
	if has {
		if base, err = r.rp.show(ctx, r.base, p.File); err != nil {
			return nil, err
		}
	}
	merged, err := testintegrity.RevertLines(string(head), string(base), p.Lines.Start, p.Lines.End)
	if err != nil {
		return nil, err
	}
	if merged == string(head) {
		return nil, nil
	}
	return []ports.File{{Path: p.File, Content: []byte(merged)}}, nil
}

// replaceLines replaces 1-based inclusive lines [first, last] of text with replacement, preserving trailing newline.
func replaceLines(text string, first, last int, replacement string) (string, error) {
	trailingNL := strings.HasSuffix(text, "\n")
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if first < 1 || last < first || last > len(lines) {
		return "", fmt.Errorf("lines %d-%d out of range 1..%d", first, last, len(lines))
	}
	repLines := strings.Split(strings.TrimSuffix(replacement, "\n"), "\n")
	out := make([]string, 0, len(lines)-(last-first+1)+len(repLines))
	out = append(out, lines[:first-1]...)
	out = append(out, repLines...)
	out = append(out, lines[last:]...)
	res := strings.Join(out, "\n")
	if trailingNL {
		res += "\n"
	}
	return res, nil
}

// revertedTreeID names the head with the overlay written in. It is the head tree when the overlay is empty, so Prove
// refuses a fixed tree that is the head itself.
func revertedTreeID(headTree string, reverted []ports.File) string {
	if len(reverted) == 0 {
		return headTree
	}
	sum := sha256.New()
	for _, f := range reverted {
		sum.Write([]byte(f.Path))
		sum.Write([]byte{0})
		sum.Write(f.Content)
		sum.Write([]byte{0})
	}
	return headTree + ":reverted:" + hex.EncodeToString(sum.Sum(nil))[:16]
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
