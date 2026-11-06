package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// checkBranch refuses a branch name that git would refuse or that could change the URL it is put in.
func checkBranch(name string) error {
	bad := name == "" || len(name) > 255 || strings.HasPrefix(name, "-") || strings.HasPrefix(name, "/") ||
		strings.HasSuffix(name, "/") || strings.HasSuffix(name, ".lock") || strings.Contains(name, "..") ||
		strings.Contains(name, "//") || strings.ContainsAny(name, "~^:?*[\\") ||
		strings.IndexFunc(name, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0
	if bad {
		return fmt.Errorf("github: branch name %q is not allowed", name)
	}
	return nil
}

// branchHead returns the commit a branch points at.
func (a *Adapter) branchHead(ctx context.Context, token, base, branch string) (string, error) {
	path := base + "/git/ref/heads/" + escapePath(branch)
	resp, err := a.get(ctx, token, path, nil)
	if err != nil {
		return "", err
	}
	ref, err := decode[refJSON](resp, path)
	if err != nil {
		return "", err
	}
	return ref.Object.SHA, nil
}

// EnsureBranch creates name at from when it does not exist, and does nothing when it does.
func (a *Adapter) EnsureBranch(ctx context.Context, repo change.Repo, from id.SHA, name string, key string) error {
	base, err := repoPath(repo)
	if err != nil {
		return err
	}
	if err := checkSHA(from); err != nil {
		return err
	}
	if err := checkBranch(name); err != nil {
		return err
	}
	if strings.TrimSpace(key) == "" {
		return errors.New("github: ensure branch needs an idempotency key")
	}
	token, err := a.tokens.Writer(ctx, OpBranchWrite, repo)
	if err != nil {
		return err
	}
	if _, err = a.branchHead(ctx, token, base, name); !errors.Is(err, ports.ErrNotFound) {
		return err // nil when the branch exists, the failure otherwise
	}
	return a.send(ctx, token, http.MethodPost, base+"/git/refs", map[string]string{"ref": "refs/heads/" + name, "sha": string(from)}, nil)
}

// validateCommit checks everything CommitFiles is given before any request is sent.
func validateCommit(repo change.Repo, branch, msg string, files []ports.File) (string, error) {
	base, err := repoPath(repo)
	if err != nil {
		return "", err
	}
	if err := checkBranch(branch); err != nil {
		return "", err
	}
	if strings.TrimSpace(msg) == "" {
		return "", errors.New("github: commit message is empty")
	}
	if len(files) == 0 {
		return "", errors.New("github: commit has no files")
	}
	for _, f := range files {
		if err := f.Validate(); err != nil {
			return "", fmt.Errorf("github: commit file: %w", err)
		}
		if _, err := cleanPath(f.Path); err != nil {
			return "", err
		}
	}
	return base, nil
}

// CommitFiles writes files to branch in one commit and returns its SHA. It builds the commit from blobs, one tree on
// top of the branch's tree, and one commit, then moves the branch. A branch that moved meanwhile makes the final move
// fail, because the update is never forced.
func (a *Adapter) CommitFiles(ctx context.Context, repo change.Repo, branch, msg string, files []ports.File) (id.SHA, error) {
	base, err := validateCommit(repo, branch, msg, files)
	if err != nil {
		return "", err
	}
	token, err := a.tokens.Writer(ctx, OpBranchWrite, repo)
	if err != nil {
		return "", err
	}
	head, err := a.branchHead(ctx, token, base, branch)
	if err != nil {
		return "", err
	}
	commitPath := fmt.Sprintf("%s/git/commits/%s", base, head)
	resp, err := a.get(ctx, token, commitPath, nil)
	if err != nil {
		return "", err
	}
	parent, err := decode[commitJSON](resp, commitPath)
	if err != nil {
		return "", err
	}
	entries, err := a.writeBlobs(ctx, token, base, files)
	if err != nil {
		return "", err
	}
	var tree, commit shaJSON
	if err := a.send(ctx, token, http.MethodPost, base+"/git/trees", map[string]any{"base_tree": parent.Tree.SHA, "tree": entries}, &tree); err != nil {
		return "", err
	}
	body := map[string]any{"message": msg, "tree": tree.SHA, "parents": []string{head}}
	if err := a.send(ctx, token, http.MethodPost, base+"/git/commits", body, &commit); err != nil {
		return "", err
	}
	if err := a.send(ctx, token, http.MethodPatch, base+"/git/refs/heads/"+escapePath(branch), map[string]string{"sha": commit.SHA}, nil); err != nil {
		return "", err
	}
	return id.ParseSHA(commit.SHA)
}

// writeBlobs uploads each file as a blob and returns the tree entries that point at them.
func (a *Adapter) writeBlobs(ctx context.Context, token, base string, files []ports.File) ([]treeEntry, error) {
	entries := make([]treeEntry, 0, len(files))
	for _, f := range files {
		var blob shaJSON
		in := map[string]string{"content": encodeBase64(f.Content), "encoding": "base64"}
		if err := a.send(ctx, token, http.MethodPost, base+"/git/blobs", in, &blob); err != nil {
			return nil, err
		}
		entries = append(entries, treeEntry{Path: f.Path, Mode: "100644", Type: "blob", SHA: blob.SHA})
	}
	return entries, nil
}
