package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// The reads and the compare-and-swap of the git-ref event store (AM8, ACTION_MODE_SPIKE.md 1.7). Reads use REST,
// because GraphQL cannot see refs/heimdall/* (S1 1.2). The ref moves only through GraphQL updateRefs with beforeOid
// and force false, the one call S1 measured as a compare-and-swap on that namespace.

var (
	_ ports.GitReader     = (*ActionAPI)(nil)
	_ ports.GitRefSwapper = (*ActionAPI)(nil)
)

// updateRefsMutation is the call S1 verified (ACTION_MODE_SPIKE.md 1.2).
const updateRefsMutation = `mutation($r:ID!,$n:GitRefname!,$b:GitObjectID,$a:GitObjectID!,$f:Boolean!){` +
	`updateRefs(input:{repositoryId:$r, refUpdates:[{name:$n, beforeOid:$b, afterOid:$a, force:$f}]}){clientMutationId}}`

// reErrorType bounds the GraphQL error type the adapter repeats in an error. The message is never repeated: it can
// echo input, and S1 saw only an opaque text with a request id.
var reErrorType = regexp.MustCompile(`^[A-Z_]{1,40}$`)

// gitCommitJSON is a git commit as the Git Data API returns it.
type gitCommitJSON struct {
	Tree    shaJSON   `json:"tree"`
	Parents []shaJSON `json:"parents"`
}

// gitTreeJSON is a tree listing with its truncation flag.
type gitTreeJSON struct {
	Tree      []treeEntry `json:"tree"`
	Truncated bool        `json:"truncated"`
}

// gitBlobJSON is a blob as the Git Data API returns it.
type gitBlobJSON struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
	Size     int64  `json:"size"`
}

// GetCommit returns the tree and parents of a commit with the reader token. A missing commit is ports.ErrNotFound.
func (x *ActionAPI) GetCommit(ctx context.Context, repo change.Repo, sha id.SHA) (ports.GitCommit, error) {
	base, err := repoPath(repo)
	if err != nil {
		return ports.GitCommit{}, err
	}
	if err := checkSHA(sha); err != nil {
		return ports.GitCommit{}, err
	}
	token, err := x.tokens.Reader(ctx)
	if err != nil {
		return ports.GitCommit{}, err
	}
	path := base + "/git/commits/" + string(sha)
	resp, err := x.c.call(ctx, request{method: http.MethodGet, path: path, auth: token})
	if err != nil {
		return ports.GitCommit{}, err
	}
	c, err := decode[gitCommitJSON](resp, path)
	if err != nil {
		return ports.GitCommit{}, err
	}
	tree, err := id.ParseSHA(c.Tree.SHA)
	if err != nil {
		return ports.GitCommit{}, fmt.Errorf("github: commit %s tree: %w", sha, err)
	}
	out := ports.GitCommit{Tree: tree}
	for _, p := range c.Parents {
		ps, err := id.ParseSHA(p.SHA)
		if err != nil {
			return ports.GitCommit{}, fmt.Errorf("github: commit %s parent: %w", sha, err)
		}
		out.Parents = append(out.Parents, ps)
	}
	return out, nil
}

// GetTree lists a tree recursively in one call with the reader token. A truncated listing, or one over maxEntries, is
// ports.ErrObjectTooLarge. Every entry must have a clean path, a known type and a valid hash. The whole reply is held
// to the client's body cap (8 MiB).
func (x *ActionAPI) GetTree(ctx context.Context, repo change.Repo, sha id.SHA, maxEntries int) ([]ports.GitTreeItem, error) {
	base, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	if err := checkSHA(sha); err != nil {
		return nil, err
	}
	if maxEntries <= 0 {
		return nil, errors.New("github: tree entry limit must be positive")
	}
	token, err := x.tokens.Reader(ctx)
	if err != nil {
		return nil, err
	}
	path := base + "/git/trees/" + string(sha)
	resp, err := x.c.call(ctx, request{method: http.MethodGet, path: path, query: url.Values{"recursive": {"1"}}, auth: token})
	if err != nil {
		return nil, err
	}
	t, err := decode[gitTreeJSON](resp, path)
	if err != nil {
		return nil, err
	}
	if t.Truncated || len(t.Tree) > maxEntries {
		return nil, fmt.Errorf("github: tree %s has %d entries (truncated %t), the limit is %d: %w",
			sha, len(t.Tree), t.Truncated, maxEntries, ports.ErrObjectTooLarge)
	}
	out := make([]ports.GitTreeItem, 0, len(t.Tree))
	for i, e := range t.Tree {
		item, err := treeItem(e)
		if err != nil {
			return nil, fmt.Errorf("github: tree %s entry %d: %w", sha, i, err)
		}
		out = append(out, item)
	}
	return out, nil
}

// treeItem checks one entry of a listing: a clean path, a known type and a valid hash.
func treeItem(e treeEntry) (ports.GitTreeItem, error) {
	if segs, err := cleanPath(e.Path); err != nil || len(segs) == 0 {
		return ports.GitTreeItem{}, errors.New("bad path")
	}
	switch e.Type {
	case "blob", "tree", "commit":
	default:
		return ports.GitTreeItem{}, fmt.Errorf("unknown type %q", e.Type)
	}
	h, err := id.ParseSHA(e.SHA)
	if err != nil {
		return ports.GitTreeItem{}, err
	}
	return ports.GitTreeItem{Path: e.Path, Mode: e.Mode, Type: e.Type, SHA: h}, nil
}

// GetBlob returns a blob's content with the reader token. The stated size is checked against maxBytes before the
// content is decoded, and the decoded length must equal it. A blob reply is base64 and must fit the client's 8 MiB
// body cap, so maxBytes above about 6 MiB cannot be reached.
func (x *ActionAPI) GetBlob(ctx context.Context, repo change.Repo, sha id.SHA, maxBytes int64) ([]byte, error) {
	base, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	if err := checkSHA(sha); err != nil {
		return nil, err
	}
	if maxBytes <= 0 {
		return nil, errors.New("github: blob size limit must be positive")
	}
	token, err := x.tokens.Reader(ctx)
	if err != nil {
		return nil, err
	}
	path := base + "/git/blobs/" + string(sha)
	resp, err := x.c.call(ctx, request{method: http.MethodGet, path: path, auth: token})
	if err != nil {
		return nil, err
	}
	b, err := decode[gitBlobJSON](resp, path)
	if err != nil {
		return nil, err
	}
	if b.Size > maxBytes {
		return nil, fmt.Errorf("github: blob %s is %d bytes, the limit is %d: %w", sha, b.Size, maxBytes, ports.ErrObjectTooLarge)
	}
	if b.Encoding != "base64" {
		return nil, fmt.Errorf("github: blob %s: encoding %q is not base64", sha, b.Encoding)
	}
	data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(b.Content, "\n", ""))
	if err != nil || int64(len(data)) != b.Size {
		return nil, fmt.Errorf("github: blob %s: content does not match its stated size", sha)
	}
	return data, nil
}

// graphqlTarget is the GraphQL endpoint next to the REST root: https://api.github.com/graphql, or
// https://<host>/api/graphql for a GitHub Enterprise Server root that ends in /api/v3.
func (x *ActionAPI) graphqlTarget() string {
	if root, ok := strings.CutSuffix(x.c.root, "/api/v3"); ok {
		return root + "/api/graphql"
	}
	return x.c.root + "/graphql"
}

// repoNodeID returns the repository's GraphQL node id, read once per repository with the reader token.
func (x *ActionAPI) repoNodeID(ctx context.Context, repo change.Repo, base string) (string, error) {
	x.nodeMu.Lock()
	cached, ok := x.nodeIDs[repo]
	x.nodeMu.Unlock()
	if ok {
		return cached, nil
	}
	token, err := x.tokens.Reader(ctx)
	if err != nil {
		return "", err
	}
	resp, err := x.c.call(ctx, request{method: http.MethodGet, path: base, auth: token})
	if err != nil {
		return "", err
	}
	r, err := decode[struct {
		NodeID string `json:"node_id"`
	}](resp, base)
	if err != nil {
		return "", err
	}
	if r.NodeID == "" || len(r.NodeID) > 100 || strings.ContainsAny(r.NodeID, " \t\r\n\"\\") {
		return "", fmt.Errorf("github: repository %s has no usable node id", repo)
	}
	x.nodeMu.Lock()
	if x.nodeIDs == nil {
		x.nodeIDs = map[change.Repo]string{}
	}
	x.nodeIDs[repo] = r.NodeID
	x.nodeMu.Unlock()
	return r.NodeID, nil
}

// graphqlReply is the envelope of a GraphQL answer. GitHub answers 200 with errors for a refused mutation.
type graphqlReply struct {
	Data struct {
		UpdateRefs *json.RawMessage `json:"updateRefs"`
	} `json:"data"`
	Errors []struct {
		Type string `json:"type"`
	} `json:"errors"`
}

// SwapRef moves ref from before to after through GraphQL updateRefs with beforeOid and force false (S1 1.2). It always
// sends beforeOid: without it GitHub compares nothing on a branch.
//
// GitHub refuses a stale beforeOid with an untyped "Something went wrong" error, and a reply lost on the way looks
// the same, so every outcome other than success and FORBIDDEN wraps ports.ErrRefSwapUnconfirmed. FORBIDDEN, 401 and
// 403 are ports.ErrUnauthorized: the token has no contents: write, and the caller fails closed. The error never
// repeats a GraphQL message, only its type.
func (x *ActionAPI) SwapRef(ctx context.Context, repo change.Repo, ref string, before, after id.SHA) error {
	base, err := checkSwap(repo, ref, before, after)
	if err != nil {
		return err
	}
	node, err := x.repoNodeID(ctx, repo, base)
	if err != nil {
		return err
	}
	token, err := x.tokens.Writer(ctx, OpBranchWrite, repo)
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]any{
		"query":     updateRefsMutation,
		"variables": map[string]any{"r": node, "n": ref, "b": string(before), "a": string(after), "f": false},
	})
	if err != nil {
		return fmt.Errorf("github: swap %s: encode body: %w", ref, err)
	}
	resp, err := x.c.call(ctx, request{method: http.MethodPost, path: "graphql", target: x.graphqlTarget(), auth: token, body: body})
	if errors.Is(err, ports.ErrUnauthorized) {
		return fmt.Errorf("github: swap %s: %w", ref, err)
	}
	if err != nil {
		return fmt.Errorf("github: swap %s: %w: %w", ref, ports.ErrRefSwapUnconfirmed, err)
	}
	return swapOutcome(ref, resp.body)
}

// checkSwap refuses a bad repository, ref or hash before any request and returns the repository path.
func checkSwap(repo change.Repo, ref string, before, after id.SHA) (string, error) {
	base, err := repoPath(repo)
	if err != nil {
		return "", err
	}
	if err := checkRef(ref); err != nil {
		return "", err
	}
	if err := checkSHA(before); err != nil {
		return "", err
	}
	if err := checkSHA(after); err != nil {
		return "", err
	}
	return base, nil
}

// swapOutcome reads a GraphQL reply to updateRefs. Success needs no errors and a non-null result.
func swapOutcome(ref string, body []byte) error {
	var out graphqlReply
	if err := json.Unmarshal(body, &out); err != nil {
		return fmt.Errorf("github: swap %s: unreadable reply: %w", ref, ports.ErrRefSwapUnconfirmed)
	}
	if len(out.Errors) > 0 {
		types := make([]string, 0, len(out.Errors))
		for _, e := range out.Errors {
			if e.Type == "FORBIDDEN" {
				return fmt.Errorf("github: swap %s: graphql FORBIDDEN: %w", ref, ports.ErrUnauthorized)
			}
			t := "untyped"
			if reErrorType.MatchString(e.Type) {
				t = e.Type
			}
			types = append(types, t)
		}
		return fmt.Errorf("github: swap %s: graphql error %s: %w", ref, strings.Join(types, ","), ports.ErrRefSwapUnconfirmed)
	}
	if out.Data.UpdateRefs == nil || string(*out.Data.UpdateRefs) == "null" {
		return fmt.Errorf("github: swap %s: reply has no updateRefs result: %w", ref, ports.ErrRefSwapUnconfirmed)
	}
	return nil
}
