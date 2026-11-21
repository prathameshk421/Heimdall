package gitlog

import (
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

	"github.com/stretchr/testify/require"
)

const (
	testToken = "ghs_TESTTOKEN0123456789abcdef"
	testRepo  = "acme/widgets"
	testRef   = "refs/heimdall/state"
)

type fakeEntry struct {
	mode, kind, sha string
}

type fakeCommit struct {
	tree    string
	parents []string
}

// fakeGit is an in-memory Git Data API on an httptest server. It implements blobs, trees (with base_tree and nested
// paths), commits and refs, and a ref update with force false that is refused unless the commit descends from the tip.
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

// store returns a Store on the fake with the given key.
func (g *fakeGit) store(t *testing.T, key []byte) *Store {
	t.Helper()
	s, err := New(Config{Repo: testRepo, Token: testToken, Key: key, BaseURL: g.srv.URL, HTTPClient: g.srv.Client()})
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
	prefix := "/repos/" + testRepo + "/git/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		http.NotFound(w, r)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, prefix)
	isRefWrite := (r.Method == http.MethodPost && rest == "refs") || (r.Method == http.MethodPatch && strings.HasPrefix(rest, "refs/"))
	if isRefWrite {
		g.mu.Lock()
		g.refWrites++
		n, hook := g.refWrites, g.hookBeforeRefWrite
		g.mu.Unlock()
		if hook != nil {
			hook(n)
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	body := readBody(r)
	key := r.Method + " " + rest
	g.calls = append(g.calls, key)
	if len(body) > 0 {
		g.bodies[key] = append(g.bodies[key], body)
	}
	g.route(w, r.Method, rest, body)
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
func (g *fakeGit) route(w http.ResponseWriter, method, rest string, body json.RawMessage) {
	if method == http.MethodGet {
		kind, arg, _ := strings.Cut(rest, "/")
		handlers := map[string]func(http.ResponseWriter, string){
			"ref": func(w http.ResponseWriter, a string) { g.getRef(w, "refs/"+a) }, "commits": g.getCommit,
			"trees": g.getTree, "blobs": g.getBlob,
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
	reply(w, http.StatusOK, map[string]any{"sha": sha, "tree": map[string]string{"sha": c.tree}})
}

func (g *fakeGit) getTree(w http.ResponseWriter, sha string) {
	t, ok := g.trees[sha]
	if !ok {
		reply(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	names := make([]string, 0, len(t))
	for n := range t {
		names = append(names, n)
	}
	sort.Strings(names)
	entries := make([]map[string]string, 0, len(names))
	for _, n := range names {
		e := t[n]
		entries = append(entries, map[string]string{"path": n, "mode": e.mode, "type": e.kind, "sha": e.sha})
	}
	reply(w, http.StatusOK, map[string]any{"sha": sha, "tree": entries, "truncated": g.truncateTrees})
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

func (g *fakeGit) patchRef(w http.ResponseWriter, name string, body json.RawMessage) {
	var in struct {
		SHA   string `json:"sha"`
		Force bool   `json:"force"`
	}
	require.NoError(g.t, json.Unmarshal(body, &in))
	cur, ok := g.refs[name]
	if !ok {
		reply(w, http.StatusUnprocessableEntity, map[string]string{"message": "Reference does not exist"})
		return
	}
	if !in.Force && !g.descends(in.SHA, cur) {
		reply(w, http.StatusUnprocessableEntity, map[string]string{"message": "Update is not a fast forward"})
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
