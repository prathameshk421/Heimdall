package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// maxBlobBytes is our cap on one blob. GitHub accepts up to 100 MB (ACTION_MODE 2.6, not verified by S1). The design
// writes files of at most 4 MiB, and a blob is sent base64 encoded inside one request body held in memory.
const maxBlobBytes = 16 << 20

// refTail is the part of a full ref after refs/, which is how the Git Data API names a ref in a path.
func refTail(ref string) string { return strings.TrimPrefix(ref, "refs/") }

// GetRef returns the commit a full ref points at. A missing ref is ports.ErrNotFound. The reply is read with an ETag,
// so a repeat read of an unchanged ref costs no rate limit.
func (x *ActionAPI) GetRef(ctx context.Context, repo change.Repo, ref string) (id.SHA, error) {
	base, err := repoPath(repo)
	if err != nil {
		return "", err
	}
	if err := checkRef(ref); err != nil {
		return "", err
	}
	token, err := x.tokens.Reader(ctx)
	if err != nil {
		return "", err
	}
	path := base + "/git/ref/" + escapePath(refTail(ref))
	resp, err := x.c.call(ctx, request{method: http.MethodGet, path: path, auth: token})
	if err != nil {
		return "", err
	}
	r, err := decode[refJSON](resp, path)
	if err != nil {
		return "", err
	}
	sha, err := id.ParseSHA(r.Object.SHA)
	if err != nil {
		return "", fmt.Errorf("github: ref %s: %w", ref, err)
	}
	return sha, nil
}

// CreateRef creates ref at sha. GitHub refuses a ref that exists with a 422, which this adapter does not type: the
// caller reads the ref with GetRef to tell "exists" from another refusal.
func (x *ActionAPI) CreateRef(ctx context.Context, repo change.Repo, ref string, sha id.SHA) error {
	base, err := repoPath(repo)
	if err != nil {
		return err
	}
	if err := checkRef(ref); err != nil {
		return err
	}
	if err := checkSHA(sha); err != nil {
		return err
	}
	token, err := x.tokens.Writer(ctx, OpBranchWrite, repo)
	if err != nil {
		return err
	}
	return x.sendJSON(ctx, token, http.MethodPost, base+"/git/refs", map[string]string{"ref": ref, "sha": string(sha)}, nil)
}

// UpdateRef moves ref to sha and sends force as given.
//
// This is not a compare-and-swap. On a branch, GitHub refuses a non-fast-forward move when force is false. On a
// non-branch ref such as refs/heimdall/*, a live spike (S1, to be confirmed) found that GitHub did NOT refuse a
// non-fast-forward update with force false. A nil error therefore does not prove that nobody moved the ref meanwhile.
// The conflict mechanism belongs to the caller (the git-ref event store, AM8), which must check the head it read.
func (x *ActionAPI) UpdateRef(ctx context.Context, repo change.Repo, ref string, sha id.SHA, force bool) error {
	base, err := repoPath(repo)
	if err != nil {
		return err
	}
	if err := checkRef(ref); err != nil {
		return err
	}
	if err := checkSHA(sha); err != nil {
		return err
	}
	token, err := x.tokens.Writer(ctx, OpBranchWrite, repo)
	if err != nil {
		return err
	}
	return x.sendJSON(ctx, token, http.MethodPatch, base+"/git/refs/"+escapePath(refTail(ref)), map[string]any{"sha": string(sha), "force": force}, nil)
}

// created reads the hash a Git Data create call answers with.
func created(out shaJSON, what string) (id.SHA, error) {
	sha, err := id.ParseSHA(out.SHA)
	if err != nil {
		return "", fmt.Errorf("github: created %s: %w", what, err)
	}
	return sha, nil
}

// CreateBlob stores content and returns the blob hash. Content up to 16 MiB is accepted.
func (x *ActionAPI) CreateBlob(ctx context.Context, repo change.Repo, content []byte) (id.SHA, error) {
	base, err := repoPath(repo)
	if err != nil {
		return "", err
	}
	if len(content) > maxBlobBytes {
		return "", fmt.Errorf("github: blob is larger than %d bytes", maxBlobBytes)
	}
	token, err := x.tokens.Writer(ctx, OpBranchWrite, repo)
	if err != nil {
		return "", err
	}
	var out shaJSON
	in := map[string]string{"content": encodeBase64(content), "encoding": "base64"}
	if err := x.sendJSON(ctx, token, http.MethodPost, base+"/git/blobs", in, &out); err != nil {
		return "", err
	}
	return created(out, "blob")
}

// CreateTree writes a tree of entries on top of base and returns its hash. An empty base writes a tree of only the
// entries. An entry with mode 040000 names a subtree hash; any other mode names a blob.
func (x *ActionAPI) CreateTree(ctx context.Context, repo change.Repo, base id.SHA, entries []ports.GitTreeEntry) (id.SHA, error) {
	root, err := repoPath(repo)
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "", errors.New("github: tree has no entries")
	}
	if base != "" {
		if err := checkSHA(base); err != nil {
			return "", err
		}
	}
	items := make([]treeEntry, 0, len(entries))
	for i, e := range entries {
		if err := e.Validate(); err != nil {
			return "", fmt.Errorf("github: tree entry %d: %w", i, err)
		}
		if _, err := cleanPath(e.Path); err != nil {
			return "", err
		}
		kind := "blob"
		if e.Mode == "040000" {
			kind = "tree"
		}
		items = append(items, treeEntry{Path: e.Path, Mode: e.Mode, Type: kind, SHA: string(e.SHA)})
	}
	token, err := x.tokens.Writer(ctx, OpBranchWrite, repo)
	if err != nil {
		return "", err
	}
	in := map[string]any{"tree": items}
	if base != "" {
		in["base_tree"] = string(base)
	}
	var out shaJSON
	if err := x.sendJSON(ctx, token, http.MethodPost, root+"/git/trees", in, &out); err != nil {
		return "", err
	}
	return created(out, "tree")
}

// CreateCommit writes a commit and returns its hash. It moves no ref. No parent makes a root commit.
func (x *ActionAPI) CreateCommit(ctx context.Context, repo change.Repo, message string, tree id.SHA, parents []id.SHA) (id.SHA, error) {
	base, err := repoPath(repo)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(message) == "" {
		return "", errors.New("github: commit message is empty")
	}
	if err := checkSHA(tree); err != nil {
		return "", err
	}
	ps := make([]string, 0, len(parents))
	for _, p := range parents {
		if err := checkSHA(p); err != nil {
			return "", err
		}
		ps = append(ps, string(p))
	}
	token, err := x.tokens.Writer(ctx, OpBranchWrite, repo)
	if err != nil {
		return "", err
	}
	var out shaJSON
	in := map[string]any{"message": message, "tree": string(tree), "parents": ps}
	if err := x.sendJSON(ctx, token, http.MethodPost, base+"/git/commits", in, &out); err != nil {
		return "", err
	}
	return created(out, "commit")
}
