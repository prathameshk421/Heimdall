package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/bench"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// A cargo or vitest case runs in a plain tree: a worktree of the cache, sparse when the case names patterns, with its
// .git link removed. So an install script (pnpm's prepare runs `vp config`, which writes git config) cannot change the
// cache, and a test cannot read the fix or later history through git. Install runs once per tree and is timed apart
// from the tests.

// plainTree checks sha out into a fresh directory named label and unlinks it from the cache. The caller removes it.
func (rp *repo) plainTree(ctx context.Context, sha, label string, sparse []string) (string, error) {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	dir := filepath.Join(rp.work, label)
	if err := rp.removeLocked(ctx, dir); err != nil {
		return "", err
	}
	if len(sparse) == 0 {
		if _, err := rp.git(ctx, "", "--git-dir", rp.gitDir, "worktree", "add", "--quiet", "--force", "--detach", dir, sha); err != nil {
			return "", err
		}
	} else {
		if _, err := rp.git(ctx, "", "--git-dir", rp.gitDir, "worktree", "add", "--quiet", "--force", "--no-checkout", "--detach", dir, sha); err != nil {
			return "", err
		}
		if _, err := rp.git(ctx, dir, append([]string{"sparse-checkout", "set", "--no-cone"}, sparse...)...); err != nil {
			return "", err
		}
		if _, err := rp.git(ctx, dir, "checkout", "--quiet", "--detach", sha); err != nil {
			return "", err
		}
	}
	if err := os.Remove(filepath.Join(dir, ".git")); err != nil {
		return "", fmt.Errorf("unlink the tree from the cache: %w", err)
	}
	return dir, nil
}

// toolEnv is the environment of install and test commands: no prompts, and every cache under the bench cache, so a
// run never fills the operator's own stores.
func (rp *repo) toolEnv(c bench.Case) []string {
	target := strings.ReplaceAll(c.RepoSlug()+"/"+c.Workdir, "/", "__")
	return []string{
		"CI=1",
		"npm_config_store_dir=" + filepath.Join(rp.cache, "pnpm-store"),
		"CARGO_HOME=" + filepath.Join(rp.cache, "cargo-home"),
		"CARGO_TARGET_DIR=" + filepath.Join(rp.cache, "cargo-target", target),
	}
}

// toolRun runs one command line of the case in the tree's workdir, through env so the environment is explicit.
func (rp *repo) toolRun(ctx context.Context, c bench.Case, tree string, argv []string) (stdout, stderr []byte, err error) {
	args := append(rp.toolEnv(c), argv...)
	return rp.r.run(ctx, filepath.Join(tree, filepath.FromSlash(c.Workdir)), "env", args...)
}

// install runs the case's install steps in the tree and returns how long they took. A failed step is an error, not a
// verdict: the tests never ran.
func (rp *repo) install(ctx context.Context, c bench.Case, tree string) (time.Duration, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	for _, step := range c.Install {
		out, errOut, err := rp.toolRun(ctx, c, tree, strings.Fields(step))
		if err != nil {
			return time.Since(start), fmt.Errorf("install %q: %w: %s", step, err, tail(append(out, errOut...), 6))
		}
	}
	return time.Since(start), nil
}

// testRun is one run of a cargo or vitest case: the parsed result and the runner's raw output.
type testRun struct {
	run    bench.Run
	output string
}

// runTests runs the named tests in the tree and parses the runner's output. No names runs the whole test command.
// A test that fails or does not build is a result. An error means there is no result: a timeout, or a runner that
// failed before any test or build step.
func (rp *repo) runTests(ctx context.Context, c bench.Case, tree, label string, tests []string) (testRun, error) {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout+timeoutGrace)
	defer cancel()
	argv := strings.Fields(c.TestCommand)
	switch c.Runner() {
	case bench.ToolchainCargo:
		if len(tests) > 0 {
			argv = append(append(argv, "--", "--exact"), tests...)
		}
		return rp.runCargo(ctx, c, tree, argv)
	case bench.ToolchainVitest:
		files, err := testFiles(c, tests)
		if err != nil {
			return testRun{}, err
		}
		return rp.runVitest(ctx, c, tree, label, files, argv)
	case bench.ToolchainGo:
	}
	return testRun{}, fmt.Errorf("no plain-tree runner for toolchain %q", c.Runner())
}

// testFiles returns the files of vitest test ids, relative to the workdir, each once.
func testFiles(c bench.Case, tests []string) ([]string, error) {
	var files []string
	for _, id := range tests {
		file, _, ok := bench.SplitTestID(id)
		if !ok {
			return nil, fmt.Errorf("test id %q: want file:name", id)
		}
		rel := strings.TrimPrefix(file, c.Workdir+"/")
		if !slices.Contains(files, rel) {
			files = append(files, rel)
		}
	}
	return files, nil
}

// exitText describes a command's exit for an error message.
func exitText(err error) string {
	if err == nil {
		return "exit status 0"
	}
	return err.Error()
}

func (rp *repo) runCargo(ctx context.Context, c bench.Case, tree string, argv []string) (testRun, error) {
	out, errOut, err := rp.toolRun(ctx, c, tree, argv)
	if ctx.Err() != nil {
		return testRun{}, fmt.Errorf("cargo test did not finish in %v: %w", c.Timeout, ctx.Err())
	}
	run, perr := bench.ParseLibtest(out, errOut)
	if perr != nil {
		return testRun{}, fmt.Errorf("cargo test (%s): %w", exitText(err), perr)
	}
	return testRun{run: run, output: string(out) + string(errOut)}, nil
}

// runVitest runs vitest on the files (relative to the workdir; none means the whole suite) and parses its report.
func (rp *repo) runVitest(ctx context.Context, c bench.Case, tree, label string, files, argv []string) (testRun, error) {
	report := filepath.Join(rp.work, label+"-vitest.json")
	if err := os.Remove(report); err != nil && !errors.Is(err, os.ErrNotExist) {
		return testRun{}, fmt.Errorf("remove old report: %w", err)
	}
	argv = append(append(argv, files...), "--reporter=json", "--outputFile="+report)
	out, errOut, err := rp.toolRun(ctx, c, tree, argv)
	if ctx.Err() != nil {
		return testRun{}, fmt.Errorf("vitest did not finish in %v: %w", c.Timeout, ctx.Err())
	}
	b, rerr := os.ReadFile(report) // #nosec G304 -- the bench names the report file under its own cache
	if rerr != nil {
		return testRun{}, fmt.Errorf("vitest wrote no report (%s): %s", exitText(err), tail(append(out, errOut...), 6))
	}
	root, lerr := filepath.EvalSymlinks(tree)
	if lerr != nil {
		return testRun{}, fmt.Errorf("resolve tree: %w", lerr)
	}
	run, perr := bench.ParseVitestJSON(b, root)
	if perr != nil {
		return testRun{}, perr
	}
	return testRun{run: run, output: string(out) + string(errOut)}, nil
}

// writeFile writes one repository file into a plain tree. The path must be local to the tree.
func writeFile(tree, p string, content []byte) error {
	return writeOverlay(tree, ports.File{Path: p, Content: content})
}

// putFixTests puts the fix's tests into a tree checked out at another commit: vitest test files and cargo
// integration tests whole, and each Rust file's inline test module spliced onto the tree's own code.
func (rp *repo) putFixTests(ctx context.Context, c bench.Case, tree string, files []bench.FileDiff) error {
	whole := bench.ToolchainTestFiles(c.Runner(), files)
	for _, p := range whole {
		src, err := rp.show(ctx, c.Fix.SHA, p)
		if err != nil {
			return err
		}
		if err := writeFile(tree, p, src); err != nil {
			return err
		}
	}
	if c.Runner() != bench.ToolchainCargo {
		return nil
	}
	for _, f := range files {
		if f.Status == bench.FileDeleted || !strings.HasSuffix(f.Path, ".rs") || slices.Contains(whole, f.Path) {
			continue
		}
		if err := rp.spliceFixTests(ctx, c, tree, f.Path); err != nil {
			return fmt.Errorf("%s: %w", f.Path, err)
		}
	}
	return nil
}

// spliceFixTests writes the fix's inline test module of one Rust file onto the tree's own version of it. A fix file
// with no test module, or a file the tree does not have, is left alone.
func (rp *repo) spliceFixTests(ctx context.Context, c bench.Case, tree, p string) error {
	fix, err := rp.show(ctx, c.Fix.SHA, p)
	if err != nil {
		return err
	}
	if bench.RustTestStart(fix) == 0 {
		return nil
	}
	base, err := os.ReadFile(filepath.Join(tree, filepath.FromSlash(p))) // #nosec G304 -- a path of the fix diff inside the bench's own tree
	if errors.Is(err, os.ErrNotExist) {
		return nil // the fix adds the file; its tests cannot run before the fix
	}
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	spliced, err := bench.SpliceRustTests(base, fix)
	if err != nil {
		return err
	}
	return writeFile(tree, p, spliced)
}

// applyDiff writes the files of commit sha that the diff changed into the tree, and deletes the ones it deleted.
func (rp *repo) applyDiff(ctx context.Context, tree, sha string, files []bench.FileDiff) error {
	for _, f := range files {
		full := filepath.Join(tree, filepath.FromSlash(f.Path))
		if f.Status == bench.FileDeleted {
			if err := os.Remove(full); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("delete %s: %w", f.Path, err)
			}
			continue
		}
		src, err := rp.show(ctx, sha, f.Path)
		if err != nil {
			return err
		}
		if err := writeFile(tree, f.Path, src); err != nil {
			return err
		}
	}
	return nil
}

// toolTruth computes the truth of a fix diff for the case's toolchain. For Rust it reads each changed file on both
// sides to find its inline test module.
func toolTruth(ctx context.Context, rp *repo, c bench.Case, files []bench.FileDiff) ([]bench.FileTruth, error) {
	tests := map[string]bench.TestModule{}
	if c.Runner() == bench.ToolchainCargo {
		for _, f := range files {
			if f.Status != bench.FileModified || !strings.HasSuffix(f.Path, ".rs") {
				continue
			}
			old, err := rp.show(ctx, c.Fix.Parent, f.Path)
			if err != nil {
				return nil, err
			}
			fixed, err := rp.show(ctx, c.Fix.SHA, f.Path)
			if err != nil {
				return nil, err
			}
			tests[f.Path] = bench.TestModule{Old: bench.RustTestStart(old), New: bench.RustTestRegion(fixed)}
		}
	}
	return bench.SourceTruth(c.Runner(), files, tests), nil
}

// timing is how long a toolchain case spent installing and testing.
type timing struct {
	InstallSeconds float64 `json:"install_seconds"`
	TestSeconds    float64 `json:"test_seconds"`
}

func (t *timing) addInstall(d time.Duration) { t.InstallSeconds += round(d) }
func (t *timing) addTest(d time.Duration)    { t.TestSeconds += round(d) }

func round(d time.Duration) float64 { return d.Round(100 * time.Millisecond).Seconds() }

// checkToolBug proves a cargo or vitest bug case: the commits match the manifest, the fix's files hold the named
// tests, the truth matches the fix diff, and the tests fail before the fix and pass after it (bench.Decide).
func checkToolBug(ctx context.Context, rp *repo, c bench.Case) caseResult {
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
	truth, err := toolTruth(ctx, rp, c, files)
	if err != nil {
		return errorf("truth: %v", err)
	}
	res := caseResult{Truth: truth, Targets: c.Tests, Timing: &timing{}}
	if missing := rp.testsNotInFix(ctx, c, files); len(missing) > 0 {
		return withReason(res, bench.OutcomeInvalid, "the fix's changed files do not hold the tests "+strings.Join(missing, ", "))
	}
	if reason := rp.beforeAndAfter(ctx, c, files, &res); reason != "" {
		return withReason(res, bench.OutcomeError, reason)
	}
	verdict := bench.Decide(c.Tests, *res.Before, *res.After)
	switch {
	case !verdict.Valid:
		return withReason(res, bench.OutcomeInvalid, verdict.Reason)
	case len(res.Truth) == 0:
		return withReason(res, bench.OutcomeInvalid, "the fix changes no source file, so the case has no truth")
	case !slices.EqualFunc(res.Truth, c.Truth, truthEqual):
		return withReason(res, bench.OutcomeInvalid, "the manifest truth differs from the fix diff; see truth")
	}
	res.Intro = toolIntroEvidence(ctx, rp, c, files)
	return withReason(res, bench.OutcomeValid, verdict.Reason)
}

// beforeAndAfter runs the targets twice in one tree: the fix parent with the fix's tests put in (before), then the
// same tree with the whole fix written over it (after). So install runs once, and again only when the fix changes an
// install input. It fills res.Before and res.After, or returns why it could not.
func (rp *repo) beforeAndAfter(ctx context.Context, c bench.Case, files []bench.FileDiff, res *caseResult) string {
	tree, err := rp.plainTree(ctx, c.Fix.Parent, c.ID+"-fix", c.Sparse)
	if err != nil {
		return "checkout: " + err.Error()
	}
	defer func() { _ = rp.remove(context.WithoutCancel(ctx), tree) }() // a leftover tree is pruned by the next run
	if err := rp.putFixTests(ctx, c, tree, files); err != nil {
		return "put the fix's tests: " + err.Error()
	}
	if err := rp.timedInstall(ctx, c, tree, res.Timing); err != nil {
		return err.Error()
	}
	before, err := rp.timedTests(ctx, c, tree, "before", res.Timing)
	if err != nil {
		return "before the fix: " + err.Error()
	}
	if err := rp.applyDiff(ctx, tree, c.Fix.SHA, files); err != nil {
		return "apply the fix: " + err.Error()
	}
	if bench.ChangesInstall(files) {
		if err := rp.timedInstall(ctx, c, tree, res.Timing); err != nil {
			return "after the fix: " + err.Error()
		}
	}
	after, err := rp.timedTests(ctx, c, tree, "after", res.Timing)
	if err != nil {
		return "after the fix: " + err.Error()
	}
	res.Before, res.After = &before, &after
	return ""
}

func (rp *repo) timedInstall(ctx context.Context, c bench.Case, tree string, t *timing) error {
	d, err := rp.install(ctx, c, tree)
	t.addInstall(d)
	return err
}

func (rp *repo) timedTests(ctx context.Context, c bench.Case, tree, label string, t *timing) (bench.Run, error) {
	start := time.Now()
	tr, err := rp.runTests(ctx, c, tree, c.ID+"-"+label, c.Tests)
	t.addTest(time.Since(start))
	return tr.run, err
}

// testsNotInFix returns the manifest tests the fix did not touch. A vitest test must sit in a test file the fix added
// or changed: it.each and describe.each format their titles, so a title need not appear in the source. A cargo test's
// function name must appear in a Rust file the fix changed.
func (rp *repo) testsNotInFix(ctx context.Context, c bench.Case, files []bench.FileDiff) []string {
	changed := map[string]bool{}
	var texts []string
	for _, f := range files {
		if f.Status == bench.FileDeleted {
			continue
		}
		changed[f.Path] = true
		if c.Runner() != bench.ToolchainCargo || !strings.HasSuffix(f.Path, ".rs") {
			continue
		}
		if src, err := rp.show(ctx, c.Fix.SHA, f.Path); err == nil {
			texts = append(texts, string(src))
		}
	}
	var missing []string
	for _, id := range c.Tests {
		if c.Runner() == bench.ToolchainVitest {
			if file, _, ok := bench.SplitTestID(id); !ok || !changed[file] {
				missing = append(missing, id)
			}
			continue
		}
		leaf := bench.TestLeaf(id)
		if !slices.ContainsFunc(texts, func(s string) bool { return strings.Contains(s, "fn "+leaf+"(") }) {
			missing = append(missing, id)
		}
	}
	return missing
}

// toolIntroEvidence runs the fix's tests on the intro commit and on its parent, each in its own tree with its own
// install. A failure to run is written as the note.
func toolIntroEvidence(ctx context.Context, rp *repo, c bench.Case, files []bench.FileDiff) *bench.Evidence {
	runs := make([]bench.Run, 2)
	for i, step := range []struct{ sha, label string }{{c.Change.SHA, "intro"}, {c.Change.Parent, "intro-parent"}} {
		run, err := rp.toolRunAt(ctx, c, step.sha, step.label, files)
		if err != nil {
			return &bench.Evidence{Note: step.label + ": could not run: " + err.Error()}
		}
		runs[i] = run
	}
	e := bench.IntroEvidence(c.Tests, runs[0], runs[1])
	return &e
}

// toolRunAt checks sha out with the fix's tests put in, installs and runs the targets.
func (rp *repo) toolRunAt(ctx context.Context, c bench.Case, sha, label string, files []bench.FileDiff) (bench.Run, error) {
	tree, err := rp.plainTree(ctx, sha, c.ID+"-"+label, c.Sparse)
	if err != nil {
		return bench.Run{}, err
	}
	defer func() { _ = rp.remove(context.WithoutCancel(ctx), tree) }()
	if err := rp.putFixTests(ctx, c, tree, files); err != nil {
		return bench.Run{}, err
	}
	if _, err := rp.install(ctx, c, tree); err != nil {
		return bench.Run{}, err
	}
	tr, err := rp.runTests(ctx, c, tree, c.ID+"-"+label, c.Tests)
	return tr.run, err
}

// checkToolClean proves a cargo or vitest clean case: the commit matches the manifest, it installs, and its whole test
// command passes.
func checkToolClean(ctx context.Context, rp *repo, c bench.Case) caseResult {
	if err := rp.fetch(ctx, c.Change.SHA, c.Change.Parent); err != nil {
		return errorf("fetch: %v", err)
	}
	if err := rp.checkParent(ctx, c.Change.SHA, c.Change.Parent); err != nil {
		return invalid(err.Error())
	}
	res := caseResult{Timing: &timing{}}
	tree, err := rp.plainTree(ctx, c.Change.SHA, c.ID+"-merge", c.Sparse)
	if err != nil {
		return withReason(res, bench.OutcomeError, "checkout: "+err.Error())
	}
	defer func() { _ = rp.remove(context.WithoutCancel(ctx), tree) }()
	if err := rp.timedInstall(ctx, c, tree, res.Timing); err != nil {
		return withReason(res, bench.OutcomeError, err.Error())
	}
	start := time.Now()
	tr, err := rp.runTests(ctx, c, tree, c.ID+"-merge", nil)
	res.Timing.addTest(time.Since(start))
	if err != nil {
		return withReason(res, bench.OutcomeError, err.Error())
	}
	run := tr.run
	res.After = &run
	if reason := cleanFailure(run); reason != "" {
		return withReason(res, bench.OutcomeInvalid, c.TestCommand+" fails at the merge commit: "+reason)
	}
	return withReason(res, bench.OutcomeValid, fmt.Sprintf("%s passes at the merge commit (%d tests)", c.TestCommand, len(run.Tests)))
}

// cleanFailure says why a whole-suite run is not green, or returns "" when it is: no build failure, at least one
// test, and no failing test.
func cleanFailure(run bench.Run) string {
	if run.BuildFailed {
		return "build failure: " + tail([]byte(run.BuildOutput), 3)
	}
	if len(run.Tests) == 0 {
		return "no test ran"
	}
	var failed []string
	for name, r := range run.Tests {
		if r == bench.ResultFail {
			failed = append(failed, name)
		}
	}
	if len(failed) == 0 {
		return ""
	}
	slices.Sort(failed)
	if len(failed) > 5 {
		failed = append(failed[:5], fmt.Sprintf("and %d more", len(failed)-5))
	}
	return strings.Join(failed, ", ")
}
