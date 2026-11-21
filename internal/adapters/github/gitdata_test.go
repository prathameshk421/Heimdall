package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const stateRef = "refs/heimdall/state"

func lastBody(t *testing.T, env *actionEnv, method, path string) map[string]any {
	t.Helper()
	var out map[string]any
	for _, r := range env.f.requests() {
		if r.Method == method && r.Path == path {
			out = nil
			if err := json.Unmarshal(r.Body, &out); err != nil {
				t.Fatalf("body of %s %s: %v", method, path, err)
			}
		}
	}
	if out == nil {
		t.Fatalf("no %s %s request", method, path)
	}
	return out
}

func TestGetRefReadsACustomRef(t *testing.T) {
	env := newActionEnv(t)
	env.f.route("GET", repoRoot+"/git/ref/heimdall/state", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ref": stateRef, "object": map[string]any{"sha": shaHead, "type": "commit"}})
	})
	got, err := env.x.GetRef(context.Background(), testRepo, stateRef)
	if err != nil || string(got) != shaHead {
		t.Fatalf("GetRef = %q, %v; want %s", got, err, shaHead)
	}
	env.wantAuth(t)
}

func TestGetRefMissingIsErrNotFound(t *testing.T) {
	env := newActionEnv(t)
	env.f.route("GET", repoRoot+"/git/ref/heimdall/state", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
	})
	if _, err := env.x.GetRef(context.Background(), testRepo, stateRef); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("error = %v, want ports.ErrNotFound", err)
	}
}

func TestCreateRefSendsTheFullRefAndSHA(t *testing.T) {
	env := newActionEnv(t)
	env.f.route("POST", repoRoot+"/git/refs", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusCreated, map[string]any{}) })
	if err := env.x.CreateRef(context.Background(), testRepo, stateRef, id.SHA(shaHead)); err != nil {
		t.Fatalf("CreateRef: %v", err)
	}
	body := lastBody(t, env, "POST", repoRoot+"/git/refs")
	if body["ref"] != stateRef || body["sha"] != shaHead {
		t.Errorf("body = %v", body)
	}
	env.wantAuth(t)
}

func TestUpdateRefSendsForceAsGivenAndDoesNotClaimCompareAndSwap(t *testing.T) {
	for _, force := range []bool{false, true} {
		env := newActionEnv(t)
		env.f.route("PATCH", repoRoot+"/git/refs/heimdall/state", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{})
		})
		if err := env.x.UpdateRef(context.Background(), testRepo, stateRef, id.SHA(shaNew), force); err != nil {
			t.Fatalf("UpdateRef: %v", err)
		}
		body := lastBody(t, env, "PATCH", repoRoot+"/git/refs/heimdall/state")
		if body["sha"] != shaNew || body["force"] != force {
			t.Errorf("force=%v: body = %v", force, body)
		}
		env.wantAuth(t)
	}
}

func TestRefWritesMapErrors(t *testing.T) {
	cases := []struct {
		status    int
		want      error
		untyped   bool // a 422 stays an ordinary error: the caller reads the ref to tell why
		wantAnErr bool
	}{
		{status: http.StatusForbidden, want: ports.ErrUnauthorized, wantAnErr: true},
		{status: http.StatusInternalServerError, want: ports.ErrUnavailable, wantAnErr: true},
		{status: http.StatusUnprocessableEntity, untyped: true, wantAnErr: true},
	}
	for _, tc := range cases {
		env := newActionEnv(t)
		env.f.route("PATCH", repoRoot+"/git/refs/heimdall/state", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, tc.status, map[string]string{"message": "Update is not a fast forward"})
		})
		err := env.x.UpdateRef(context.Background(), testRepo, stateRef, id.SHA(shaNew), false)
		if err == nil {
			t.Fatalf("status %d: no error", tc.status)
		}
		if tc.untyped {
			for _, sentinel := range []error{ports.ErrNotFound, ports.ErrUnauthorized, ports.ErrUnavailable} {
				if errors.Is(err, sentinel) {
					t.Errorf("status %d: error = %v, must not map to %v", tc.status, err, sentinel)
				}
			}
			if strings.Contains(err.Error(), "fast forward") {
				t.Errorf("error carries the response body: %v", err)
			}
			continue
		}
		if !errors.Is(err, tc.want) {
			t.Errorf("status %d: error = %v, want %v", tc.status, err, tc.want)
		}
	}
}

func TestRefOperationsRefuseBadInputBeforeAnyRequest(t *testing.T) {
	env := newActionEnv(t)
	ctx := context.Background()
	if _, err := env.x.GetRef(ctx, testRepo, "refs/heads/../x"); err == nil {
		t.Error("GetRef accepted a ref that climbs")
	}
	if err := env.x.CreateRef(ctx, testRepo, "heimdall/state", id.SHA(shaHead)); err == nil {
		t.Error("CreateRef accepted a short ref")
	}
	if err := env.x.CreateRef(ctx, testRepo, stateRef, id.SHA("zzz")); err == nil {
		t.Error("CreateRef accepted a bad hash")
	}
	if err := env.x.UpdateRef(ctx, testRepo, stateRef, id.SHA(""), false); err == nil {
		t.Error("UpdateRef accepted an empty hash")
	}
	if n := len(env.f.requests()); n != 0 {
		t.Errorf("%d requests were sent", n)
	}
}

func TestCreateBlobSendsBase64AndReturnsTheHash(t *testing.T) {
	env := newActionEnv(t)
	env.f.route("POST", repoRoot+"/git/blobs", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{"sha": shaNew})
	})
	got, err := env.x.CreateBlob(context.Background(), testRepo, []byte("hello\x00world"))
	if err != nil || string(got) != shaNew {
		t.Fatalf("CreateBlob = %q, %v", got, err)
	}
	body := lastBody(t, env, "POST", repoRoot+"/git/blobs")
	raw, _ := base64.StdEncoding.DecodeString(body["content"].(string))
	if body["encoding"] != "base64" || string(raw) != "hello\x00world" {
		t.Errorf("body = %v", body)
	}
	env.wantAuth(t)
}

func TestCreateBlobRefusesAnOversizedBlob(t *testing.T) {
	env := newActionEnv(t)
	if _, err := env.x.CreateBlob(context.Background(), testRepo, make([]byte, maxBlobBytes+1)); err == nil {
		t.Error("an oversized blob was accepted")
	}
	if n := len(env.f.requests()); n != 0 {
		t.Errorf("%d requests were sent", n)
	}
}

func TestCreateBlobRefusesAReplyWithoutAHash(t *testing.T) {
	env := newActionEnv(t)
	env.f.route("POST", repoRoot+"/git/blobs", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusCreated, map[string]any{}) })
	if _, err := env.x.CreateBlob(context.Background(), testRepo, []byte("x")); err == nil {
		t.Error("a reply with no hash was accepted")
	}
}

func TestCreateTreeSendsEntriesAndBase(t *testing.T) {
	env := newActionEnv(t)
	env.f.route("POST", repoRoot+"/git/trees", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{"sha": shaTree})
	})
	entries := []ports.GitTreeEntry{
		{Path: "changes/pr-1.jsonl", Mode: "100644", SHA: id.SHA(shaNew)},
		{Path: "blobs", Mode: "040000", SHA: id.SHA(shaSub)},
	}
	got, err := env.x.CreateTree(context.Background(), testRepo, id.SHA(shaHead), entries)
	if err != nil || string(got) != shaTree {
		t.Fatalf("CreateTree = %q, %v", got, err)
	}
	body := lastBody(t, env, "POST", repoRoot+"/git/trees")
	tree := body["tree"].([]any)
	if body["base_tree"] != shaHead || len(tree) != 2 {
		t.Fatalf("body = %v", body)
	}
	first, second := tree[0].(map[string]any), tree[1].(map[string]any)
	if first["type"] != "blob" || first["mode"] != "100644" || second["type"] != "tree" || second["sha"] != shaSub {
		t.Errorf("entries = %v", tree)
	}
	env.wantAuth(t)
}

func TestCreateTreeWithoutBaseOmitsBaseTree(t *testing.T) {
	env := newActionEnv(t)
	env.f.route("POST", repoRoot+"/git/trees", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{"sha": shaTree})
	})
	if _, err := env.x.CreateTree(context.Background(), testRepo, "", []ports.GitTreeEntry{{Path: "a", Mode: "100644", SHA: id.SHA(shaNew)}}); err != nil {
		t.Fatal(err)
	}
	if _, has := lastBody(t, env, "POST", repoRoot+"/git/trees")["base_tree"]; has {
		t.Error("base_tree was sent for an empty base")
	}
}

func TestCreateTreeRefusesBadEntries(t *testing.T) {
	env := newActionEnv(t)
	ok := ports.GitTreeEntry{Path: "a", Mode: "100644", SHA: id.SHA(shaNew)}
	bad := map[string][]ports.GitTreeEntry{
		"none":         nil,
		"climbing":     {{Path: "../a", Mode: "100644", SHA: id.SHA(shaNew)}},
		"absolute":     {{Path: "/a", Mode: "100644", SHA: id.SHA(shaNew)}},
		"backslash":    {{Path: `a\b`, Mode: "100644", SHA: id.SHA(shaNew)}},
		"bad mode":     {ok, {Path: "b", Mode: "120000", SHA: id.SHA(shaNew)}},
		"bad hash":     {{Path: "a", Mode: "100644", SHA: id.SHA("nope")}},
		"empty path":   {{Path: "", Mode: "100644", SHA: id.SHA(shaNew)}},
		"dot segments": {{Path: "a/./b", Mode: "100644", SHA: id.SHA(shaNew)}},
	}
	for name, entries := range bad {
		if _, err := env.x.CreateTree(context.Background(), testRepo, "", entries); err == nil {
			t.Errorf("%s: entries were accepted", name)
		}
	}
	if _, err := env.x.CreateTree(context.Background(), testRepo, id.SHA("zz"), []ports.GitTreeEntry{ok}); err == nil {
		t.Error("a bad base hash was accepted")
	}
	if n := len(env.f.requests()); n != 0 {
		t.Errorf("%d requests were sent", n)
	}
}

func TestCreateCommitSendsTreeAndParentsAndMovesNoRef(t *testing.T) {
	env := newActionEnv(t)
	env.f.route("POST", repoRoot+"/git/commits", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{"sha": shaNew})
	})
	got, err := env.x.CreateCommit(context.Background(), testRepo, "state: append", id.SHA(shaTree), []id.SHA{id.SHA(shaHead)})
	if err != nil || string(got) != shaNew {
		t.Fatalf("CreateCommit = %q, %v", got, err)
	}
	body := lastBody(t, env, "POST", repoRoot+"/git/commits")
	parents := body["parents"].([]any)
	if body["message"] != "state: append" || body["tree"] != shaTree || len(parents) != 1 || parents[0] != shaHead {
		t.Errorf("body = %v", body)
	}
	for _, r := range env.f.requests() {
		if strings.Contains(r.Path, "/git/refs") {
			t.Errorf("CreateCommit touched a ref: %s %s", r.Method, r.Path)
		}
	}
	env.wantAuth(t)
}

func TestCreateCommitWithNoParentSendsAnEmptyList(t *testing.T) {
	env := newActionEnv(t)
	env.f.route("POST", repoRoot+"/git/commits", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{"sha": shaNew})
	})
	if _, err := env.x.CreateCommit(context.Background(), testRepo, "root", id.SHA(shaTree), nil); err != nil {
		t.Fatal(err)
	}
	if p, ok := lastBody(t, env, "POST", repoRoot+"/git/commits")["parents"].([]any); !ok || len(p) != 0 {
		t.Errorf("parents = %v, want an empty list", p)
	}
}

func TestCreateCommitRefusesBadInput(t *testing.T) {
	env := newActionEnv(t)
	ctx := context.Background()
	if _, err := env.x.CreateCommit(ctx, testRepo, "  ", id.SHA(shaTree), nil); err == nil {
		t.Error("an empty message was accepted")
	}
	if _, err := env.x.CreateCommit(ctx, testRepo, "m", id.SHA("x"), nil); err == nil {
		t.Error("a bad tree hash was accepted")
	}
	if _, err := env.x.CreateCommit(ctx, testRepo, "m", id.SHA(shaTree), []id.SHA{"bad"}); err == nil {
		t.Error("a bad parent hash was accepted")
	}
	if n := len(env.f.requests()); n != 0 {
		t.Errorf("%d requests were sent", n)
	}
}

func TestGitDataWritesAreRefusedWithoutPermission(t *testing.T) {
	env := newActionEnv(t)
	env.f.route("POST", repoRoot+"/git/blobs", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "Resource not accessible by integration"})
	})
	if _, err := env.x.CreateBlob(context.Background(), testRepo, []byte("x")); !errors.Is(err, ports.ErrUnauthorized) {
		t.Errorf("error = %v, want ports.ErrUnauthorized", err)
	}
}
