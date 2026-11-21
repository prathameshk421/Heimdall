package gitlog

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/github"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

const (
	testToken = "ghs_TESTTOKEN0123456789abcdef"
	testRepo  = "acme/widgets"
	testRef   = "refs/heimdall/state"
	testNode  = "R_kgDOTEST"
	// opaqueError is how GitHub refuses an updateRefs with a stale beforeOid (S1 1.2).
	opaqueError = "Something went wrong while executing your query on 2026-11-21T04:00:00Z. Please include `C0DE:1234` when reporting this issue."
)

// swapMode is how the fake answers one updateRefs call.
type swapMode int

const (
	// swapNormal is GitHub as S1 measured it.
	swapNormal swapMode = iota
	// swapLostAnswer applies the update, then answers with the opaque error: the reply was lost.
	swapLostAnswer
	// swapLostGateway applies the update, then answers 502.
	swapLostGateway
	// swapTransient applies nothing and answers with the opaque error, though beforeOid is right.
	swapTransient
	// swapForbidden answers as GitHub does for a token with contents: read.
	swapForbidden
)

type fakeEntry struct {
	mode, kind, sha string
}

type fakeCommit struct {
	tree    string
	parents []string
}

// fakeGit is GitHub as spike S1 measured it, on an httptest server (ACTION_MODE_SPIKE.md 1.1, 1.2, 1.5). It holds
// blobs, trees (with base_tree, nested paths and recursive listing), commits and refs. POST /git/refs refuses a ref
// that exists with 422. GraphQL updateRefs is the compare-and-swap: a beforeOid that is not the tip, or a missing
// beforeOid, gets the opaque "Something went wrong" error, and a non-fast-forward gets UNPROCESSABLE. REST PATCH
// /git/refs on refs/heimdall/* is a plain set with no fast-forward check, as S1 found, so a store that used it would
// lose writes in the tests.
type fakeGit struct {
	t       *testing.T
	srv     *httptest.Server
	mu      sync.Mutex
	blobs   map[string][]byte
	trees   map[string]map[string]fakeEntry
	commits map[string]fakeCommit
	refs    map[string]string
	calls   []string
	bodies  map[string][]json.RawMessage // "METHOD path" -> request bodies

	// swapHook picks how the n-th updateRefs call is answered. Nil means swapNormal.
	swapHook func(n int) swapMode
	swaps    int
	// waits records every wait the store asked for.
	waits []time.Duration

	// hookBeforeRefWrite runs, without the lock, before a ref create or update is handled. A test uses it to let
	// another writer move the ref after the store read it.
	hookBeforeRefWrite func(n int)
	refWrites          int
	// intercept may answer a request itself. It returns true when it did.
	intercept func(w http.ResponseWriter, r *http.Request) bool
	// blobSizeLie, when set, replaces the size field of every blob reply.
	blobSizeLie *int64
	// truncateTrees marks every tree reply truncated.
	truncateTrees bool
}

func newFakeGit(t *testing.T) *fakeGit {
	t.Helper()
	g := &fakeGit{
		t: t, blobs: map[string][]byte{}, trees: map[string]map[string]fakeEntry{}, commits: map[string]fakeCommit{},
		refs: map[string]string{}, bodies: map[string][]json.RawMessage{},
	}
	g.srv = httptest.NewServer(http.HandlerFunc(g.serve))
	t.Cleanup(g.srv.Close)
	return g
}

func objectID(kind string, parts ...string) string {
	h := sha256.New()
	h.Write([]byte(kind))
	for _, p := range parts {
		h.Write([]byte{0})
		h.Write([]byte(p))
	}
	// 40 hex characters, the length of a git object id.
	return hex.EncodeToString(h.Sum(nil))[:40]
}

// fixedClock is the github client's clock. Nothing in these tests depends on time passing.
type fixedClock struct{}

func (fixedClock) Now() time.Time { return base }

// api returns the real GitHub adapter of Action mode, pointed at the fake, with token.
func (g *fakeGit) api(t *testing.T, token string) *github.ActionAPI {
	t.Helper()
	tok, err := github.NewStaticToken(token)
	require.NoError(t, err)
	x, err := github.NewAction(github.ActionConfig{BaseURL: g.srv.URL, Tokens: tok, HTTPClient: g.srv.Client(),
		Clock: fixedClock{}, Wait: func(context.Context, time.Duration) error { return nil }})
	require.NoError(t, err)
	return x
}

// wait records the store's wait and returns at once.
func (g *fakeGit) wait(ctx context.Context, d time.Duration) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.waits = append(g.waits, d)
	return ctx.Err()
}

// store returns a Store on the fake with the given key, through the real GitHub adapter.
func (g *fakeGit) store(t *testing.T, key []byte) *Store {
	t.Helper()
	s, err := New(Config{Repo: change.Repo(testRepo), Git: g.api(t, testToken), Key: key, Wait: g.wait})
	require.NoError(t, err)
	return s
}

func (g *fakeGit) serve(w http.ResponseWriter, r *http.Request) {
	if g.intercept != nil && g.intercept(w, r) {
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+testToken {
		http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
		return
	}
	if g.serveOutsideGit(w, r) {
		return
	}
	prefix := "/repos/" + testRepo + "/git/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		http.NotFound(w, r)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, prefix)
	isRefWrite := (r.Method == http.MethodPost && rest == "refs") || (r.Method == http.MethodPatch && strings.HasPrefix(rest, "refs/"))
	if isRefWrite {
		g.beforeRefWrite()
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	body := readBody(r)
	key := r.Method + " " + rest
	g.calls = append(g.calls, key)
	if len(body) > 0 {
		g.bodies[key] = append(g.bodies[key], body)
	}
	g.route(w, r.Method, rest, r.URL.Query().Get("recursive") == "1", body)
}

// serveOutsideGit answers the repository read (for its node id) and the GraphQL endpoint. It returns false for any
// other path.
func (g *fakeGit) serveOutsideGit(w http.ResponseWriter, r *http.Request) bool {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/repos/"+testRepo:
		g.record("GET repo", nil)
		reply(w, http.StatusOK, map[string]any{"node_id": testNode, "default_branch": "main"})
		return true
	case r.Method == http.MethodPost && r.URL.Path == "/graphql":
		g.graphql(w, readBody(r))
		return true
	}
	return false
}

// beforeRefWrite counts a ref write and runs the hook outside the lock.
func (g *fakeGit) beforeRefWrite() {
	g.mu.Lock()
	g.refWrites++
	n, hook := g.refWrites, g.hookBeforeRefWrite
	g.mu.Unlock()
	if hook != nil {
		hook(n)
	}
}

func (g *fakeGit) record(key string, body json.RawMessage) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, key)
	if len(body) > 0 {
		g.bodies[key] = append(g.bodies[key], body)
	}
}

// swapVars are the variables of the updateRefs mutation (ACTION_MODE_SPIKE.md 1.2).
type swapVars struct {
	R string  `json:"r"`
	N string  `json:"n"`
	B *string `json:"b"`
	A string  `json:"a"`
	F bool    `json:"f"`
}

func graphqlErr(kind, message string) map[string]any {
	e := map[string]any{"message": message}
	if kind != "" {
		e["type"] = kind
	}
	return map[string]any{"errors": []map[string]any{e}}
}

// graphql answers the updateRefs mutation the way S1 measured it on refs/heimdall/* (ACTION_MODE_SPIKE.md 1.2).
func (g *fakeGit) graphql(w http.ResponseWriter, body json.RawMessage) {
	g.beforeRefWrite()
	g.record("POST graphql", body)
	g.mu.Lock()
	defer g.mu.Unlock()
	g.swaps++
	mode := swapNormal
	if g.swapHook != nil {
		mode = g.swapHook(g.swaps)
	}
	var in struct {
		Query     string   `json:"query"`
		Variables swapVars `json:"variables"`
	}
	require.NoError(g.t, json.Unmarshal(body, &in))
	require.Contains(g.t, in.Query, "updateRefs(")
	require.Contains(g.t, in.Query, "beforeOid:$b")
	switch mode {
	case swapForbidden:
		reply(w, http.StatusOK, graphqlErr("FORBIDDEN", "Resource not accessible by integration"))
		return
	case swapTransient:
		reply(w, http.StatusOK, graphqlErr("", opaqueError))
		return
	case swapNormal, swapLostAnswer, swapLostGateway:
	}
	if refusal := g.applySwap(in.Variables); refusal != nil {
		reply(w, http.StatusOK, refusal)
		return
	}
	switch mode {
	case swapLostAnswer:
		reply(w, http.StatusOK, graphqlErr("", opaqueError))
	case swapLostGateway:
		reply(w, http.StatusBadGateway, map[string]string{"message": "Server Error"})
	case swapNormal, swapTransient, swapForbidden:
		reply(w, http.StatusOK, map[string]any{"data": map[string]any{"updateRefs": map[string]any{"clientMutationId": nil}}})
	}
}

// applySwap moves the ref when beforeOid is the tip and afterOid a fast-forward, and returns nil. Otherwise it returns
// GitHub's refusal: the opaque error for a stale or missing beforeOid, UNPROCESSABLE for a non-fast-forward.
func (g *fakeGit) applySwap(v swapVars) map[string]any {
	if v.R != testNode {
		return graphqlErr("NOT_FOUND", "Could not resolve to a node")
	}
	cur, exists := g.refs[v.N]
	if _, ok := g.commits[v.A]; !ok || !exists || v.B == nil || (!v.F && *v.B != cur) {
		return graphqlErr("", opaqueError)
	}
	if !v.F && !g.descends(v.A, cur) {
		return graphqlErr("UNPROCESSABLE", "rejecting non-fast forward update")
	}
	g.refs[v.N] = v.A
	return nil
}

func readBody(r *http.Request) json.RawMessage {
	buf := make([]byte, 0, 1024)
	tmp := make([]byte, 4096)
	for {
		n, err := r.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			return buf
		}
	}
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// route sends a request to its handler. rest is the path after /git/.
func (g *fakeGit) route(w http.ResponseWriter, method, rest string, recursive bool, body json.RawMessage) {
	if method == http.MethodGet {
		kind, arg, _ := strings.Cut(rest, "/")
		handlers := map[string]func(http.ResponseWriter, string){
			"ref": func(w http.ResponseWriter, a string) { g.getRef(w, "refs/"+a) }, "commits": g.getCommit,
			"trees": func(w http.ResponseWriter, a string) { g.getTree(w, a, recursive) }, "blobs": g.getBlob,
		}
		if h, ok := handlers[kind]; ok {
			h(w, arg)
			return
		}
	}
	posts := map[string]func(http.ResponseWriter, json.RawMessage){
		"blobs": g.postBlob, "trees": g.postTree, "commits": g.postCommit, "refs": g.postRef,
	}
	if h, ok := posts[rest]; ok && method == http.MethodPost {
		h(w, body)
		return
	}
	if name, ok := strings.CutPrefix(rest, "refs/"); ok && method == http.MethodPatch {
		g.patchRef(w, "refs/"+name, body)
		return
	}
	http.NotFound(w, nil)
}

func (g *fakeGit) getRef(w http.ResponseWriter, name string) {
	sha, ok := g.refs[name]
	if !ok {
		reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	reply(w, http.StatusOK, map[string]any{"ref": name, "object": map[string]string{"sha": sha, "type": "commit"}})
}

func (g *fakeGit) getCommit(w http.ResponseWriter, sha string) {
	c, ok := g.commits[sha]
	if !ok {
		reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	parents := make([]map[string]string, 0, len(c.parents))
	for _, p := range c.parents {
		parents = append(parents, map[string]string{"sha": p})
	}
	reply(w, http.StatusOK, map[string]any{"sha": sha, "tree": map[string]string{"sha": c.tree}, "parents": parents})
}

func (g *fakeGit) getTree(w http.ResponseWriter, sha string, recursive bool) {
	if _, ok := g.trees[sha]; !ok {
		reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	entries := g.listTree(sha, "", recursive)
	reply(w, http.StatusOK, map[string]any{"sha": sha, "tree": entries, "truncated": g.truncateTrees})
}

// listTree lists a tree in path order, descending into subtrees when recursive is set, as GitHub does.
func (g *fakeGit) listTree(sha, prefix string, recursive bool) []map[string]string {
	t := g.trees[sha]
	names := make([]string, 0, len(t))
	for n := range t {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []map[string]string
	for _, n := range names {
		e := t[n]
		out = append(out, map[string]string{"path": prefix + n, "mode": e.mode, "type": e.kind, "sha": e.sha})
		if recursive && e.kind == "tree" {
			out = append(out, g.listTree(e.sha, prefix+n+"/", true)...)
		}
	}
	return out
}

func (g *fakeGit) getBlob(w http.ResponseWriter, sha string) {
	data, ok := g.blobs[sha]
	if !ok {
		reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	size := int64(len(data))
	if g.blobSizeLie != nil {
		size = *g.blobSizeLie
	}
	reply(w, http.StatusOK, map[string]any{"sha": sha, "encoding": "base64", "size": size,
		"content": base64.StdEncoding.EncodeToString(data)})
}

func (g *fakeGit) postBlob(w http.ResponseWriter, body json.RawMessage) {
	var in struct{ Content, Encoding string }
	require.NoError(g.t, json.Unmarshal(body, &in))
	require.Equal(g.t, "base64", in.Encoding)
	data, err := base64.StdEncoding.DecodeString(in.Content)
	require.NoError(g.t, err)
	sha := objectID("blob", string(data))
	g.blobs[sha] = data
	reply(w, http.StatusCreated, map[string]string{"sha": sha})
}

type postedEntry struct{ Path, Mode, Type, SHA string }

func (g *fakeGit) postTree(w http.ResponseWriter, body json.RawMessage) {
	var in struct {
		BaseTree string        `json:"base_tree"`
		Tree     []postedEntry `json:"tree"`
	}
	require.NoError(g.t, json.Unmarshal(body, &in))
	if in.BaseTree != "" {
		if _, ok := g.trees[in.BaseTree]; !ok {
			reply(w, http.StatusUnprocessableEntity, map[string]string{"message": "base tree not found"})
			return
		}
	}
	for _, e := range in.Tree {
		if _, ok := g.blobs[e.SHA]; !ok && e.SHA != "" {
			reply(w, http.StatusUnprocessableEntity, map[string]string{"message": "blob not found"})
			return
		}
	}
	reply(w, http.StatusCreated, map[string]string{"sha": g.putTree(in.BaseTree, in.Tree)})
}

// putTree applies paths on a base tree, making subtrees as needed. An entry with an empty sha deletes the path.
func (g *fakeGit) putTree(base string, ents []postedEntry) string {
	m := map[string]fakeEntry{}
	for k, v := range g.trees[base] {
		m[k] = v
	}
	nested := map[string][]postedEntry{}
	for _, e := range ents {
		dir, rest, found := strings.Cut(e.Path, "/")
		if !found {
			if e.SHA == "" {
				delete(m, dir)
			} else {
				m[dir] = fakeEntry{mode: e.Mode, kind: "blob", sha: e.SHA}
			}
			continue
		}
		e.Path = rest
		nested[dir] = append(nested[dir], e)
	}
	for dir, sub := range nested {
		subBase := ""
		if old, ok := m[dir]; ok && old.kind == "tree" {
			subBase = old.sha
		}
		m[dir] = fakeEntry{mode: "040000", kind: "tree", sha: g.putTree(subBase, sub)}
	}
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, n+":"+m[n].kind+":"+m[n].sha+":"+m[n].mode)
	}
	sha := objectID("tree", parts...)
	g.trees[sha] = m
	return sha
}

func (g *fakeGit) postCommit(w http.ResponseWriter, body json.RawMessage) {
	var in struct {
		Message string   `json:"message"`
		Tree    string   `json:"tree"`
		Parents []string `json:"parents"`
	}
	require.NoError(g.t, json.Unmarshal(body, &in))
	if _, ok := g.trees[in.Tree]; !ok {
		reply(w, http.StatusUnprocessableEntity, map[string]string{"message": "tree not found"})
		return
	}
	sha := objectID("commit", in.Message, in.Tree, strings.Join(in.Parents, ","), fmt.Sprint(len(g.commits)))
	g.commits[sha] = fakeCommit{tree: in.Tree, parents: in.Parents}
	reply(w, http.StatusCreated, map[string]string{"sha": sha})
}

func (g *fakeGit) postRef(w http.ResponseWriter, body json.RawMessage) {
	var in struct{ Ref, SHA string }
	require.NoError(g.t, json.Unmarshal(body, &in))
	if _, ok := g.refs[in.Ref]; ok {
		reply(w, http.StatusUnprocessableEntity, map[string]string{"message": "Reference already exists"})
		return
	}
	g.refs[in.Ref] = in.SHA
	reply(w, http.StatusCreated, map[string]string{"ref": in.Ref})
}

// patchRef is REST PATCH /git/refs as S1 measured it on refs/heimdall/*: a plain set. It checks neither the old tip
// nor fast-forward, whatever force says. The store must never use it.
func (g *fakeGit) patchRef(w http.ResponseWriter, name string, body json.RawMessage) {
	var in struct {
		SHA string `json:"sha"`
	}
	require.NoError(g.t, json.Unmarshal(body, &in))
	if _, ok := g.refs[name]; !ok {
		reply(w, http.StatusUnprocessableEntity, map[string]string{"message": "Reference does not exist"})
		return
	}
	g.refs[name] = in.SHA
	reply(w, http.StatusOK, map[string]string{"ref": name})
}

// descends reports whether commit has ancestor as itself or as a parent, walking back.
func (g *fakeGit) descends(commit, ancestor string) bool {
	for commit != "" {
		if commit == ancestor {
			return true
		}
		ps := g.commits[commit].parents
		if len(ps) == 0 {
			return false
		}
		commit = ps[0]
	}
	return false
}

// tipTree returns the root tree of the ref, or "".
func (g *fakeGit) tipTree() string {
	tip, ok := g.refs[testRef]
	if !ok {
		return ""
	}
	return g.commits[tip].tree
}

// writeFiles makes a commit on the tip that sets each path to the content. A nil content deletes the path. It moves
// the ref, as a person with write access or another gate run would.
func (g *fakeGit) writeFiles(files map[string][]byte) {
	g.mu.Lock()
	defer g.mu.Unlock()
	var ents []postedEntry
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		if files[p] == nil {
			ents = append(ents, postedEntry{Path: p})
			continue
		}
		sha := objectID("blob", string(files[p]))
		g.blobs[sha] = files[p]
		ents = append(ents, postedEntry{Path: p, Mode: "100644", Type: "blob", SHA: sha})
	}
	tree := g.putTree(g.tipTree(), ents)
	var parents []string
	if tip, ok := g.refs[testRef]; ok {
		parents = []string{tip}
	}
	sha := objectID("commit", "external", tree, fmt.Sprint(len(g.commits)))
	g.commits[sha] = fakeCommit{tree: tree, parents: parents}
	g.refs[testRef] = sha
}

// file returns the content of a path at the tip, or nil.
func (g *fakeGit) file(path string) []byte {
	g.mu.Lock()
	defer g.mu.Unlock()
	cur := g.tipTree()
	parts := strings.Split(path, "/")
	for i, p := range parts {
		e, ok := g.trees[cur][p]
		if !ok {
			return nil
		}
		if i == len(parts)-1 {
			return g.blobs[e.sha]
		}
		cur = e.sha
	}
	return nil
}

func (g *fakeGit) tip() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.refs[testRef]
}

func (g *fakeGit) callCount(prefix string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := 0
	for _, c := range g.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}
