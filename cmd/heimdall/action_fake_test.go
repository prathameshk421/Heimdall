package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha1" // #nosec G505 -- the fake names objects the way git does; no security rests on it
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeGitHub is an in-memory GitHub for the action smoke tests: one repository, one pull request, the Git Data API
// with the GraphQL compare-and-swap, check runs, issue comments and workflow artifacts. It checks the token on every
// call, so a step that sends none fails here as it would on GitHub.
type fakeGitHub struct {
	t     *testing.T
	srv   *httptest.Server
	token string

	mu        sync.Mutex
	pr        map[string]any
	files     []map[string]any
	blobs     map[string][]byte
	trees     map[string]map[string]fakeEntry
	commits   map[string]fakeCommit
	refs      map[string]string
	checks    []map[string]any
	comments  []map[string]any
	artifacts map[int64][]fakeArtifact
	nextID    int64
	calls     []string
}

type fakeEntry struct{ mode, sha string }

type fakeCommit struct {
	tree    string
	parents []string
}

type fakeArtifact struct {
	id    int64
	name  string
	files map[string][]byte
}

const (
	fakeRepo   = "acme/pay"
	fakeToken  = "ghs_" + "fakeworkflowtoken0123456789"
	fakeAuthor = "github-actions[bot]"
)

func newFakeGitHub(t *testing.T) *fakeGitHub {
	f := &fakeGitHub{t: t, token: fakeToken, blobs: map[string][]byte{}, trees: map[string]map[string]fakeEntry{},
		commits: map[string]fakeCommit{}, refs: map[string]string{}, artifacts: map[int64][]fakeArtifact{}, nextID: 100}
	f.pr = map[string]any{"number": 7, "title": "Add refunds", "body": "", "state": "open", "merged": false,
		"head": map[string]any{"ref": "feature", "sha": string(testHead)},
		"base": map[string]any{"sha": string(testBase)}, "user": map[string]any{"login": "ana"}}
	f.files = []map[string]any{{"filename": "pay/refund.go", "status": "modified", "additions": 1, "deletions": 0,
		"patch": "@@ -1 +1,2 @@\n package pay\n+func Refund() {}"}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

// addArtifact adds an artifact with one file to a workflow run, zipped as GitHub serves it.
func (f *fakeGitHub) addArtifact(run int64, name string, files map[string][]byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	f.artifacts[run] = append(f.artifacts[run], fakeArtifact{id: f.nextID, name: name, files: files})
}

func (f *fakeGitHub) count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func writeFakeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func gitSHA(parts ...string) string {
	h := sha1.New() // #nosec G401 -- see the import
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// fakeRoute is one endpoint of the fake: a method, a path pattern under the repository, and its handler. m holds the
// pattern's groups.
type fakeRoute struct {
	method string
	path   *regexp.Regexp
	serve  func(w http.ResponseWriter, r *http.Request, m []string, body []byte) bool
}

func (f *fakeGitHub) routes() []fakeRoute {
	route := func(method, pattern string, h func(http.ResponseWriter, *http.Request, []string, []byte) bool) fakeRoute {
		return fakeRoute{method: method, path: regexp.MustCompile("^" + pattern + "$"), serve: h}
	}
	ok := func(v func(*http.Request) any) func(http.ResponseWriter, *http.Request, []string, []byte) bool {
		return func(w http.ResponseWriter, r *http.Request, _ []string, _ []byte) bool {
			writeFakeJSON(w, http.StatusOK, v(r))
			return true
		}
	}
	return []fakeRoute{
		route("GET", "/git/ref/(.+)", f.getRef),
		route("POST", "/git/refs", f.createRef),
		route("POST", "/git/blobs", f.createBlob),
		route("POST", "/git/trees", f.createTree),
		route("POST", "/git/commits", f.createCommit),
		route("GET", "/git/commits/([0-9a-f]{40})", f.getCommit),
		route("GET", "/git/trees/([0-9a-f]{40})", f.getTree),
		route("GET", "/git/blobs/([0-9a-f]{40})", f.getBlob),
		route("GET", "/pulls/7", ok(func(*http.Request) any { return f.pr })),
		route("GET", "/pulls", ok(func(*http.Request) any { return []any{f.pr} })),
		route("GET", "/pulls/7/files", ok(func(*http.Request) any { return f.files })),
		route("GET", "/pulls/7/(?:reviews|comments)", ok(func(*http.Request) any { return []any{} })),
		route("GET", "/issues/7/comments", ok(func(*http.Request) any { return f.comments })),
		route("POST", "/issues/7/comments", f.postComment),
		route("PATCH", `/issues/comments/(\d+)`, f.patchComment),
		route("GET", "/contents/.+", func(http.ResponseWriter, *http.Request, []string, []byte) bool { return false }),
		route("GET", "/commits/([0-9a-f]{40})/check-runs", ok(f.listChecks)),
		route("POST", "/check-runs", f.postCheck),
		route("PATCH", `/check-runs/(\d+)`, ok(func(*http.Request) any { return map[string]any{} })),
		route("GET", `/actions/runs/(\d+)/artifacts`, f.listArtifacts),
		route("GET", `/actions/artifacts/(\d+)/zip`, f.artifactZip),
	}
}

func (f *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+f.token && r.Header.Get("Authorization") != "token "+f.token {
		writeFakeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
		return
	}
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	root := "/repos/" + fakeRepo
	switch r.URL.Path {
	case "/graphql":
		f.swap(w, body)
		return
	case root:
		writeFakeJSON(w, http.StatusOK, map[string]any{"node_id": "R_kgDOfake", "default_branch": "main"})
		return
	}
	if p, found := strings.CutPrefix(r.URL.Path, root); found {
		for _, rt := range f.routes() {
			if m := rt.path.FindStringSubmatch(p); m != nil && rt.method == r.Method && rt.serve(w, r, m, body) {
				return
			}
		}
	}
	writeFakeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
}

func (f *fakeGitHub) getRef(w http.ResponseWriter, _ *http.Request, m []string, _ []byte) bool {
	sha, ok := f.refs["refs/"+m[1]]
	if !ok {
		return false
	}
	writeFakeJSON(w, http.StatusOK, map[string]any{"object": map[string]string{"sha": sha, "type": "commit"}})
	return true
}

func (f *fakeGitHub) createRef(w http.ResponseWriter, _ *http.Request, _ []string, body []byte) bool {
	var in struct{ Ref, SHA string }
	_ = json.Unmarshal(body, &in)
	if _, ok := f.refs[in.Ref]; ok {
		writeFakeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": "Reference already exists"})
		return true
	}
	f.refs[in.Ref] = in.SHA
	writeFakeJSON(w, http.StatusCreated, map[string]any{"ref": in.Ref, "object": map[string]string{"sha": in.SHA}})
	return true
}

func (f *fakeGitHub) createBlob(w http.ResponseWriter, _ *http.Request, _ []string, body []byte) bool {
	var in struct{ Content string }
	_ = json.Unmarshal(body, &in)
	data, _ := base64.StdEncoding.DecodeString(in.Content)
	sha := gitSHA("blob", string(data))
	f.blobs[sha] = data
	writeFakeJSON(w, http.StatusCreated, map[string]string{"sha": sha})
	return true
}

func (f *fakeGitHub) createTree(w http.ResponseWriter, _ *http.Request, _ []string, body []byte) bool {
	var in struct {
		BaseTree string `json:"base_tree"`
		Tree     []struct{ Path, Mode, SHA string }
	}
	_ = json.Unmarshal(body, &in)
	next := map[string]fakeEntry{}
	for k, v := range f.trees[in.BaseTree] {
		next[k] = v
	}
	keys := []string{"tree", in.BaseTree}
	for _, e := range in.Tree {
		next[e.Path] = fakeEntry{mode: e.Mode, sha: e.SHA}
		keys = append(keys, e.Path, e.SHA)
	}
	sha := gitSHA(keys...)
	f.trees[sha] = next
	writeFakeJSON(w, http.StatusCreated, map[string]string{"sha": sha})
	return true
}

func (f *fakeGitHub) createCommit(w http.ResponseWriter, _ *http.Request, _ []string, body []byte) bool {
	var in struct {
		Message, Tree string
		Parents       []string
	}
	_ = json.Unmarshal(body, &in)
	sha := gitSHA(append([]string{"commit", in.Message, in.Tree}, in.Parents...)...)
	f.commits[sha] = fakeCommit{tree: in.Tree, parents: in.Parents}
	writeFakeJSON(w, http.StatusCreated, map[string]string{"sha": sha})
	return true
}

func (f *fakeGitHub) getCommit(w http.ResponseWriter, _ *http.Request, m []string, _ []byte) bool {
	c, ok := f.commits[m[1]]
	if !ok {
		return false
	}
	parents := []map[string]string{}
	for _, ps := range c.parents {
		parents = append(parents, map[string]string{"sha": ps})
	}
	writeFakeJSON(w, http.StatusOK, map[string]any{"tree": map[string]string{"sha": c.tree}, "parents": parents})
	return true
}

func (f *fakeGitHub) getTree(w http.ResponseWriter, _ *http.Request, m []string, _ []byte) bool {
	t, ok := f.trees[m[1]]
	if !ok {
		return false
	}
	items := []map[string]string{}
	for path, e := range t {
		items = append(items, map[string]string{"path": path, "mode": e.mode, "type": "blob", "sha": e.sha})
	}
	writeFakeJSON(w, http.StatusOK, map[string]any{"tree": items, "truncated": false})
	return true
}

func (f *fakeGitHub) getBlob(w http.ResponseWriter, _ *http.Request, m []string, _ []byte) bool {
	data, ok := f.blobs[m[1]]
	if !ok {
		return false
	}
	writeFakeJSON(w, http.StatusOK, map[string]any{"content": base64.StdEncoding.EncodeToString(data),
		"encoding": "base64", "size": len(data)})
	return true
}

// swap is the GraphQL updateRefs compare-and-swap: the ref moves only from the commit the caller read.
func (f *fakeGitHub) swap(w http.ResponseWriter, body []byte) {
	var in struct {
		Variables struct{ N, B, A string } `json:"variables"`
	}
	_ = json.Unmarshal(body, &in)
	if f.refs[in.Variables.N] != in.Variables.B {
		writeFakeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"updateRefs": nil},
			"errors": []map[string]string{{"message": "Something went wrong"}}})
		return
	}
	f.refs[in.Variables.N] = in.Variables.A
	writeFakeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"updateRefs": map[string]any{"clientMutationId": nil}}})
}

func (f *fakeGitHub) postComment(w http.ResponseWriter, _ *http.Request, _ []string, body []byte) bool {
	f.nextID++
	var in struct{ Body string }
	_ = json.Unmarshal(body, &in)
	c := map[string]any{"id": f.nextID, "body": in.Body, "created_at": time.Now().UTC(), "updated_at": time.Now().UTC(),
		"user": map[string]string{"login": fakeAuthor, "type": "Bot"}}
	f.comments = append(f.comments, c)
	writeFakeJSON(w, http.StatusCreated, c)
	return true
}

func (f *fakeGitHub) patchComment(w http.ResponseWriter, _ *http.Request, m []string, body []byte) bool {
	id, _ := strconv.ParseInt(m[1], 10, 64)
	var in struct{ Body string }
	_ = json.Unmarshal(body, &in)
	for _, c := range f.comments {
		if c["id"] == id {
			c["body"] = in.Body
			writeFakeJSON(w, http.StatusOK, c)
			return true
		}
	}
	return false
}

func (f *fakeGitHub) listChecks(r *http.Request) any {
	sha := strings.Split(strings.TrimPrefix(r.URL.Path, "/repos/"+fakeRepo+"/commits/"), "/")[0]
	name := r.URL.Query().Get("check_name")
	runs := []map[string]any{}
	for _, c := range f.checks {
		if c["head_sha"] == sha && (name == "" || c["name"] == name) {
			runs = append(runs, c)
		}
	}
	return map[string]any{"total_count": len(runs), "check_runs": runs}
}

func (f *fakeGitHub) postCheck(w http.ResponseWriter, _ *http.Request, _ []string, body []byte) bool {
	f.nextID++
	c := map[string]any{}
	_ = json.Unmarshal(body, &c)
	c["id"] = f.nextID
	f.checks = append(f.checks, c)
	writeFakeJSON(w, http.StatusCreated, c)
	return true
}

func (f *fakeGitHub) listArtifacts(w http.ResponseWriter, _ *http.Request, m []string, _ []byte) bool {
	run, _ := strconv.ParseInt(m[1], 10, 64)
	list := []map[string]any{}
	for _, a := range f.artifacts[run] {
		list = append(list, map[string]any{"id": a.id, "name": a.name, "size_in_bytes": 100, "expired": false,
			"created_at": time.Now().UTC(), "workflow_run": map[string]any{"id": run, "head_sha": string(testHead)}})
	}
	writeFakeJSON(w, http.StatusOK, map[string]any{"total_count": len(list), "artifacts": list})
	return true
}

func (f *fakeGitHub) artifactZip(w http.ResponseWriter, _ *http.Request, m []string, _ []byte) bool {
	id, _ := strconv.ParseInt(m[1], 10, 64)
	for _, arts := range f.artifacts {
		for _, a := range arts {
			if a.id == id {
				w.Header().Set("Content-Type", "application/zip")
				_, _ = w.Write(zipOf(f.t, a.files))
				return true
			}
		}
	}
	return false
}

func zipOf(t *testing.T, files map[string][]byte) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Errorf("zip: %v", err)
		}
		_, _ = w.Write(content)
	}
	if err := zw.Close(); err != nil {
		t.Errorf("zip: %v", err)
	}
	return buf.Bytes()
}
