//go:build component

package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testset"
)

// The fakes of the two flow checks (HARNESS_DESIGN B9 items 1 and 2): one GitHub repository that remembers what the
// server wrote to it, a fake model provider for the liveness probe, and the runner's container runtime. Each fake
// answers only the routes the real adapters call; any other route is a 404 that the test reports by name.

const (
	flowRepo     = "acme/pay"
	flowBaseRef  = "main"
	flowBaseSHA  = id.SHA("b100000000000000000000000000000000000001")
	flowAuthor   = "dev-ana"
	flowApprover = "bob"
)

// flowCommit is one commit of the fake repository: its parent and the files at it.
type flowCommit struct {
	parent string
	files  map[string]string
}

// flowCheck is one check run the server set on a commit.
type flowCheck struct {
	ID         int64
	SHA        string
	Name       string
	Status     string
	Conclusion string
	ExternalID string
	Summary    string
}

// flowPR is the one pull request of the fake repository.
type flowPR struct {
	number int
	title  string
	body   string
	branch string
	author string
	head   string
	open   bool
	merged bool
}

// fakeRepoGitHub is a GitHub with one repository. Commits, refs, blobs, trees and check runs the server writes are
// kept, so a later read sees them, as on GitHub.
type fakeRepoGitHub struct {
	t       *testing.T
	srv     *httptest.Server
	mu      sync.Mutex
	commits map[string]flowCommit
	refs    map[string]string
	blobs   map[string]string
	trees   map[string]map[string]string // tree id -> path -> blob sha, for trees the server posted
	pr      *flowPR
	reviews []map[string]any
	checks  []flowCheck
	unknown []string
	seq     int
}

func newFakeRepoGitHub(t *testing.T, baseFiles map[string]string) *fakeRepoGitHub {
	t.Helper()
	f := &fakeRepoGitHub{t: t, commits: map[string]flowCommit{string(flowBaseSHA): {files: baseFiles}},
		refs: map[string]string{flowBaseRef: string(flowBaseSHA)}, blobs: map[string]string{}, trees: map[string]map[string]string{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	t.Cleanup(func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		if len(f.unknown) > 0 {
			t.Errorf("the fake GitHub got routes it does not serve: %s", strings.Join(f.unknown, ", "))
		}
	})
	return f
}

// gitSHA names a new object of the fake from a sequence number, as a 40 character hex id.
func (f *fakeRepoGitHub) gitSHA(kind string) string {
	f.seq++
	sum := sha1.Sum([]byte(kind + strconv.Itoa(f.seq)))
	return hex.EncodeToString(sum[:])
}

// push makes a commit on the ticket branch with files changed on top of its head, as a developer push does, and returns
// its SHA. The open pull request moves to it.
func (f *fakeRepoGitHub) push(files map[string]string) id.SHA {
	branch := flowBranch
	f.mu.Lock()
	defer f.mu.Unlock()
	parent := f.refs[branch]
	next := map[string]string{}
	for p, c := range f.commits[parent].files {
		next[p] = c
	}
	for p, c := range files {
		next[p] = c
	}
	sha := f.gitSHA("push")
	f.commits[sha] = flowCommit{parent: parent, files: next}
	f.refs[branch] = sha
	if f.pr != nil && f.pr.branch == branch {
		f.pr.head = sha
	}
	return id.SHA(sha)
}

// openPR opens the developer's pull request of the ticket branch into main: #7, "KAN-1: Add refunds".
func (f *fakeRepoGitHub) openPR() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pr = &flowPR{number: 7, title: "KAN-1: Add refunds", body: "Implements KAN-1.", branch: flowBranch, author: flowAuthor,
		head: f.refs[flowBranch], open: true}
}

// approve records an APPROVED review by login on the current head.
func (f *fakeRepoGitHub) approve(login string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reviews = append(f.reviews, map[string]any{"id": len(f.reviews) + 100, "state": "APPROVED", "commit_id": f.pr.head,
		"submitted_at": time.Now().UTC().Format(time.RFC3339), "user": map[string]any{"login": login}})
}

// branchHead returns the commit a branch points at.
func (f *fakeRepoGitHub) branchHead(branch string) id.SHA {
	f.mu.Lock()
	defer f.mu.Unlock()
	return id.SHA(f.refs[branch])
}

// filesAt returns the files at a commit.
func (f *fakeRepoGitHub) filesAt(sha id.SHA) map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.commits[string(sha)].files
}

// checksOn returns the latest state of each check run on sha, by name.
func (f *fakeRepoGitHub) checksOn(sha id.SHA) map[string]flowCheck {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]flowCheck{}
	for _, c := range f.checks {
		if c.SHA == string(sha) {
			out[c.Name] = c
		}
	}
	return out
}

func (f *fakeRepoGitHub) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost && r.URL.Path == fmt.Sprintf("/app/installations/%s/access_tokens", acceptanceInstall) {
		writeJSON(w, http.StatusCreated, map[string]any{"token": acceptanceTicketTok, "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+acceptanceTicketTok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
		return
	}
	rest, ok := strings.CutPrefix(r.URL.Path, "/repos/"+flowRepo)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pr != nil {
		num := "/" + strconv.Itoa(f.pr.number)
		rest = strings.Replace(strings.Replace(rest, "/pulls"+num, "/pulls/{n}", 1), "/issues"+num, "/issues/{n}", 1)
	}
	for _, route := range flowRoutes() {
		arg, matched := route.match(r.Method, rest)
		if ok && matched {
			route.handle(f, w, r, arg)
			return
		}
	}
	f.unknown = append(f.unknown, r.Method+" "+r.URL.Path)
	writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
}

// flowRoute is one GitHub route of the fake: a method and a path under the repository, exact or as a prefix whose
// remainder the handler gets.
type flowRoute struct {
	method string
	path   string
	prefix bool
	handle func(f *fakeRepoGitHub, w http.ResponseWriter, r *http.Request, arg string)
}

func (rt flowRoute) match(method, rest string) (string, bool) {
	if method != rt.method {
		return "", false
	}
	if !rt.prefix {
		return "", rest == rt.path
	}
	arg, ok := strings.CutPrefix(rest, rt.path)
	return arg, ok
}

// flowRoutes are the routes the real GitHub adapter calls. A pull request number is "{n}" in the path.
func flowRoutes() []flowRoute {
	get, post, patch := http.MethodGet, http.MethodPost, http.MethodPatch
	return []flowRoute{
		{get, "", false, (*fakeRepoGitHub).serveRepo},
		{get, "/commits/", true, (*fakeRepoGitHub).serveCommit},
		{get, "/contents/", true, (*fakeRepoGitHub).serveContents},
		{get, "/tarball/", true, (*fakeRepoGitHub).serveTarball},
		{get, "/git/ref/heads/", true, (*fakeRepoGitHub).serveRef},
		{post, "/git/refs", false, (*fakeRepoGitHub).postRef},
		{patch, "/git/refs/heads/", true, (*fakeRepoGitHub).patchRef},
		{get, "/git/commits/", true, (*fakeRepoGitHub).serveGitCommit},
		{post, "/git/blobs", false, (*fakeRepoGitHub).postBlob},
		{post, "/git/trees", false, (*fakeRepoGitHub).postTree},
		{post, "/git/commits", false, (*fakeRepoGitHub).postCommit},
		{get, "/git/trees/", true, (*fakeRepoGitHub).serveTree},
		{get, "/pulls", false, (*fakeRepoGitHub).servePulls},
		{get, "/pulls/{n}", false, func(f *fakeRepoGitHub, w http.ResponseWriter, _ *http.Request, _ string) {
			writeJSON(w, http.StatusOK, f.prJSON())
		}},
		{get, "/pulls/{n}/files", false, func(f *fakeRepoGitHub, w http.ResponseWriter, _ *http.Request, _ string) {
			writeJSON(w, http.StatusOK, f.prFiles())
		}},
		{get, "/pulls/{n}/reviews", false, func(f *fakeRepoGitHub, w http.ResponseWriter, _ *http.Request, _ string) {
			writeJSON(w, http.StatusOK, append([]map[string]any{}, f.reviews...))
		}},
		{get, "/issues/{n}/comments", false, serveNoComments},
		{get, "/pulls/{n}/comments", false, serveNoComments},
		{post, "/issues/{n}/comments", false, postComment},
		{post, "/pulls/{n}/comments", false, postComment},
		{post, "/check-runs", false, (*fakeRepoGitHub).postCheck},
		{patch, "/check-runs/", true, (*fakeRepoGitHub).patchCheck},
	}
}

func serveNoComments(_ *fakeRepoGitHub, w http.ResponseWriter, _ *http.Request, _ string) {
	writeJSON(w, http.StatusOK, []any{})
}

func postComment(_ *fakeRepoGitHub, w http.ResponseWriter, _ *http.Request, _ string) {
	writeJSON(w, http.StatusCreated, map[string]any{"id": 1})
}

func (f *fakeRepoGitHub) serveRepo(w http.ResponseWriter, _ *http.Request, _ string) {
	writeJSON(w, http.StatusOK, map[string]any{"default_branch": flowBaseRef})
}

// serveCommit answers a commit read by branch or SHA, and the check runs of a commit.
func (f *fakeRepoGitHub) serveCommit(w http.ResponseWriter, r *http.Request, ref string) {
	if sha, ok := strings.CutSuffix(ref, "/check-runs"); ok {
		f.serveCheckRuns(w, r, sha)
		return
	}
	if sha, ok := f.refs[ref]; ok {
		ref = sha
	}
	writeJSON(w, http.StatusOK, map[string]any{"sha": ref})
}

func (f *fakeRepoGitHub) serveContents(w http.ResponseWriter, r *http.Request, path string) {
	content, ok := f.commits[r.URL.Query().Get("ref")].files[path]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	w.Header().Set("Content-Type", "application/vnd.github.raw+json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(content))
}

// serveTarball answers with the commit and its files, which the runner fake reads back out of the staged archive.
func (f *fakeRepoGitHub) serveTarball(w http.ResponseWriter, _ *http.Request, sha string) {
	raw, _ := json.Marshal(flowArchive{SHA: sha, Files: f.commits[sha].files})
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func (f *fakeRepoGitHub) serveRef(w http.ResponseWriter, _ *http.Request, branch string) {
	sha, ok := f.refs[branch]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": map[string]any{"sha": sha}})
}

func (f *fakeRepoGitHub) postRef(w http.ResponseWriter, r *http.Request, _ string) {
	var body struct{ Ref, SHA string }
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.refs[strings.TrimPrefix(body.Ref, "refs/heads/")] = body.SHA
	writeJSON(w, http.StatusCreated, map[string]any{"ref": body.Ref})
}

func (f *fakeRepoGitHub) patchRef(w http.ResponseWriter, r *http.Request, branch string) {
	var body struct{ SHA string }
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.refs[branch] = body.SHA
	writeJSON(w, http.StatusOK, map[string]any{"object": map[string]any{"sha": body.SHA}})
}

func (f *fakeRepoGitHub) serveGitCommit(w http.ResponseWriter, _ *http.Request, sha string) {
	writeJSON(w, http.StatusOK, map[string]any{"sha": sha, "tree": map[string]any{"sha": "root-" + sha}})
}

func (f *fakeRepoGitHub) postBlob(w http.ResponseWriter, r *http.Request, _ string) {
	var body struct{ Content string }
	_ = json.NewDecoder(r.Body).Decode(&body)
	raw, _ := base64.StdEncoding.DecodeString(body.Content)
	sha := f.gitSHA("blob")
	f.blobs[sha] = string(raw)
	writeJSON(w, http.StatusCreated, map[string]any{"sha": sha})
}

func (f *fakeRepoGitHub) postTree(w http.ResponseWriter, r *http.Request, _ string) {
	var body struct {
		BaseTree string `json:"base_tree"`
		Tree     []struct{ Path, SHA string }
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	entries := map[string]string{}
	for _, e := range body.Tree {
		entries[e.Path] = e.SHA
	}
	sha := f.gitSHA("tree")
	f.trees[sha] = entries
	f.trees[sha][".base"] = strings.TrimPrefix(body.BaseTree, "root-")
	writeJSON(w, http.StatusCreated, map[string]any{"sha": sha})
}

func (f *fakeRepoGitHub) postCommit(w http.ResponseWriter, r *http.Request, _ string) {
	var body struct {
		Tree    string
		Parents []string
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	tree := f.trees[body.Tree]
	next := map[string]string{}
	for p, c := range f.commits[tree[".base"]].files {
		next[p] = c
	}
	for p, blob := range tree {
		if p != ".base" {
			next[p] = f.blobs[blob]
		}
	}
	sha := f.gitSHA("commit")
	f.commits[sha] = flowCommit{parent: body.Parents[0], files: next}
	writeJSON(w, http.StatusCreated, map[string]any{"sha": sha})
}

// serveTree answers a tree read. A tree id is "root-<commit>" or "root-<commit>/<dir>": the fake builds every tree
// from the commit's files. The tests directory of a ticket has the hash git gives it, so the gate can compare it with
// the hash pinned at verification.
func (f *fakeRepoGitHub) serveTree(w http.ResponseWriter, r *http.Request, treeID string) {
	if r.URL.Query().Get("recursive") == "1" {
		paths := make([]any, 0)
		for p := range f.commits[treeID].files {
			paths = append(paths, map[string]any{"path": p, "type": "blob", "sha": string(flowBaseSHA)})
		}
		writeJSON(w, http.StatusOK, map[string]any{"sha": treeID, "tree": paths})
		return
	}
	commit, dir, _ := strings.Cut(strings.TrimPrefix(treeID, "root-"), "/")
	files := f.commits[commit].files
	prefix := ""
	if dir != "" {
		prefix = dir + "/"
	}
	children := map[string]bool{}
	for p := range files {
		if rest, ok := strings.CutPrefix(p, prefix); ok && strings.Contains(rest, "/") {
			children[strings.SplitN(rest, "/", 2)[0]] = true
		}
	}
	var entries []any
	for name := range children {
		entries = append(entries, map[string]any{"path": name, "type": "tree", "sha": f.treeHashOf(commit, prefix+name)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"sha": treeID, "tree": entries})
}

// treeHashOf names a directory of a commit. A ticket's tests directory gets the git tree hash of its files, so it
// matches the hash Heimdall pinned for them; any other directory gets an id the fake can walk further.
func (f *fakeRepoGitHub) treeHashOf(commit, dir string) string {
	if strings.Count(dir, "/") == 2 && strings.HasPrefix(dir, "tests/heimdall/") {
		return gitTreeHash(f.commits[commit].files, dir)
	}
	return "root-" + commit + "/" + dir
}

func (f *fakeRepoGitHub) servePulls(w http.ResponseWriter, _ *http.Request, _ string) {
	if f.pr == nil || !f.pr.open {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, []any{f.prJSON()})
}

func (f *fakeRepoGitHub) prJSON() map[string]any {
	state := "open"
	if !f.pr.open {
		state = "closed"
	}
	return map[string]any{"number": f.pr.number, "title": f.pr.title, "body": f.pr.body, "state": state, "merged": f.pr.merged,
		"user": map[string]any{"login": f.pr.author}, "head": map[string]any{"ref": f.pr.branch, "sha": f.pr.head},
		"base": map[string]any{"ref": flowBaseRef, "sha": string(flowBaseSHA)}}
}

// prFiles is the diff of the pull request: every file that differs between main and the head.
func (f *fakeRepoGitHub) prFiles() []any {
	base, head := f.commits[string(flowBaseSHA)].files, f.commits[f.pr.head].files
	var names []string
	for p, c := range head {
		if old, ok := base[p]; !ok || old != c {
			names = append(names, p)
		}
	}
	sort.Strings(names)
	out := make([]any, 0, len(names))
	for _, p := range names {
		status := "modified"
		if _, ok := base[p]; !ok {
			status = "added"
		}
		lines := strings.Count(head[p], "\n")
		out = append(out, map[string]any{"filename": p, "status": status, "additions": lines, "deletions": 0, "changes": lines})
	}
	return out
}

// serveCheckRuns lists the check runs on sha, newest state of each, filtered by check_name.
func (f *fakeRepoGitHub) serveCheckRuns(w http.ResponseWriter, r *http.Request, sha string) {
	name := r.URL.Query().Get("check_name")
	runs := []any{}
	// The repository's own CI reports build success on every commit of the branch.
	if name == "" || name == "build" {
		runs = append(runs, map[string]any{"id": 1, "name": "build", "head_sha": sha, "status": "completed", "conclusion": "success"})
	}
	latest := map[int64]flowCheck{}
	var order []int64
	for _, c := range f.checks {
		if c.SHA != sha || (name != "" && c.Name != name) {
			continue
		}
		if _, seen := latest[c.ID]; !seen {
			order = append(order, c.ID)
		}
		latest[c.ID] = c
	}
	for _, cid := range order {
		runs = append(runs, latest[cid].runJSON())
	}
	writeJSON(w, http.StatusOK, map[string]any{"total_count": len(runs), "check_runs": runs})
}

func (c flowCheck) runJSON() map[string]any {
	run := map[string]any{"id": c.ID, "name": c.Name, "head_sha": c.SHA, "status": c.Status, "external_id": c.ExternalID}
	if c.Conclusion != "" {
		run["conclusion"] = c.Conclusion
	}
	return run
}

func (f *fakeRepoGitHub) postCheck(w http.ResponseWriter, r *http.Request, _ string) {
	c := f.decodeCheck(r)
	c.ID = int64(len(f.checks) + 1000)
	f.checks = append(f.checks, c)
	writeJSON(w, http.StatusCreated, map[string]any{"id": c.ID})
}

func (f *fakeRepoGitHub) patchCheck(w http.ResponseWriter, r *http.Request, arg string) {
	runID, _ := strconv.ParseInt(arg, 10, 64)
	c := f.decodeCheck(r)
	for _, old := range f.checks {
		if old.ID == runID {
			c.SHA, c.Name = old.SHA, old.Name
		}
	}
	c.ID = runID
	f.checks = append(f.checks, c)
	writeJSON(w, http.StatusOK, map[string]any{"id": runID})
}

func (f *fakeRepoGitHub) decodeCheck(r *http.Request) flowCheck {
	var body struct {
		Name       string `json:"name"`
		HeadSHA    string `json:"head_sha"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		ExternalID string `json:"external_id"`
		Output     struct {
			Summary string `json:"summary"`
		} `json:"output"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	return flowCheck{SHA: body.HeadSHA, Name: body.Name, Status: body.Status, Conclusion: body.Conclusion,
		ExternalID: body.ExternalID, Summary: body.Output.Summary}
}

// gitTreeHash is the git tree hash of the files under dir, computed by the same code that pins verified tests.
func gitTreeHash(files map[string]string, dir string) string {
	var sub testset.Submission
	for p, c := range files {
		if strings.HasPrefix(p, dir+"/") {
			sub.Files = append(sub.Files, testset.File{Path: p, Content: c})
		}
	}
	h, err := sub.TreeHash(dir)
	if err != nil {
		return "root-unknown/" + dir
	}
	return h
}

// flowArchive is what the fake GitHub serves as a commit's tarball.
type flowArchive struct {
	SHA   string            `json:"sha"`
	Files map[string]string `json:"files"`
}

// fakeModelProvider answers the liveness probe like a healthy OpenRouter, and counts the probes.
type fakeModelProvider struct {
	srv    *httptest.Server
	mu     sync.Mutex
	probes int
}

func newFakeModelProvider(t *testing.T) *fakeModelProvider {
	t.Helper()
	f := &fakeModelProvider{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.probes++
		f.mu.Unlock()
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer "+flowOpenRouterKey {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "bad key or route"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "ok"}}}})
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeModelProvider) probeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.probes
}

const flowOpenRouterKey = "openrouter-key-value"

// flowContainers is the container runtime under the real runner: it reads the commit from the staged archive and the
// submitted tests from the staged overlays, and answers as the tools would. Everything around it (the runner's HTTP
// handler, its checks, the queue headers, the client's parsing) is the production code.
type flowContainers struct {
	mu sync.Mutex
	// findingAt is the check result per commit: the file and line the "patterns" check reports there.
	findingAt map[string]string
	jobs      []string
}

// Execute plays one container run. A check job prints SARIF; a test job prints go test -json events.
func (c *flowContainers) Execute(_ context.Context, args []string, dir string) ([]byte, error) {
	archive, err := os.ReadFile(filepath.Join(dir, "source.tgz"))
	if err != nil {
		return nil, fmt.Errorf("read the staged archive: %w", err)
	}
	var src flowArchive
	if err := json.Unmarshal(archive, &src); err != nil {
		return nil, fmt.Errorf("decode the staged archive: %w", err)
	}
	sha := src.SHA
	run := ""
	for _, a := range args {
		if v, ok := strings.CutPrefix(a, "HEIMDALL_RUN="); ok {
			run = v
		}
	}
	c.mu.Lock()
	c.jobs = append(c.jobs, run+"@"+sha)
	at := c.findingAt[sha]
	c.mu.Unlock()
	if strings.HasPrefix(run, "patterns-scan") {
		return patternsOutput(at), nil
	}
	return testEventsOutput(dir, src.Files)
}

// setFinding makes the "patterns" check report a result at "file:line" on the commit.
func (c *flowContainers) setFinding(sha id.SHA, at string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.findingAt[string(sha)] = at
}

// patternsOutput is the wrapper output of the "patterns" check: one SARIF result at "file:line", or none.
func patternsOutput(at string) []byte {
	results := []any{}
	if file, line, ok := strings.Cut(at, ":"); ok {
		n, _ := strconv.Atoi(line)
		results = append(results, map[string]any{"ruleId": "refund-unchecked-amount", "level": "error",
			"message": map[string]string{"text": "the refund amount is not checked against the payment"},
			"locations": []any{map[string]any{"physicalLocation": map[string]any{
				"artifactLocation": map[string]string{"uri": file}, "region": map[string]any{"startLine": n, "endLine": n,
					"snippet": map[string]string{"text": "return amount, nil"}}}}}})
	}
	sarif, _ := json.Marshal(map[string]any{"version": "2.1.0", "runs": []any{map[string]any{
		"tool": map[string]any{"driver": map[string]any{"name": "opengrep"}}, "results": results}}})
	exit := 0
	if len(results) > 0 {
		exit = 1
	}
	return []byte(fmt.Sprintf("\nheimdall-log-begin\nscan done\n\nheimdall-result-begin\n%s\n\nheimdall-result-end\nheimdall-exit=%d\n", sarif, exit))
}

// testEventsOutput runs the tests as the fake product would: a test passes once the commit implements refunds (a file
// holds flowFixMarker), or when its source says it passes anyway; otherwise it fails. The tests are the staged overlay
// files, or the committed tests of the commit when there are no overlays.
func testEventsOutput(dir string, files map[string]string) ([]byte, error) {
	sources, err := stagedTests(dir)
	if err != nil {
		return nil, err
	}
	implemented := false
	for _, c := range files {
		implemented = implemented || strings.Contains(c, flowFixMarker)
	}
	if len(sources) == 0 {
		sources = committedTests(files)
	}
	var out bytes.Buffer
	for _, src := range sources {
		pass := implemented || strings.Contains(src, "passes without the feature")
		for _, name := range testNamesIn(src) {
			writeTestEvents(&out, name, pass)
		}
	}
	return out.Bytes(), nil
}

func committedTests(files map[string]string) []string {
	var out []string
	for p, c := range files {
		if strings.HasPrefix(p, "tests/heimdall/") && strings.HasSuffix(p, "_test.go") {
			out = append(out, c)
		}
	}
	return out
}

// writeTestEvents writes the go test -json events of one test.
func writeTestEvents(out *bytes.Buffer, name string, pass bool) {
	action := "fail"
	if pass {
		action = "pass"
	}
	for _, ev := range []map[string]any{
		{"Action": "run", "Package": "acme/pay/tests", "Test": name},
		{"Action": action, "Package": "acme/pay/tests", "Test": name, "Elapsed": 0.01},
	} {
		line, _ := json.Marshal(ev)
		out.Write(append(line, '\n'))
	}
}

// flowFixMarker is the line of the product file that implements refunds; the developer's push adds it.
const flowFixMarker = "// refunds: implemented"

func stagedTests(dir string) ([]string, error) {
	var sources []string
	err := filepath.WalkDir(filepath.Join(dir, "overlays"), func(p string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return filepath.SkipAll
			}
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, "_test.go") {
			raw, readErr := os.ReadFile(p)
			if readErr != nil {
				return readErr
			}
			sources = append(sources, string(raw))
		}
		return nil
	})
	return sources, err
}

func testNamesIn(src string) []string {
	var names []string
	for _, line := range strings.Split(src, "\n") {
		if rest, ok := strings.CutPrefix(line, "func Test"); ok {
			names = append(names, "Test"+strings.SplitN(rest, "(", 2)[0])
		}
	}
	return names
}
