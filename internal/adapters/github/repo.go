package github

import (
	"context"
	"fmt"
	"net/http"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// DefaultHead returns the head commit of the repository's base branch: the branch named in Config.BaseBranches, or the
// repository's default branch when none is configured. It reads the commit that branch points at. An empty repository has no commit, so it is ports.ErrNotFound.
func (a *Adapter) DefaultHead(ctx context.Context, repo change.Repo) (id.SHA, error) {
	base, err := repoPath(repo)
	if err != nil {
		return "", err
	}
	token, err := a.tokens.Reader(ctx)
	if err != nil {
		return "", err
	}
	branch, err := a.baseBranch(ctx, token, repo, base)
	if err != nil {
		return "", err
	}
	commitPath := base + "/commits/" + escapePath(branch)
	resp, err := a.c.call(ctx, request{
		method:          http.MethodGet,
		path:            commitPath,
		auth:            token,
		conflictMissing: true,
	})
	if err != nil {
		return "", err
	}
	c, err := decode[shaJSON](resp, commitPath)
	if err != nil {
		return "", err
	}
	sha, err := id.ParseSHA(c.SHA)
	if err != nil {
		return "", fmt.Errorf("github: default head of %s: %w", repo, err)
	}
	return sha, nil
}

// baseBranch is the configured base branch of repo, or the default branch the repository reports.
func (a *Adapter) baseBranch(ctx context.Context, token string, repo change.Repo, base string) (string, error) {
	if branch, ok := a.baseBranches[repo]; ok {
		return branch, nil
	}
	resp, err := a.get(ctx, token, base, nil)
	if err != nil {
		return "", err
	}
	r, err := decode[repoJSON](resp, base)
	if err != nil {
		return "", err
	}
	if r.DefaultBranch == "" {
		return "", fmt.Errorf("%w: %s has no default branch", ports.ErrNotFound, repo)
	}
	if _, err := cleanPath(r.DefaultBranch); err != nil {
		return "", err
	}
	return r.DefaultBranch, nil
}
