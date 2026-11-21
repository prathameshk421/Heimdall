package gitlog

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const (
	apiVersion     = "2022-11-28"
	requestTimeout = 30 * time.Second
	// maxSmallReply bounds every reply that carries no file or tree: refs, commits and created objects.
	maxSmallReply = 64 << 10
	// maxTreeReply and maxTreeEntries bound a directory listing. GitHub marks a tree over 100,000 entries truncated.
	maxTreeReply   = 16 << 20
	maxTreeEntries = 50_000
)

var reSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

// errRefused is a 409 or 422 on a ref write. The caller reads the ref again to learn whether it moved.
var errRefused = errors.New("ref write refused")

// api is the part of the GitHub Git Data API this adapter uses. It holds the token and never prints it: errors name
// the method and the path, never a header or a body.
type api struct {
	http  *http.Client
	base  string
	repo  string
	token string
	ref   string // full name, for example refs/heimdall/state
}

// treeEntry is one entry of a tree reply.
type treeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

// newTreeEntry is one path of a tree to create. The path may hold slashes; GitHub makes the subtrees.
type newTreeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

func (a *api) path(format string, args ...any) string {
	return "/repos/" + a.repo + "/git/" + fmt.Sprintf(format, args...)
}

// refPath is the ref name without the leading refs/, as the read and update routes want it.
func (a *api) refPath() string { return strings.TrimPrefix(a.ref, "refs/") }

// do sends one request and decodes the reply into out (nil to ignore it). limit bounds the reply body.
func (a *api) do(ctx context.Context, method, path string, in, out any, limit int64) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("github %s %s: encode request: %w", method, path, err)
		}
		body = bytes.NewReader(raw)
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, a.base+path, body)
	if err != nil {
		return fmt.Errorf("github %s %s: build request: %w", method, path, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("Authorization", "Bearer "+a.token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.http.Do(req)
	if err != nil {
		// The transport error can quote the URL, which holds no secret. It never holds the header.
		return fmt.Errorf("github %s %s: %w: %w", method, path, ports.ErrUnavailable, urlFree(err))
	}
	defer func() { _ = resp.Body.Close() }()
	if err := statusError(method, path, resp); err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	return readJSON(method, path, resp.Body, limit, out)
}

// urlFree returns the cause of a transport error without the *url.Error wrapper, so the message is short.
func urlFree(err error) error {
	var ue interface{ Unwrap() error }
	if errors.As(err, &ue) && ue.Unwrap() != nil {
		return ue.Unwrap()
	}
	return err
}

// statusError maps a reply status to a port error. A 2xx is nil.
func statusError(method, path string, resp *http.Response) error {
	switch s := resp.StatusCode; {
	case s >= 200 && s < 300:
		return nil
	case s == http.StatusNotFound:
		return fmt.Errorf("github %s %s: %w", method, path, ports.ErrNotFound)
	case s == http.StatusUnauthorized:
		return fmt.Errorf("github %s %s: %w", method, path, ports.ErrUnauthorized)
	case s == http.StatusTooManyRequests || (s == http.StatusForbidden && rateLimited(resp)):
		return fmt.Errorf("github %s %s: rate limited (status %d): %w", method, path, s, ports.ErrUnavailable)
	case s == http.StatusForbidden:
		return fmt.Errorf("github %s %s: %w", method, path, ports.ErrUnauthorized)
	case s == http.StatusConflict || s == http.StatusUnprocessableEntity:
		return fmt.Errorf("github %s %s: status %d: %w", method, path, s, errRefused)
	case s >= 500:
		return fmt.Errorf("github %s %s: status %d: %w", method, path, s, ports.ErrUnavailable)
	default:
		return fmt.Errorf("github %s %s: unexpected status %d", method, path, s)
	}
}

func rateLimited(resp *http.Response) bool {
	return resp.Header.Get("Retry-After") != "" || resp.Header.Get("X-RateLimit-Remaining") == "0"
}

// readJSON reads at most limit bytes and decodes them. A longer reply is refused.
func readJSON(method, path string, r io.Reader, limit int64, out any) error {
	raw, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return fmt.Errorf("github %s %s: read reply: %w: %w", method, path, ports.ErrUnavailable, err)
	}
	if int64(len(raw)) > limit {
		return &LimitError{What: "reply of " + method + " " + path, Max: limit}
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("github %s %s: unexpected reply shape: %w", method, path, err)
	}
	return nil
}

func checkSHA(what, sha string) error {
	if !reSHA.MatchString(sha) {
		return fmt.Errorf("github reply: %s is not a 40 character object id", what)
	}
	return nil
}

// refSHA returns the commit the ref points at, or "" when the ref does not exist.
func (a *api) refSHA(ctx context.Context) (string, error) {
	var out struct {
		Object struct {
			SHA  string `json:"sha"`
			Type string `json:"type"`
		} `json:"object"`
	}
	err := a.do(ctx, http.MethodGet, a.path("ref/%s", a.refPath()), nil, &out, maxSmallReply)
	if errors.Is(err, ports.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if out.Object.Type != "commit" {
		return "", fmt.Errorf("github ref %s does not point at a commit", a.ref)
	}
	if err := checkSHA("ref object", out.Object.SHA); err != nil {
		return "", err
	}
	return out.Object.SHA, nil
}

// commitTree returns the root tree of a commit.
func (a *api) commitTree(ctx context.Context, commit string) (string, error) {
	var out struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if err := a.do(ctx, http.MethodGet, a.path("commits/%s", commit), nil, &out, maxSmallReply); err != nil {
		return "", err
	}
	if err := checkSHA("commit tree", out.Tree.SHA); err != nil {
		return "", err
	}
	return out.Tree.SHA, nil
}

// tree lists one directory. A truncated or oversized listing is refused.
func (a *api) tree(ctx context.Context, sha string) ([]treeEntry, error) {
	var out struct {
		Tree      []treeEntry `json:"tree"`
		Truncated bool        `json:"truncated"`
	}
	if err := a.do(ctx, http.MethodGet, a.path("trees/%s", sha), nil, &out, maxTreeReply); err != nil {
		return nil, err
	}
	if out.Truncated || len(out.Tree) > maxTreeEntries {
		return nil, &LimitError{What: "tree listing", Max: maxTreeEntries, Got: int64(len(out.Tree))}
	}
	for _, e := range out.Tree {
		if e.Type == "blob" || e.Type == "tree" {
			if err := checkSHA("tree entry", e.SHA); err != nil {
				return nil, err
			}
		}
	}
	return out.Tree, nil
}

// blob reads one file of at most limit bytes. The size GitHub states is checked before anything is decoded.
func (a *api) blob(ctx context.Context, sha, what string, limit int64) ([]byte, error) {
	var out struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
		Size     int64  `json:"size"`
	}
	replyLimit := limit/3*4 + 4<<10
	if err := a.do(ctx, http.MethodGet, a.path("blobs/%s", sha), nil, &out, replyLimit); err != nil {
		return nil, err
	}
	if out.Size > limit {
		return nil, &LimitError{What: what, Max: limit, Got: out.Size}
	}
	if out.Encoding != "base64" {
		return nil, fmt.Errorf("github blob %s: encoding %q is not base64", sha, out.Encoding)
	}
	data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(out.Content, "\n", ""))
	if err != nil || int64(len(data)) != out.Size {
		return nil, fmt.Errorf("github blob %s: content does not match its stated size", sha)
	}
	return data, nil
}

func (a *api) createBlob(ctx context.Context, data []byte) (string, error) {
	in := map[string]string{"content": base64.StdEncoding.EncodeToString(data), "encoding": "base64"}
	return a.create(ctx, "blobs", in)
}

func (a *api) createTree(ctx context.Context, base string, entries []newTreeEntry) (string, error) {
	in := map[string]any{"tree": entries}
	if base != "" {
		in["base_tree"] = base
	}
	return a.create(ctx, "trees", in)
}

func (a *api) createCommit(ctx context.Context, message, tree string, parents []string) (string, error) {
	if parents == nil {
		parents = []string{}
	}
	return a.create(ctx, "commits", map[string]any{"message": message, "tree": tree, "parents": parents})
}

// create posts an object and returns its id.
func (a *api) create(ctx context.Context, kind string, in any) (string, error) {
	var out struct {
		SHA string `json:"sha"`
	}
	if err := a.do(ctx, http.MethodPost, a.path("%s", kind), in, &out, maxSmallReply); err != nil {
		return "", err
	}
	if err := checkSHA("created "+kind, out.SHA); err != nil {
		return "", err
	}
	return out.SHA, nil
}

// createRef makes the ref. It is refused when the ref already exists.
func (a *api) createRef(ctx context.Context, commit string) error {
	in := map[string]string{"ref": a.ref, "sha": commit}
	return a.do(ctx, http.MethodPost, a.path("refs"), in, nil, maxSmallReply)
}

// updateRef moves the ref to commit with force false, so GitHub refuses unless commit descends from the current tip.
// This is the compare-and-swap of ACTION_MODE 2.3.
func (a *api) updateRef(ctx context.Context, commit string) error {
	in := map[string]any{"sha": commit, "force": false}
	return a.do(ctx, http.MethodPatch, a.path("refs/%s", a.refPath()), in, nil, maxSmallReply)
}
