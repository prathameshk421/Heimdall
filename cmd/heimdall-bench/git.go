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
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/bench"
)

// runner runs one external command. Only git and go run through it, plus a clean case's test command.
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
type repo struct {
	r      runner
	gitDir string
	work   string // the parent directory of this repo's worktrees
}

// openRepo opens the cache of the case's repository under root, and creates it on first use.
func openRepo(ctx context.Context, r runner, root string, c bench.Case) (*repo, error) {
	name := strings.ReplaceAll(c.RepoSlug(), "/", "__")
	rp := &repo{r: r, gitDir: filepath.Join(root, "repos", name+".git"), work: filepath.Join(root, "work", name)}
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
		{"--git-dir", rp.gitDir, "remote", "add", "origin", c.Repo + ".git"},
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

func (rp *repo) show(ctx context.Context, sha, path string) ([]byte, error) {
	return rp.git(ctx, "", "--git-dir", rp.gitDir, "show", sha+":"+path)
}

// worktree checks sha out into a fresh directory named label and returns it. The caller removes it.
func (rp *repo) worktree(ctx context.Context, sha, label string) (string, error) {
	dir := filepath.Join(rp.work, label)
	if err := rp.remove(ctx, dir); err != nil {
		return "", err
	}
	_, err := rp.git(ctx, "", "--git-dir", rp.gitDir, "worktree", "add", "--quiet", "--force", "--detach", dir, sha)
	return dir, err
}

// remove deletes a worktree, also one a killed run left behind. It removes through os.Root, so only a path inside the
// repository's own worktree parent can be touched, whatever label a caller passes.
func (rp *repo) remove(ctx context.Context, dir string) error {
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

// copyFrom puts the files of commit sha at paths into the worktree dir.
func (rp *repo) copyFrom(ctx context.Context, dir, sha string, paths []string) error {
	args := append([]string{"checkout", "--quiet", sha, "--"}, paths...)
	_, err := rp.git(ctx, dir, args...)
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
