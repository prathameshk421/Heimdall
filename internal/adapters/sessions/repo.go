package sessions

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const maxGitPointerBytes = 4096

// Matcher tells whether a working directory from a session record belongs to one repository. A directory belongs
// when it lies under the repository root, or when its git common dir is the one of the root. The second rule
// finds linked worktrees that live outside the root, for example under ../repo-wt/feature.
type Matcher struct {
	root   string
	common string
	mu     sync.Mutex
	seen   map[string]bool
}

// NewMatcher builds a matcher for the repository at root.
func NewMatcher(root string) *Matcher {
	root = filepath.Clean(root)
	return &Matcher{root: root, common: CommonGitDir(root), seen: map[string]bool{}}
}

// Match reports whether dir is inside the repository or inside one of its worktrees. Answers are cached.
func (m *Matcher) Match(dir string) bool {
	if dir == "" || !filepath.IsAbs(dir) {
		return false
	}
	dir = filepath.Clean(dir)
	if UnderRoot(dir, m.root) {
		return true
	}
	if m.common == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if v, ok := m.seen[dir]; ok {
		return v
	}
	v := CommonGitDir(dir) == m.common
	m.seen[dir] = v
	return v
}

// UnderRoot reports whether dir is root or a path below it.
func UnderRoot(dir, root string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(dir))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// CommonGitDir returns the common git dir of the repository that holds dir, with symlinks resolved. It returns ""
// when dir does not exist, is in no repository, or points at a git dir that does not exist. A deleted folder is
// not guessed: only the path rule of Match can accept it.
func CommonGitDir(dir string) string {
	if _, err := os.Stat(dir); err != nil {
		return ""
	}
	for cur := filepath.Clean(dir); ; {
		if gitdir, ok := gitDirAt(cur); ok {
			return commonOf(gitdir)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return ""
		}
		cur = parent
	}
}

// gitDirAt returns the git dir named by <dir>/.git: the folder itself, or the target of a "gitdir:" pointer file.
func gitDirAt(dir string) (string, bool) {
	dotGit := filepath.Join(dir, ".git")
	info, err := os.Lstat(dotGit)
	if err != nil {
		return "", false
	}
	if info.IsDir() {
		return dotGit, true
	}
	if !info.Mode().IsRegular() || info.Size() > maxGitPointerBytes {
		return "", false
	}
	data, err := readSmall(dir, ".git")
	if err != nil {
		return "", false
	}
	target, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
	if !ok {
		return "", false
	}
	target = strings.TrimSpace(target)
	if !filepath.IsAbs(target) {
		target = filepath.Join(dir, target)
	}
	return target, true
}

// commonOf follows the commondir file of a linked worktree's git dir. A git dir without that file is its own
// common dir.
func commonOf(gitdir string) string {
	if _, err := os.Stat(gitdir); err != nil {
		return ""
	}
	common := gitdir
	if data, err := readSmall(gitdir, "commondir"); err == nil {
		rel := strings.TrimSpace(string(data))
		if rel != "" {
			common = rel
			if !filepath.IsAbs(common) {
				common = filepath.Join(gitdir, rel)
			}
		}
	}
	resolved, err := filepath.EvalSymlinks(common)
	if err != nil {
		return ""
	}
	return filepath.Clean(resolved)
}

// RepoRelative turns a tool path into a path relative to the working directory of the record, or to the repository
// root. A path outside both is dropped, so output never names files of other projects.
func RepoRelative(p, cwd, root string) string {
	if p == "" {
		return ""
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(cwd, p)
	}
	for _, base := range []string{cwd, root} {
		if base != "" && UnderRoot(p, base) {
			if rel, err := filepath.Rel(base, p); err == nil && rel != "." {
				return filepath.ToSlash(rel)
			}
		}
	}
	return ""
}

// readSmall reads a pointer file of at most maxGitPointerBytes. Root keeps a symlink from leading out of dir.
func readSmall(dir, name string) ([]byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(io.LimitReader(f, maxGitPointerBytes))
}
