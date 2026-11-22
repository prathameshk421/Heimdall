package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/bench"
)

// runner runs one external command: git, go, a clean case's test command, or env with a cargo or vitest case's
// install and test commands.
type runner interface {
	run(ctx context.Context, dir, name string, args ...string) (stdout, stderr []byte, err error)
}

type execRunner struct{}

func (execRunner) run(ctx context.Context, dir, name string, args ...string) (stdout, stderr []byte, err error) {
	// #nosec G204,G702 -- name is git, go or the test command of a case in the repo's own manifest; no shell is involved
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	// go test starts a test binary; when the context ends, do not wait forever for it to close the pipes.
	cmd.WaitDelay = 10 * time.Second
	err = cmd.Run()
	return out.Bytes(), errOut.Bytes(), err
}

// repo is a bare, shallow, blob-less cache of one GitHub repository. Blobs come on demand from the remote.
//
// The cases of one repository in a parallel run share one repo (repoSet). mu serialises the writes to the cache: a
// fetch, which locks the shallow file, and a worktree add, remove or prune, which edit the worktree list.
type repo struct {
	r      runner
	gitDir string
	work   string // the parent directory of this repo's worktrees
	cache  string // the bench cache root: the cargo and pnpm caches live under it
	mu     sync.Mutex
}

// repoSet opens each repository cache once per review run, so the workers of a parallel run share one repo, and with
// it the lock on the cache's writes.
type repoSet struct {
	mu    sync.Mutex
	repos map[string]*repo
}

func newRepoSet() *repoSet { return &repoSet{repos: map[string]*repo{}} }

// open returns the cache of the case's repository under root, opened on first use.
func (s *repoSet) open(ctx context.Context, r runner, root string, c bench.Case) (*repo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := filepath.Join(root, c.RepoSlug())
	if rp, ok := s.repos[key]; ok {
		return rp, nil
	}
	rp, err := openRepo(ctx, r, root, c)
	if err != nil {
		return nil, err
	}
	s.repos[key] = rp
	return rp, nil
}

// openRepo opens the cache of the case's repository under root, and creates it on first use.
func openRepo(ctx context.Context, r runner, root string, c bench.Case) (*repo, error) {
	name := strings.ReplaceAll(c.RepoSlug(), "/", "__")
	rp := &repo{r: r, gitDir: filepath.Join(root, "repos", name+".git"), work: filepath.Join(root, "work", name), cache: root}
	// #nosec G703 -- root is the operator's -cache flag and name comes from the manifest's repo slug, never a request
	if err := os.MkdirAll(rp.work, 0o750); err != nil {
		return nil, fmt.Errorf("make work dir: %w", err)
	}
	// #nosec G703 -- see above: the cache directory is the operator's own
	if _, err := os.Stat(filepath.Join(rp.gitDir, "HEAD")); err == nil {
		return rp, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("stat cache: %w", err)
	}
	steps := [][]string{
		{"init", "--quiet", "--bare", rp.gitDir},
		{"--git-dir", rp.gitDir, "remote", "add", "origin", c.Remote() + ".git"},
		{"--git-dir", rp.gitDir, "config", "remote.origin.promisor", "true"},
		{"--git-dir", rp.gitDir, "config", "remote.origin.partialclonefilter", "blob:none"},
		{"--git-dir", rp.gitDir, "config", "extensions.partialClone", "origin"},
	}
	for _, args := range steps {
		if _, err := rp.git(ctx, "", args...); err != nil {
			return nil, err
		}
	}
	return rp, nil
}

func (rp *repo) git(ctx context.Context, dir string, args ...string) ([]byte, error) {
	out, errOut, err := rp.r.run(ctx, dir, "git", args...)
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, tail(errOut, 5))
	}
	return out, nil
}

// fetch gets the commits and their trees, one level deep, unless the cache has them.
func (rp *repo) fetch(ctx context.Context, shas ...string) error {
	var missing []string
	for _, sha := range shas {
		if _, err := rp.git(ctx, "", "--git-dir", rp.gitDir, "cat-file", "-e", sha+"^{commit}"); err != nil {
			missing = append(missing, sha)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	rp.mu.Lock()
	defer rp.mu.Unlock()
	args := append([]string{"--git-dir", rp.gitDir, "fetch", "--quiet", "--filter=blob:none", "--depth=1", "origin"}, missing...)
	_, err := rp.git(ctx, "", args...)
	return err
}

// checkParent returns an error when parent is not the first parent of sha.
func (rp *repo) checkParent(ctx context.Context, sha, parent string) error {
	raw, err := rp.git(ctx, "", "--git-dir", rp.gitDir, "cat-file", "-p", sha)
	if err != nil {
		return err
	}
	parents := bench.CommitParents(raw)
	if len(parents) == 0 || parents[0] != parent {
		return fmt.Errorf("%s is not the first parent of %s (parents: %v)", parent, sha, parents)
	}
	return nil
}

func (rp *repo) diff(ctx context.Context, from, to string) ([]bench.FileDiff, error) {
	out, err := rp.git(ctx, "", "--git-dir", rp.gitDir, "diff", "-U0", "--no-color", "--no-renames", "--no-ext-diff", from, to)
	if err != nil {
		return nil, err
	}
	return bench.ParseDiff(out)
}

// hasFile reports whether the tree of commit sha holds the file at path. A failure to ask is an error, not a missing
// file.
func (rp *repo) hasFile(ctx context.Context, sha, path string) (bool, error) {
	out, err := rp.git(ctx, "", "--git-dir", rp.gitDir, "ls-tree", "--name-only", sha, "--", path)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) == path, nil
}

func (rp *repo) show(ctx context.Context, sha, path string) ([]byte, error) {
	return rp.git(ctx, "", "--git-dir", rp.gitDir, "show", sha+":"+path)
}

// worktree checks sha out into a fresh directory named label and returns it. The caller removes it.
func (rp *repo) worktree(ctx context.Context, sha, label string) (string, error) {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	dir := filepath.Join(rp.work, label)
	if err := rp.removeLocked(ctx, dir); err != nil {
		return "", err
	}
	_, err := rp.git(ctx, "", "--git-dir", rp.gitDir, "worktree", "add", "--quiet", "--force", "--detach", dir, sha)
	return dir, err
}

// remove deletes a worktree, also one a killed run left behind. It removes through os.Root, so only a path inside the
// repository's own worktree parent can be touched, whatever label a caller passes.
func (rp *repo) remove(ctx context.Context, dir string) error {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	return rp.removeLocked(ctx, dir)
}

// removeLocked is remove for a caller that holds mu.
func (rp *repo) removeLocked(ctx context.Context, dir string) error {
	rel, err := filepath.Rel(rp.work, dir)
	if err != nil {
		return fmt.Errorf("worktree path %q: %w", dir, err)
	}
	root, err := os.OpenRoot(rp.work)
	if err != nil {
		return fmt.Errorf("open the worktree parent: %w", err)
	}
	defer func() { _ = root.Close() }()
	if err := root.RemoveAll(rel); err != nil {
		return fmt.Errorf("remove worktree: %w", err)
	}
	_, err = rp.git(ctx, "", "--git-dir", rp.gitDir, "worktree", "prune")
	return err
}

// tail returns the last n non-empty lines of b, joined by " | ".
func tail(b []byte, n int) string {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}
