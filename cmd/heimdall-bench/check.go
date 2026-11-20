package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/bench"
)

// caseResult is one case in the results file.
type caseResult struct {
	ID       string            `json:"id"`
	Kind     bench.Kind        `json:"kind"`
	Language string            `json:"language"`
	Outcome  bench.Outcome     `json:"outcome"`
	Reason   string            `json:"reason"`
	Seconds  float64           `json:"seconds"`
	Targets  []string          `json:"targets,omitempty"` // package dir and test name, as "dir:TestName"
	Before   *bench.Run        `json:"before,omitempty"`
	After    *bench.Run        `json:"after,omitempty"`
	Intro    *bench.Evidence   `json:"intro,omitempty"`
	Truth    []bench.FileTruth `json:"truth,omitempty"` // computed from the fix diff
	Manifest string            `json:"manifest"`        // "agrees", or what to change in the manifest
}

// target is the set of fix tests in one Go package directory.
type target struct {
	dir   string // relative to the repository root
	names []string
}

// errorf marks a failure to check, which is not a verdict on the case.
func errorf(format string, args ...any) caseResult {
	return caseResult{Outcome: bench.OutcomeError, Reason: fmt.Sprintf(format, args...)}
}

func invalid(reason string) caseResult {
	return caseResult{Outcome: bench.OutcomeInvalid, Reason: reason}
}

// checkBug proves a bug case: the commits match the manifest, the truth matches the fix diff, and the fix's tests
// fail before the fix and pass after it (bench.Decide).
func checkBug(ctx context.Context, rp *repo, c bench.Case) caseResult {
	fix := *c.Fix
	if err := rp.fetch(ctx, c.Change.SHA, c.Change.Parent, fix.SHA, fix.Parent); err != nil {
		return errorf("fetch: %v", err)
	}
	for _, pair := range [][2]string{{c.Change.SHA, c.Change.Parent}, {fix.SHA, fix.Parent}} {
		if err := rp.checkParent(ctx, pair[0], pair[1]); err != nil {
			return invalid(err.Error())
		}
	}
	files, err := rp.diff(ctx, fix.Parent, fix.SHA)
	if err != nil {
		return errorf("diff: %v", err)
	}
	testFiles := bench.GoTestFiles(files)
	targets, err := fixTargets(ctx, rp, fix.SHA, files)
	if err != nil {
		return errorf("find the fix's tests: %v", err)
	}
	res := caseResult{Truth: bench.GoTruth(files), Targets: targetNames(targets)}
	after, err := runAt(ctx, rp, c, fix.SHA, "after", nil, targets)
	if err != nil {
		return withReason(res, bench.OutcomeError, "after the fix: "+err.Error())
	}
	before, err := runAt(ctx, rp, c, fix.Parent, "before", &copySpec{sha: fix.SHA, paths: testFiles}, targets)
	if err != nil {
		return withReason(res, bench.OutcomeError, "before the fix: "+err.Error())
	}
	res.Before, res.After = &before, &after
	verdict := bench.Decide(res.Targets, before, after)
	switch {
	case !verdict.Valid:
		return withReason(res, bench.OutcomeInvalid, verdict.Reason)
	case len(res.Truth) == 0:
		return withReason(res, bench.OutcomeInvalid, "the fix changes no Go source, so the case has no truth")
	case !slices.EqualFunc(res.Truth, c.Truth, truthEqual):
		return withReason(res, bench.OutcomeInvalid, "the manifest truth differs from the fix diff; see truth")
	}
	res.Intro = introEvidence(ctx, rp, c, testFiles, targets)
	return withReason(res, bench.OutcomeValid, verdict.Reason)
}

func withReason(res caseResult, o bench.Outcome, reason string) caseResult {
	res.Outcome, res.Reason = o, reason
	return res
}

func truthEqual(a, b bench.FileTruth) bool { return a.File == b.File && slices.Equal(a.Lines, b.Lines) }

// introEvidence runs the fix's tests on the intro commit and on its parent. A failure to run is written as the note.
func introEvidence(ctx context.Context, rp *repo, c bench.Case, testFiles []string, targets []target) *bench.Evidence {
	spec := &copySpec{sha: c.Fix.SHA, paths: testFiles}
	intro, err := runAt(ctx, rp, c, c.Change.SHA, "intro", spec, targets)
	if err != nil {
		return &bench.Evidence{Note: "intro: could not run: " + err.Error()}
	}
	parent, err := runAt(ctx, rp, c, c.Change.Parent, "intro-parent", spec, targets)
	if err != nil {
		return &bench.Evidence{Note: "intro parent: could not run: " + err.Error()}
	}
	e := bench.IntroEvidence(targetNames(targets), intro, parent)
	return &e
}

// fixTargets names the top-level tests whose bodies the fix added lines to, grouped by package directory.
func fixTargets(ctx context.Context, rp *repo, fixSHA string, files []bench.FileDiff) ([]target, error) {
	byDir := map[string][]string{}
	var dirs []string
	for _, f := range files {
		if f.Status == bench.FileDeleted || !strings.HasSuffix(f.Path, "_test.go") {
			continue
		}
		src, err := rp.show(ctx, fixSHA, f.Path)
		if err != nil {
			return nil, err
		}
		names, err := bench.TestFuncs(src, bench.AddedLines(f))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.Path, err)
		}
		dir := path.Dir(f.Path)
		if _, ok := byDir[dir]; !ok && len(names) > 0 {
			dirs = append(dirs, dir)
		}
		byDir[dir] = append(byDir[dir], names...)
	}
	var out []target
	for _, d := range dirs {
		out = append(out, target{dir: d, names: byDir[d]})
	}
	return out, nil
}

func targetNames(targets []target) []string {
	var out []string
	for _, t := range targets {
		for _, n := range t.names {
			out = append(out, t.dir+":"+n)
		}
	}
	return out
}

// copySpec names files to copy from another commit into a worktree before the tests run.
type copySpec struct {
	sha   string
	paths []string
}

// runAt checks out sha, copies files in when asked, and runs the targets package by package. The merged Run keys
// each test as "dir:TestName", like targetNames.
func runAt(ctx context.Context, rp *repo, c bench.Case, sha, label string, cp *copySpec, targets []target) (bench.Run, error) {
	merged := bench.Run{Tests: map[string]bench.TestResult{}}
	if len(targets) == 0 {
		return merged, nil
	}
	dir, err := rp.worktree(ctx, sha, c.ID+"-"+label)
	if err != nil {
		return bench.Run{}, err
	}
	defer func() { _ = rp.remove(context.WithoutCancel(ctx), dir) }() // a leftover worktree is pruned by the next run
	if cp != nil {
		if err := rp.copyFrom(ctx, dir, cp.sha, cp.paths); err != nil {
			return bench.Run{}, err
		}
	}
	for _, t := range targets {
		run, err := goTest(ctx, rp.r, dir, t, c)
		if err != nil {
			return bench.Run{}, fmt.Errorf("%s: %w", t.dir, err)
		}
		for name, r := range run.Tests {
			merged.Tests[t.dir+":"+name] = r
		}
		merged.BuildFailed = merged.BuildFailed || run.BuildFailed
		merged.BuildOutput += run.BuildOutput
	}
	return merged, nil
}

// goTest runs one package's targets from the root of the module that holds it. go test's own timeout is the case's
// time limit, so a hung test fails as a test; the context kills the process a minute later.
func goTest(ctx context.Context, r runner, root string, t target, c bench.Case) (bench.Run, error) {
	modRoot, err := moduleRoot(root, t.dir)
	if err != nil {
		return bench.Run{}, err
	}
	rel, err := filepath.Rel(modRoot, filepath.Join(root, filepath.FromSlash(t.dir)))
	if err != nil {
		return bench.Run{}, fmt.Errorf("package path: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, c.Timeout+timeoutGrace)
	defer cancel()
	args := []string{"test", "-json", "-count=1", "-timeout", c.Timeout.String(), "-run", bench.RunPattern(t.names),
		"./" + filepath.ToSlash(rel)}
	out, errOut, runErr := r.run(ctx, modRoot, "go", args...)
	run, err := bench.ParseGoTestJSON(out)
	if err != nil {
		return bench.Run{}, err
	}
	var exitErr interface{ ExitCode() int }
	if runErr != nil && (!errors.As(runErr, &exitErr) || ctx.Err() != nil) {
		return bench.Run{}, fmt.Errorf("go test: %w: %s", runErr, tail(errOut, 5))
	}
	if runErr != nil && len(run.Tests) == 0 && !run.BuildFailed {
		// go test failed before any test or build event: a setup failure such as a module download.
		return bench.Run{}, fmt.Errorf("go test set-up failed: %s", tail(errOut, 5))
	}
	return run, nil
}

// moduleRoot walks up from the package directory to the nearest go.mod, but not above the worktree.
func moduleRoot(root, dir string) (string, error) {
	cur := filepath.Join(root, filepath.FromSlash(dir))
	for {
		_, err := os.Stat(filepath.Join(cur, "go.mod"))
		if err == nil {
			return cur, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("find go.mod: %w", err)
		}
		if cur == root {
			return "", fmt.Errorf("no go.mod above %s", dir)
		}
		cur = filepath.Dir(cur)
	}
}

// checkClean proves a clean case: the commit matches the manifest and its test command passes.
func checkClean(ctx context.Context, rp *repo, c bench.Case) caseResult {
	if err := rp.fetch(ctx, c.Change.SHA, c.Change.Parent); err != nil {
		return errorf("fetch: %v", err)
	}
	if err := rp.checkParent(ctx, c.Change.SHA, c.Change.Parent); err != nil {
		return invalid(err.Error())
	}
	dir, err := rp.worktree(ctx, c.Change.SHA, c.ID+"-merge")
	if err != nil {
		return errorf("checkout: %v", err)
	}
	defer func() { _ = rp.remove(context.WithoutCancel(ctx), dir) }() // a leftover worktree is pruned by the next run
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	argv := strings.Fields(c.TestCommand)
	out, errOut, err := rp.r.run(ctx, dir, argv[0], argv[1:]...)
	switch {
	case ctx.Err() != nil:
		return invalid(fmt.Sprintf("%s did not finish in %v", c.TestCommand, c.Timeout))
	case err != nil:
		return invalid(fmt.Sprintf("%s failed: %v: %s", c.TestCommand, err, tail(append(out, errOut...), 8)))
	}
	return caseResult{Outcome: bench.OutcomeValid, Reason: c.TestCommand + " passes at the merge commit"}
}
