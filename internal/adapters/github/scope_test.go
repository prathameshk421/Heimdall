package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// scope is what GitHub granted one installation token: its permissions and, when narrowed, its repositories.
type scope struct {
	perms map[string]string
	repos []string // nil means every repository
}

// scopeBook holds the scope of each token the scoped fake issued. It enforces the scope on every API call, the way
// GitHub does, so a call outside the scope gets 403.
type scopeBook struct {
	mu      sync.Mutex
	byToken map[string]scope
}

// installScopedGitHub replaces the token route with one that records each token's scope, and turns on enforcement.
func installScopedGitHub(t *testing.T, env *testEnv) {
	t.Helper()
	book := &scopeBook{byToken: map[string]scope{}}
	issued := 0
	env.f.route("POST", "/app/installations/42/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		if err := verifyJWT(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), &env.key.PublicKey, env.clock.now); err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
			return
		}
		var body struct {
			Permissions  map[string]string `json:"permissions"`
			Repositories []string          `json:"repositories"`
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil || json.Unmarshal(raw, &body) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "bad token request"})
			return
		}
		issued++
		tok := fmt.Sprintf("ghs_%d", issued)
		book.mu.Lock()
		book.byToken[tok] = scope{perms: body.Permissions, repos: body.Repositories}
		book.mu.Unlock()
		writeJSON(w, http.StatusCreated, map[string]any{
			"token":      tok,
			"expires_at": env.clock.now.Add(time.Hour).UTC().Format(time.RFC3339),
		})
	})
	env.f.gate = book.gate
}

// gate refuses an installation token whose scope does not allow the call. App JWT calls pass; they are not
// operations on the repository.
func (b *scopeBook) gate(r *http.Request) int {
	auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !strings.HasPrefix(auth, "ghs_") {
		return 0
	}
	b.mu.Lock()
	sc, ok := b.byToken[auth]
	b.mu.Unlock()
	if !ok {
		return http.StatusUnauthorized
	}
	perm, need := requiredFor(r.Method, r.URL.Path)
	if perm == "" {
		return http.StatusInternalServerError // an endpoint the test model does not know: fail loudly
	}
	if sc.repos != nil && !contains(sc.repos, repoOf(r.URL.Path)) {
		return http.StatusForbidden
	}
	if levelRank(sc.perms[perm]) < levelRank(need) {
		return http.StatusForbidden
	}
	return 0
}

// requiredFor names the permission an endpoint needs and the level. Reads need read; every other call needs write.
func requiredFor(method, path string) (string, string) {
	need := "write"
	if method == http.MethodGet {
		need = "read"
	}
	switch {
	case strings.Contains(path, "/check-runs"):
		return "checks", need
	case strings.Contains(path, "/git/"):
		return "contents", need
	case strings.Contains(path, "/issues/"):
		return "issues", need
	}
	return "", ""
}

// repoOf returns the repository name in a /repos/owner/name/... path.
func repoOf(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) < 4 {
		return ""
	}
	return parts[3]
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// scriptWriteEndpoints answers the endpoints the matrix test calls. Each route answers only when the gate lets the
// call through.
func scriptWriteEndpoints(env *testEnv) {
	env.f.route("POST", repoRoot+"/check-runs", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{"id": 1})
	})
	env.f.route("POST", repoRoot+"/git/refs", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{})
	})
	env.f.route("POST", repoRoot+"/issues/42/comments", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{"id": 1})
	})
	env.f.route("GET", repoRoot+"/git/ref/heads/heimdall-test/run1", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"object": map[string]any{"sha": shaHead}})
	})
}

func TestEachWriteOperationRunsOnItsOwnNarrowedToken(t *testing.T) {
	env := newTestEnv(t)
	installScopedGitHub(t, env)
	ctx := context.Background()

	env.f.route("GET", repoRoot+"/commits/"+shaHead+"/check-runs", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"check_runs": []any{}})
	})
	env.f.route("POST", repoRoot+"/check-runs", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{"id": 1})
	})
	if err := env.a.SetCheck(ctx, testRepo, shaHead, ports.Check{
		Name: "heimdall/gate", Status: ports.CheckInProgress, ExternalID: "e1", Summary: "pending",
	}); err != nil {
		t.Errorf("SetCheck on the checks token: %v", err)
	}

	env.f.route("GET", repoRoot+"/git/ref/heads/heimdall-test/run1", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
	})
	env.f.route("POST", repoRoot+"/git/refs", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{})
	})
	if err := env.a.EnsureBranch(ctx, testRepo, shaHead, "heimdall-test/run1"); err != nil {
		t.Errorf("EnsureBranch on the branch token: %v", err)
	}

	env.f.route("GET", repoRoot+"/issues/42/comments", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, []any{})
	})
	env.f.route("POST", repoRoot+"/issues/42/comments", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{"id": 1})
	})
	if err := env.a.Comment(ctx, testRepo, 42, "k1", "hello"); err != nil {
		t.Errorf("Comment on the comment token: %v", err)
	}

	env.f.route("GET", repoRoot+"/git/ref/heads/tests-branch", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"object": map[string]any{"sha": shaHead}})
	})
	env.f.route("GET", repoRoot+"/git/commits/"+shaHead, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"tree": map[string]any{"sha": shaTree}})
	})
	env.f.route("POST", repoRoot+"/git/blobs", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{"sha": shaSub})
	})
	env.f.route("POST", repoRoot+"/git/trees", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{"sha": shaDeep})
	})
	env.f.route("POST", repoRoot+"/git/commits", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{"sha": shaNew})
	})
	env.f.route("PATCH", repoRoot+"/git/refs/heads/tests-branch", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{})
	})
	if _, err := env.a.CommitFiles(ctx, testRepo, "tests-branch", "add tests",
		[]ports.File{{Path: "tests/heimdall/PAY-1/a_test.go", Content: []byte("package x\n")}}); err != nil {
		t.Errorf("CommitFiles on the branch token: %v", err)
	}
}

func TestNarrowedTokenIsRefusedForEveryOtherOperation(t *testing.T) {
	env := newTestEnv(t)
	installScopedGitHub(t, env)
	scriptWriteEndpoints(env)
	ctx := context.Background()

	checkTok, err := env.a.Tokens().Writer(ctx, OpCheckWrite, testRepo)
	if err != nil {
		t.Fatalf("checks token: %v", err)
	}
	branchTok, err := env.a.Tokens().Writer(ctx, OpBranchWrite, testRepo)
	if err != nil {
		t.Fatalf("branch token: %v", err)
	}
	commentTok, err := env.a.Tokens().Writer(ctx, OpCommentWrite, testRepo)
	if err != nil {
		t.Fatalf("comment token: %v", err)
	}
	readerTok, err := env.a.Tokens().Reader(ctx)
	if err != nil {
		t.Fatalf("reader token: %v", err)
	}
	otherTok, err := env.a.Tokens().Writer(ctx, OpCheckWrite, change.Repo("aarush323/other-repo"))
	if err != nil {
		t.Fatalf("checks token for another repository: %v", err)
	}

	endpoints := []struct {
		name, method, path string
	}{
		{"check run", http.MethodPost, repoRoot + "/check-runs"},
		{"git ref create", http.MethodPost, repoRoot + "/git/refs"},
		{"issue comment", http.MethodPost, repoRoot + "/issues/42/comments"},
		{"git ref read", http.MethodGet, repoRoot + "/git/ref/heads/heimdall-test/run1"},
	}
	cases := []struct {
		token string
		tok   string
		allow map[string]bool // endpoint name to whether the token may call it
	}{
		{"checks", checkTok, map[string]bool{"check run": true, "git ref create": false, "issue comment": false, "git ref read": false}},
		{"branch", branchTok, map[string]bool{"check run": false, "git ref create": true, "issue comment": false, "git ref read": true}},
		{"comment", commentTok, map[string]bool{"check run": false, "git ref create": false, "issue comment": true, "git ref read": false}},
		{"reader", readerTok, map[string]bool{"check run": false, "git ref create": false, "issue comment": false, "git ref read": true}},
		{"checks for another repo", otherTok, map[string]bool{"check run": false, "git ref create": false, "issue comment": false, "git ref read": false}},
	}
	for _, c := range cases {
		for _, e := range endpoints {
			_, err := env.a.c.call(ctx, request{method: e.method, path: e.path, auth: c.tok, body: []byte("{}")})
			want := c.allow[e.name]
			switch {
			case want && err != nil:
				t.Errorf("%s token refused for %s: %v", c.token, e.name, err)
			case !want && !errors.Is(err, ports.ErrUnauthorized):
				t.Errorf("%s token for %s: err = %v, want ports.ErrUnauthorized", c.token, e.name, err)
			}
		}
	}
}
