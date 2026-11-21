package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/bench"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// sandboxNote goes into every review report, so nobody reads a bench result as a sandbox result.
const sandboxNote = "local go test in a fresh git worktree per run (cargo test or vitest in a fresh plain tree, after " +
	"the case's install), as the operator's user with the network on; the uid sandbox (ACTION_MODE 4.4) is not used locally"

// maxTestOutput bounds the go test output a run returns to the reviewer.
const maxTestOutput = 8 << 10

// localSandbox runs one Go package's tests in a fresh worktree of a cached repository, or, for a cargo or vitest case,
// the reviewer's test file in a fresh plain tree (toolchain.go) after the case's install steps. It implements ports.Sandbox
// and the overlay extension that app/effects calls OverlaySandbox, so the reviewer's tools see the product's port.
// It is not isolated: see sandboxNote.
type localSandbox struct {
	repos map[string]*repo      // by owner/name
	cases map[string]bench.Case // the case under review, by owner/name: it names the runner of a cargo or vitest repo
	seq   int
}

var _ ports.Sandbox = (*localSandbox)(nil)

func newLocalSandbox() *localSandbox {
	return &localSandbox{repos: map[string]*repo{}, cases: map[string]bench.Case{}}
}

// add registers the case under review and its cache. Each case of a review has its own sandbox, so the newest case of
// a repository is the one its jobs serve.
func (s *localSandbox) add(c bench.Case, rp *repo) {
	s.repos[c.RepoSlug()], s.cases[c.RepoSlug()] = rp, c
}

// nextLabel names the tree of the next run: the case and the run's number in this sandbox. The cases of one repository
// share its worktree parent in a parallel run, so the case id keeps their trees apart.
func (s *localSandbox) nextLabel(repo string) string {
	s.seq++
	return "sbx-" + s.cases[repo].ID + "-" + strconv.Itoa(s.seq)
}

// Run runs the job's package tests at the job's commit, with no overlay.
func (s *localSandbox) Run(ctx context.Context, j ports.SandboxJob) (ports.SandboxResult, error) {
	return s.RunOverlay(ctx, j, nil)
}

// RunOverlay checks the job's commit out, writes the overlay files, and runs go test -json on the job's package
// directory from the root of the module that holds it. An error means there is no result at all. A test that does not
// build, fails or panics is a result.
func (s *localSandbox) RunOverlay(ctx context.Context, j ports.SandboxJob, overlays []ports.File) (ports.SandboxResult, error) {
	if err := j.Validate(); err != nil {
		return ports.SandboxResult{}, fmt.Errorf("sandbox job: %w", err)
	}
	if j.Kind != ports.SandboxRepro {
		return ports.SandboxResult{}, fmt.Errorf("sandbox job kind %q: the bench runs repro jobs only", j.Kind)
	}
	rp, ok := s.repos[string(j.Repo)]
	if !ok {
		return ports.SandboxResult{}, fmt.Errorf("sandbox: no cache for %s", j.Repo)
	}
	label := s.nextLabel(string(j.Repo))
	if c := s.cases[string(j.Repo)]; c.Runner() == bench.ToolchainCargo || c.Runner() == bench.ToolchainVitest {
		return s.runTool(ctx, rp, c, j, overlays, label)
	}
	dir, err := rp.worktree(ctx, string(j.SHA), label)
	if err != nil {
		return ports.SandboxResult{}, err
	}
	defer func() { _ = rp.remove(context.WithoutCancel(ctx), dir) }() // a leftover worktree is pruned by the next run
	// The copy has no link to the cache, so a test cannot run git on it to read the fix or later history. A test can
	// still read the cache by its path: only the uid sandbox closes that (sandboxNote).
	if err := os.Remove(filepath.Join(dir, ".git")); err != nil {
		return ports.SandboxResult{}, fmt.Errorf("unlink the copy from the cache: %w", err)
	}
	for _, f := range overlays {
		if err := writeOverlay(dir, f); err != nil {
			return ports.SandboxResult{}, err
		}
	}
	return s.goTest(ctx, rp.r, dir, j)
}

// writeOverlay writes one file inside the worktree. The path must be local to it.
func writeOverlay(dir string, f ports.File) error {
	if !filepath.IsLocal(f.Path) || strings.HasPrefix(filepath.ToSlash(f.Path), ".git/") {
		return fmt.Errorf("overlay path %q is not inside the repository", f.Path)
	}
	full := filepath.Join(dir, filepath.FromSlash(f.Path))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		return fmt.Errorf("overlay dir: %w", err)
	}
	if err := os.WriteFile(full, f.Content, 0o600); err != nil {
		return fmt.Errorf("write overlay: %w", err)
	}
	return nil
}

func (s *localSandbox) goTest(ctx context.Context, r runner, root string, j ports.SandboxJob) (ports.SandboxResult, error) {
	modRoot, err := moduleRoot(root, j.Dir)
	if err != nil {
		return ports.SandboxResult{}, err
	}
	rel, err := filepath.Rel(modRoot, filepath.Join(root, filepath.FromSlash(j.Dir)))
	if err != nil {
		return ports.SandboxResult{}, fmt.Errorf("package path: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, j.Timeout+timeoutGrace)
	defer cancel()
	out, errOut, runErr := r.run(ctx, modRoot, "go", "test", "-json", "-count=1", "-timeout", j.Timeout.String(),
		"./"+filepath.ToSlash(rel))
	run, err := bench.ParseGoTestJSON(out)
	if err != nil {
		return ports.SandboxResult{}, err
	}
	var exitErr interface{ ExitCode() int }
	if runErr != nil && (!errors.As(runErr, &exitErr) || ctx.Err() != nil) {
		return ports.SandboxResult{}, fmt.Errorf("go test: %w: %s", runErr, tail(errOut, 5))
	}
	if runErr != nil && len(run.Tests) == 0 && !run.BuildFailed {
		return ports.SandboxResult{}, fmt.Errorf("go test set-up failed: %s", tail(errOut, 5))
	}
	res := sandboxResult(run)
	res.Output = boundTail(testOutput(out)+run.BuildOutput+string(errOut), maxTestOutput)
	sum := sha256.Sum256(append(out, errOut...))
	res.LogRef = workflow.BlobRef(fmt.Sprintf("blob:sha256:%x", sum))
	return res, nil
}

// sandboxResult maps the parsed go test run to the port's result. A build failure is one errored entry, the JUnit
// <error> the port expects.
func sandboxResult(run bench.Run) ports.SandboxResult {
	var res ports.SandboxResult
	for name, r := range run.Tests {
		t := ports.TestResult{Name: name}
		switch r {
		case bench.ResultPass:
			t.Outcome, res.Passed = ports.OutcomePassed, res.Passed+1
		case bench.ResultFail:
			t.Outcome, res.Failed = ports.OutcomeFailure, res.Failed+1
		case bench.ResultSkip:
			t.Outcome = ports.OutcomeSkipped
		}
		res.Tests = append(res.Tests, t)
	}
	if run.BuildFailed {
		res.Errored++
		res.Tests = append(res.Tests, ports.TestResult{Name: "[build]", Outcome: ports.OutcomeError})
	}
	return res
}

// testOutput joins the test output lines of go test -json events. Build output is in bench.Run already. A line that
// is not an event is kept as it is.
func testOutput(raw []byte) string {
	var b strings.Builder
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 64<<10), 16<<20)
	for sc.Scan() {
		var ev struct {
			Action string
			Output string
		}
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			b.Write(sc.Bytes())
			b.WriteByte('\n')
			continue
		}
		if ev.Action == "output" {
			b.WriteString(ev.Output)
		}
	}
	return b.String()
}

// boundTail keeps the last n bytes of s, cut at a line start, and says when it cut.
func boundTail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := s[len(s)-n:]
	if i := strings.IndexByte(cut, '\n'); i >= 0 {
		cut = cut[i+1:]
	}
	return "[earlier output cut]\n" + cut
}

// runTool runs the overlay tests of a cargo or vitest case at the job's commit, in a fresh plain tree: write the
// overlays, declare a Rust test module in the crate root, install, and run only the reviewer's tests.
func (s *localSandbox) runTool(ctx context.Context, rp *repo, c bench.Case, j ports.SandboxJob, overlays []ports.File,
	label string) (ports.SandboxResult, error) {
	tree, err := rp.plainTree(ctx, string(j.SHA), label, c.Sparse)
	if err != nil {
		return ports.SandboxResult{}, err
	}
	defer func() { _ = rp.remove(context.WithoutCancel(ctx), tree) }() // a leftover tree is pruned by the next run
	var files, modules []string
	for _, f := range overlays {
		if err := writeOverlay(tree, f); err != nil {
			return ports.SandboxResult{}, err
		}
		files = append(files, f.Path)
		if c.Runner() == bench.ToolchainCargo {
			module := strings.TrimSuffix(filepath.Base(f.Path), ".rs")
			if err := wireCrateRoot(tree, c, module); err != nil {
				return ports.SandboxResult{}, err
			}
			modules = append(modules, module+"::")
		}
	}
	if _, err := rp.install(ctx, c, tree); err != nil {
		return ports.SandboxResult{}, err
	}
	c.Timeout = j.Timeout
	tr, err := rp.runOverlayTests(ctx, c, tree, label, files, modules)
	if err != nil {
		return ports.SandboxResult{}, err
	}
	res := sandboxResult(stripTestFiles(c.Runner(), tr.run))
	res.Output = boundTail(tr.output+tr.run.BuildOutput, maxTestOutput)
	sum := sha256.Sum256([]byte(tr.output))
	res.LogRef = workflow.BlobRef(fmt.Sprintf("blob:sha256:%x", sum))
	return res, nil
}

// wireCrateRoot declares a reviewer module in the crate root of the workdir: src/lib.rs when the crate has a library,
// else src/main.rs.
func wireCrateRoot(tree string, c bench.Case, module string) error {
	src := filepath.Join(tree, filepath.FromSlash(c.Workdir), "src")
	for _, name := range []string{"lib.rs", "main.rs"} {
		root := filepath.Join(src, name)
		b, err := os.ReadFile(root) // #nosec G304 -- the crate root inside the bench's own tree
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read crate root: %w", err)
		}
		// #nosec G703 -- root is the crate root inside the bench's own tree; module passed rustModule (reviewTestPath)
		if err := os.WriteFile(root, wireRustTestModule(b, module), 0o600); err != nil {
			return fmt.Errorf("declare the test module: %w", err)
		}
		return nil
	}
	return fmt.Errorf("no src/lib.rs or src/main.rs in %s", c.Workdir)
}
