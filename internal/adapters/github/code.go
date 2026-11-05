package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// cleanPath splits a repository-relative path into segments and refuses anything that could leave the repository or
// change the URL it is put in. An empty path has no segments, which is the repository root.
func cleanPath(p string) ([]string, error) {
	if p == "" {
		return nil, nil
	}
	if strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\\x00\r\n") {
		return nil, fmt.Errorf("github: path %q is not a clean relative path", p)
	}
	segs := strings.Split(p, "/")
	for _, s := range segs {
		if s == "" || s == "." || s == ".." {
			return nil, fmt.Errorf("github: path %q is not a clean relative path", p)
		}
	}
	return segs, nil
}

// ReadFile returns one file at sha. A missing path is ports.ErrNotFound.
func (a *Adapter) ReadFile(ctx context.Context, repo change.Repo, sha id.SHA, path string) ([]byte, error) {
	base, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	if err := checkSHA(sha); err != nil {
		return nil, err
	}
	segs, err := cleanPath(path)
	if err != nil {
		return nil, err
	}
	if len(segs) == 0 {
		return nil, fmt.Errorf("github: read file: path is empty")
	}
	token, err := a.tokens.Reader(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := a.c.call(ctx, request{
		method: http.MethodGet,
		path:   base + "/contents/" + escapePath(path),
		query:  url.Values{"ref": {string(sha)}},
		auth:   token,
		accept: acceptRaw,
	})
	if err != nil {
		return nil, err
	}
	return resp.body, nil
}

// TreeHash returns the git tree hash of dir at sha, walking down from the commit's root tree. An empty dir is the
// root. A directory that does not exist, or a path that names a file, is ports.ErrNotFound.
func (a *Adapter) TreeHash(ctx context.Context, repo change.Repo, sha id.SHA, dir string) (id.SHA, error) {
	base, err := repoPath(repo)
	if err != nil {
		return "", err
	}
	if err := checkSHA(sha); err != nil {
		return "", err
	}
	segs, err := cleanPath(dir)
	if err != nil {
		return "", err
	}
	token, err := a.tokens.Reader(ctx)
	if err != nil {
		return "", err
	}
	commitPath := fmt.Sprintf("%s/git/commits/%s", base, sha)
	resp, err := a.get(ctx, token, commitPath, nil)
	if err != nil {
		return "", err
	}
	commit, err := decode[commitJSON](resp, commitPath)
	if err != nil {
		return "", err
	}
	current := commit.Tree.SHA
	for _, seg := range segs {
		next, err := a.childTree(ctx, token, base, current, seg)
		if err != nil {
			return "", fmt.Errorf("github: tree of %q at %s: %w", dir, sha, err)
		}
		current = next
	}
	return id.ParseSHA(current)
}

// childTree finds the subdirectory name inside the tree and returns its hash.
func (a *Adapter) childTree(ctx context.Context, token, base, treeSHA, name string) (string, error) {
	path := fmt.Sprintf("%s/git/trees/%s", base, treeSHA)
	resp, err := a.get(ctx, token, path, nil)
	if err != nil {
		return "", err
	}
	tree, err := decode[treeJSON](resp, path)
	if err != nil {
		return "", err
	}
	for _, e := range tree.Tree {
		if e.Path == name && e.Type == "tree" {
			return e.SHA, nil
		}
	}
	return "", fmt.Errorf("%w: no directory %q", ports.ErrNotFound, name)
}

// Tarball returns the source tree at sha as GitHub's tarball. The caller closes it.
func (a *Adapter) Tarball(ctx context.Context, repo change.Repo, sha id.SHA) (io.ReadCloser, error) {
	base, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	if err := checkSHA(sha); err != nil {
		return nil, err
	}
	token, err := a.tokens.Reader(ctx)
	if err != nil {
		return nil, err
	}
	return a.c.open(ctx, request{method: http.MethodGet, path: fmt.Sprintf("%s/tarball/%s", base, sha), auth: token})
}
