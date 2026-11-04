// Package fixtures builds the hermetic seed repositories under testdata/repos as real git repositories.
package fixtures

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	baseDir     = "base"
	branchesDir = "branches"
	branchFile  = "branch.yaml"
	prBodyFile  = "PR_BODY.md"
	reproRelDir = "tests/heimdall/repro"
	gitName     = "Heimdall Fixtures"
	gitMail     = "fixtures@heimdall.invalid"
	gitDate     = "2026-11-04T09:00:00Z"
	mainBranch  = "main"
	grepNoMatch = 1
	dirPerm     = 0o755
	filePerm    = 0o644
	baseMessage = "Add the payments service"
	branchSep   = "/"
)

// branchSpec is the content of an overlay's branch.yaml.
type branchSpec struct {
	Parent  string   `yaml:"parent"`
	Message string   `yaml:"commit_message"`
	Delete  []string `yaml:"delete"`
}

// overlay is one branch directory with its parsed spec. branch is the full git branch name, <repo>/<dir>.
type overlay struct {
	branch string
	dir    string
	spec   branchSpec
}

// ModuleRoot is the repository root that manifest and story paths are relative to.
func ModuleRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

// Root is the testdata/repos directory.
func Root() string {
	return filepath.Join(ModuleRoot(), "testdata", "repos")
}

// ManifestPath is the location of pay-demo's planted-findings manifest.
func ManifestPath() string {
	return filepath.Join(Root(), "pay-demo", "manifest.yaml")
}

// Materialize builds repo (pay-demo or ledger-demo) as a git repository in a temp dir: the base as main, then every
// overlay branch in parent order. It returns the path, with main checked out.
func Materialize(t testing.TB, repo string) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", mainBranch)
	copyTree(t, filepath.Join(Root(), repo, baseDir), dir, nil)
	commitAll(t, dir, baseMessage)
	for _, o := range loadOverlays(t, repo) {
		createBranch(t, dir, o)
	}
	Checkout(t, dir, mainBranch)
	return dir
}

// Branches lists main and every overlay branch of repo, the overlays in name order.
func Branches(t testing.TB, repo string) []string {
	t.Helper()
	out := []string{mainBranch}
	for _, o := range loadOverlays(t, repo) {
		out = append(out, o.branch)
	}
	return out
}

// Checkout switches the materialised repository in dir to branch.
func Checkout(t testing.TB, dir, branch string) {
	t.Helper()
	gitRun(t, dir, "checkout", "-q", branch)
}

// Git runs git in dir, fails the test on error and returns stdout.
func Git(t testing.TB, dir string, args ...string) string {
	t.Helper()
	out, err := gitOutput(t, dir, args...)
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

// CountInBranch counts the occurrences of s in the committed tree of branch. Zero when there is none.
func CountInBranch(t testing.TB, dir, branch, s string) int {
	t.Helper()
	out, err := gitCmd(t, dir, "grep", "-o", "-F", "-e", s, branch, "--").Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == grepNoMatch {
		return 0
	}
	if err != nil {
		t.Fatalf("git grep %s: %v", branch, err)
	}
	return len(strings.Split(strings.TrimSpace(string(out)), "\n"))
}

// GoRun runs the go tool in dir offline and returns its combined output and error.
func GoRun(t testing.TB, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := runTool(t.Context(), dir, "go", []string{"GOTOOLCHAIN=local", "GOPROXY=off", "GOFLAGS=", "GOWORK=off"}, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// Gofmt returns the output of gofmt -l in dir: empty when every Go file is formatted.
func Gofmt(t testing.TB, dir string) string {
	t.Helper()
	out, err := runTool(t.Context(), dir, "gofmt", nil, "-l", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("gofmt in %s: %v\n%s", dir, err, out)
	}
	return string(out)
}

// ApplyRepro copies a finding's repro test into tests/heimdall/repro of dir, where go test runs it.
func ApplyRepro(t testing.TB, dir string, f Finding) {
	t.Helper()
	data := readFile(t, filepath.Join(ModuleRoot(), f.Repro))
	target := openRoot(t, dir)
	must(t, target.MkdirAll(reproRelDir, dirPerm))
	must(t, target.WriteFile(filepath.Join(reproRelDir, filepath.Base(f.Repro)), data, filePerm))
}

// PRBody returns the pull request body kept beside a branch overlay. It is never committed.
func PRBody(t testing.TB, repo, branch string) string {
	t.Helper()
	name := strings.TrimPrefix(branch, repo+branchSep)
	return string(readFile(t, filepath.Join(Root(), repo, branchesDir, name, prBodyFile)))
}

// createBranch makes o's branch from its parent, removes the listed files, applies the overlay and commits it.
func createBranch(t testing.TB, dir string, o overlay) {
	t.Helper()
	gitRun(t, dir, "checkout", "-q", "-b", o.branch, o.spec.Parent)
	work := openRoot(t, dir)
	for _, rel := range o.spec.Delete {
		must(t, work.Remove(rel))
	}
	copyTree(t, o.dir, dir, func(rel string) bool { return rel == branchFile || rel == prBodyFile })
	commitAll(t, dir, o.spec.Message)
}

// loadOverlays reads every branches/<name>/branch.yaml of repo, in parent order. Strict decoding means a misspelled
// key fails the build rather than silently changing a branch.
func loadOverlays(t testing.TB, repo string) []overlay {
	t.Helper()
	repoSrc := filepath.Join(Root(), repo)
	entries, err := os.ReadDir(filepath.Join(repoSrc, branchesDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	must(t, err)
	var out []overlay
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(repoSrc, branchesDir, e.Name())
		out = append(out, overlay{
			branch: repo + branchSep + e.Name(),
			dir:    dir,
			spec:   readSpec(t, filepath.Join(dir, branchFile)),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].branch < out[j].branch })
	return orderByParent(t, out)
}

// orderByParent sorts overlays so every parent is built before its children.
func orderByParent(t testing.TB, in []overlay) []overlay {
	t.Helper()
	built := map[string]bool{mainBranch: true}
	var out []overlay
	pending := in
	for len(pending) > 0 {
		var next []overlay
		for _, o := range pending {
			if !built[o.spec.Parent] {
				next = append(next, o)
				continue
			}
			out = append(out, o)
			built[o.branch] = true
		}
		if len(next) == len(pending) {
			t.Fatalf("branch %s has no buildable parent %q", pending[0].branch, pending[0].spec.Parent)
		}
		pending = next
	}
	return out
}

// readSpec decodes branch.yaml strictly.
func readSpec(t testing.TB, path string) branchSpec {
	t.Helper()
	var spec branchSpec
	dec := yaml.NewDecoder(bytes.NewReader(readFile(t, path)))
	dec.KnownFields(true)
	if err := dec.Decode(&spec); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return spec
}

// copyTree copies every file under src into dst, keeping the layout. Both ends go through an os.Root, so no path
// can leave its directory. skip, when set, names files to leave out.
func copyTree(t testing.TB, src, dst string, skip func(rel string) bool) {
	t.Helper()
	from := openRoot(t, src)
	to := openRoot(t, dst)
	err := fs.WalkDir(from.FS(), ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			return to.MkdirAll(rel, dirPerm)
		}
		if skip != nil && skip(rel) {
			return nil
		}
		data, err := fs.ReadFile(from.FS(), rel)
		if err != nil {
			return err
		}
		return to.WriteFile(rel, data, filePerm)
	})
	must(t, err)
}

// commitAll stages every change in dir and commits it with the fixed fixture identity.
func commitAll(t testing.TB, dir, message string) {
	t.Helper()
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "-m", message)
}

// gitRun runs git and fails the test on error.
func gitRun(t testing.TB, dir string, args ...string) {
	t.Helper()
	if out, err := gitOutput(t, dir, args...); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// gitOutput runs git in dir and returns stdout, or stderr on failure.
func gitOutput(t testing.TB, dir string, args ...string) (string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := gitCmd(t, dir, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stderr.String(), err
	}
	return stdout.String(), nil
}

// gitCmd prepares git with the global and system config off, hooks off and the fixture identity and date.
func gitCmd(t testing.TB, dir string, args ...string) *exec.Cmd {
	t.Helper()
	full := append([]string{"-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null"}, args...)
	return runTool(t.Context(), dir, "git", []string{
		"GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=" + gitName, "GIT_AUTHOR_EMAIL=" + gitMail, "GIT_AUTHOR_DATE=" + gitDate,
		"GIT_COMMITTER_NAME=" + gitName, "GIT_COMMITTER_EMAIL=" + gitMail, "GIT_COMMITTER_DATE=" + gitDate,
	}, full...)
}

// runTool prepares one of the fixed tools (git, go, gofmt) in dir. The environment is an allowlist plus extra, so a
// caller's git configuration or GIT_DIR cannot leak into a fixture.
func runTool(ctx context.Context, dir, name string, extra []string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- name is one of the fixed tools; args are chosen by tests
	cmd.Dir = dir
	cmd.Env = append(toolEnv(), extra...)
	return cmd
}

// toolEnv returns the variables the tools need to find themselves and their caches, and nothing else.
func toolEnv() []string {
	var env []string
	for _, name := range []string{"PATH", "HOME", "GOCACHE", "GOMODCACHE", "GOPATH", "TMPDIR", "XDG_CACHE_HOME"} {
		if v := os.Getenv(name); v != "" {
			env = append(env, name+"="+v)
		}
	}
	return env
}

// openRoot opens dir as an os.Root, so every path handled through it stays inside dir.
func openRoot(t testing.TB, dir string) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(dir)
	must(t, err)
	t.Cleanup(func() { _ = root.Close() })
	return root
}

// readFileErr reads path through an os.Root on its directory, so the read stays inside that directory.
func readFileErr(path string) ([]byte, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	return root.ReadFile(filepath.Base(path))
}

// readFile is readFileErr for tests: it fails the test on error.
func readFile(t testing.TB, path string) []byte {
	t.Helper()
	data, err := readFileErr(path)
	must(t, err)
	return data
}

// must fails the test on a non-nil error.
func must(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
