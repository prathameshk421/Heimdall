package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const (
	repoRoot = "/repos/aarush323/heimdall-fixture-pay"
	shaHead  = "1111111111111111111111111111111111111111"
	shaTree  = "3333333333333333333333333333333333333333"
	shaSub   = "4444444444444444444444444444444444444444"
	shaDeep  = "5555555555555555555555555555555555555555"
	shaNew   = "6666666666666666666666666666666666666666"
)

func TestReadFileReturnsRawContentAtTheSha(t *testing.T) {
	env := newTestEnv(t)
	env.f.route("GET", repoRoot+"/contents/dir/heimdall.yaml", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("ref") != shaHead || r.Header.Get("Accept") != acceptRaw {
			t.Errorf("ref %q accept %q", r.URL.Query().Get("ref"), r.Header.Get("Accept"))
		}
		_, _ = w.Write([]byte("version: 1\n"))
	})
	got, err := env.a.ReadFile(context.Background(), testRepo, shaHead, "dir/heimdall.yaml")
	if err != nil || string(got) != "version: 1\n" {
		t.Fatalf("ReadFile = %q, %v", got, err)
	}
}

func TestReadFileMissingIsErrNotFound(t *testing.T) {
	env := newTestEnv(t)
	env.f.route("GET", repoRoot+"/contents/heimdall.yaml", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
	})
	_, err := env.a.ReadFile(context.Background(), testRepo, shaHead, "heimdall.yaml")
	if !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("error = %v, want ports.ErrNotFound", err)
	}
}

func TestReadFileRefusesAPathThatClimbsOut(t *testing.T) {
	env := newTestEnv(t)
	if _, err := env.a.ReadFile(context.Background(), testRepo, shaHead, "../secrets"); err == nil {
		t.Fatal("a path with .. was accepted")
	}
	if n := len(env.f.requests()); n != 0 {
		t.Errorf("%d requests were sent for a refused path", n)
	}
}

func scriptTrees(env *testEnv) {
	env.f.route("GET", repoRoot+"/git/commits/"+shaHead, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"tree": map[string]any{"sha": shaTree}})
	})
	env.f.route("GET", repoRoot+"/git/trees/"+shaTree, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"tree": []any{
			map[string]any{"path": "README.md", "type": "blob", "sha": shaNew},
			map[string]any{"path": "tests", "type": "tree", "sha": shaSub},
		}})
	})
	env.f.route("GET", repoRoot+"/git/trees/"+shaSub, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"tree": []any{
			map[string]any{"path": "heimdall", "type": "tree", "sha": shaDeep},
		}})
	})
}

func TestTreeHashWalksToTheDirectory(t *testing.T) {
	env := newTestEnv(t)
	scriptTrees(env)
	got, err := env.a.TreeHash(context.Background(), testRepo, shaHead, "tests/heimdall")
	if err != nil || string(got) != shaDeep {
		t.Fatalf("TreeHash = %q, %v; want %s", got, err, shaDeep)
	}
	root, err := env.a.TreeHash(context.Background(), testRepo, shaHead, "")
	if err != nil || string(root) != shaTree {
		t.Fatalf("root TreeHash = %q, %v; want %s", root, err, shaTree)
	}
}

func TestTreeHashMissingDirectoryIsErrNotFound(t *testing.T) {
	env := newTestEnv(t)
	scriptTrees(env)
	_, err := env.a.TreeHash(context.Background(), testRepo, shaHead, "tests/nope")
	if !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("error = %v, want ports.ErrNotFound", err)
	}
	// A blob is not a directory.
	_, err = env.a.TreeHash(context.Background(), testRepo, shaHead, "README.md")
	if !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("blob error = %v, want ports.ErrNotFound", err)
	}
}

func TestTreeHashRefusesAPathThatClimbsOut(t *testing.T) {
	env := newTestEnv(t)
	for _, dir := range []string{"../x", "a/../b", "/abs"} {
		if _, err := env.a.TreeHash(context.Background(), testRepo, shaHead, dir); err == nil {
			t.Errorf("dir %q was accepted", dir)
		}
	}
}

func TestTarballStreamsTheBody(t *testing.T) {
	env := newTestEnv(t)
	env.f.route("GET", repoRoot+"/tarball/"+shaHead, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("tar-bytes"))
	})
	rc, err := env.a.Tarball(context.Background(), testRepo, shaHead)
	if err != nil {
		t.Fatalf("Tarball: %v", err)
	}
	defer func() { _ = rc.Close() }()
	got, _ := io.ReadAll(rc)
	if string(got) != "tar-bytes" {
		t.Errorf("body = %q", got)
	}
}

func TestEnsureBranchDoesNothingWhenItExists(t *testing.T) {
	env := newTestEnv(t)
	env.f.route("GET", repoRoot+"/git/ref/heads/heimdall-test/run1", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"object": map[string]any{"sha": shaHead}})
	})
	if err := env.a.EnsureBranch(context.Background(), testRepo, shaHead, "heimdall-test/run1"); err != nil {
		t.Fatalf("EnsureBranch: %v", err)
	}
	if env.f.count("POST", repoRoot+"/git/refs") != 0 {
		t.Error("an existing branch was created again")
	}
}

func TestEnsureBranchCreatesItFromTheSha(t *testing.T) {
	env := newTestEnv(t)
	env.f.route("GET", repoRoot+"/git/ref/heads/heimdall-test/run1", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
	})
	env.f.route("POST", repoRoot+"/git/refs", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{})
	})
	if err := env.a.EnsureBranch(context.Background(), testRepo, shaHead, "heimdall-test/run1"); err != nil {
		t.Fatalf("EnsureBranch: %v", err)
	}
	var body map[string]string
	for _, r := range env.f.requests() {
		if r.Method == "POST" && r.Path == repoRoot+"/git/refs" {
			if err := json.Unmarshal(r.Body, &body); err != nil {
				t.Fatalf("body: %v", err)
			}
		}
	}
	if body["ref"] != "refs/heads/heimdall-test/run1" || body["sha"] != shaHead {
		t.Errorf("create body = %v", body)
	}
}

func TestEnsureBranchRefusesAnUnsafeName(t *testing.T) {
	env := newTestEnv(t)
	for _, name := range []string{"", "a..b", "-x", "a b", "a\nb"} {
		if err := env.a.EnsureBranch(context.Background(), testRepo, shaHead, name); err == nil {
			t.Errorf("branch %q was accepted", name)
		}
	}
	if n := len(env.f.requests()); n != 0 {
		t.Errorf("%d requests for refused names", n)
	}
}

func TestCommitFilesMakesOneCommitAndMovesTheBranch(t *testing.T) {
	env := newTestEnv(t)
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
	got, err := env.a.CommitFiles(context.Background(), testRepo, "tests-branch", "add tests",
		[]ports.File{{Path: "tests/heimdall/PAY-1/a_test.go", Content: []byte("package x\n")}})
	if err != nil || string(got) != shaNew {
		t.Fatalf("CommitFiles = %q, %v; want %s", got, err, shaNew)
	}
	assertCommitBodies(t, env)
}

func assertCommitBodies(t *testing.T, env *testEnv) {
	t.Helper()
	bodies := map[string]map[string]any{}
	for _, r := range env.f.requests() {
		var m map[string]any
		if len(r.Body) > 0 && json.Unmarshal(r.Body, &m) == nil {
			bodies[r.Method+" "+r.Path] = m
		}
	}
	blob := bodies["POST "+repoRoot+"/git/blobs"]
	if blob["encoding"] != "base64" || blob["content"] != base64.StdEncoding.EncodeToString([]byte("package x\n")) {
		t.Errorf("blob body = %v", blob)
	}
	tree := bodies["POST "+repoRoot+"/git/trees"]
	if tree["base_tree"] != shaTree {
		t.Errorf("tree base = %v, want %s", tree["base_tree"], shaTree)
	}
	commit := bodies["POST "+repoRoot+"/git/commits"]
	parents, _ := commit["parents"].([]any)
	if commit["tree"] != shaDeep || commit["message"] != "add tests" || len(parents) != 1 || parents[0] != shaHead {
		t.Errorf("commit body = %v", commit)
	}
	if bodies["PATCH "+repoRoot+"/git/refs/heads/tests-branch"]["sha"] != shaNew {
		t.Errorf("ref was not moved to the new commit")
	}
}

func TestCommitFilesRefusesEmptyAndUnsafeInput(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	if _, err := env.a.CommitFiles(ctx, testRepo, "b", "m", nil); err == nil {
		t.Error("no files was accepted")
	}
	if _, err := env.a.CommitFiles(ctx, testRepo, "b", "", []ports.File{{Path: "a.go"}}); err == nil {
		t.Error("an empty message was accepted")
	}
	if _, err := env.a.CommitFiles(ctx, testRepo, "b", "m", []ports.File{{Path: "../a.go"}}); err == nil {
		t.Error("a path with .. was accepted")
	}
	if n := len(env.f.requests()); n != 0 {
		t.Errorf("%d requests for refused input", n)
	}
}

func decodeBody(t *testing.T, env *testEnv, method, path string) map[string]any {
	t.Helper()
	for _, r := range env.f.requests() {
		if r.Method == method && r.Path == path {
			var m map[string]any
			if err := json.Unmarshal(r.Body, &m); err != nil {
				t.Fatalf("body of %s %s: %v", method, path, err)
			}
			return m
		}
	}
	t.Fatalf("no %s %s request", method, path)
	return nil
}

func TestSetCheckCreatesAPendingRun(t *testing.T) {
	env := newTestEnv(t)
	env.f.route("GET", repoRoot+"/commits/"+shaHead+"/check-runs", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"check_runs": []any{}})
	})
	env.f.route("POST", repoRoot+"/check-runs", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{"id": 77})
	})
	err := env.a.SetCheck(context.Background(), testRepo, shaHead, ports.Check{
		Name: "heimdall/gate", Status: ports.CheckInProgress, ExternalID: "c1", Summary: "waiting",
	})
	if err != nil {
		t.Fatalf("SetCheck: %v", err)
	}
	body := decodeBody(t, env, "POST", repoRoot+"/check-runs")
	if body["name"] != "heimdall/gate" || body["head_sha"] != shaHead || body["status"] != "in_progress" || body["external_id"] != "c1" {
		t.Errorf("create body = %v", body)
	}
	if _, has := body["conclusion"]; has {
		t.Errorf("a pending run carried a conclusion: %v", body)
	}
}

func TestSetCheckUpdatesTheRunWithTheSameExternalID(t *testing.T) {
	env := newTestEnv(t)
	env.f.route("GET", repoRoot+"/commits/"+shaHead+"/check-runs", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"check_runs": []any{
			map[string]any{"id": 5, "external_id": "other"},
			map[string]any{"id": 77, "external_id": "c1"},
		}})
	})
	env.f.route("PATCH", repoRoot+"/check-runs/77", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"id": 77})
	})
	err := env.a.SetCheck(context.Background(), testRepo, shaHead, ports.Check{
		Name: "heimdall/gate", Status: ports.CheckCompleted, Conclusion: change.CIFailure, ExternalID: "c1", Summary: "slop failed",
	})
	if err != nil {
		t.Fatalf("SetCheck: %v", err)
	}
	if env.f.count("POST", repoRoot+"/check-runs") != 0 {
		t.Error("an existing run was created again")
	}
	body := decodeBody(t, env, "PATCH", repoRoot+"/check-runs/77")
	if body["status"] != "completed" || body["conclusion"] != "failure" {
		t.Errorf("update body = %v", body)
	}
}

func TestSetCheckRefusesAnInvalidCheck(t *testing.T) {
	env := newTestEnv(t)
	err := env.a.SetCheck(context.Background(), testRepo, shaHead, ports.Check{Name: "g", Status: ports.CheckCompleted})
	if err == nil {
		t.Fatal("a completed check without a conclusion was accepted")
	}
	if n := len(env.f.requests()); n != 0 {
		t.Errorf("%d requests for an invalid check", n)
	}
}

const commentsPath = repoRoot + "/issues/9/comments"

func TestCommentPostsWhenNoMarkerExists(t *testing.T) {
	env := newTestEnv(t)
	env.f.route("GET", commentsPath, func(w http.ResponseWriter, _ *http.Request) {
		// A person quoting the marker must not be edited.
		writeJSON(w, http.StatusOK, []any{map[string]any{
			"id": 4, "body": "see <!-- heimdall:effect=eff-1 -->", "user": map[string]any{"login": "bob", "type": "User"},
		}})
	})
	env.f.route("POST", commentsPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{"id": 8})
	})
	if err := env.a.Comment(context.Background(), testRepo, 9, change.EffectKey("eff-1"), "hello"); err != nil {
		t.Fatalf("Comment: %v", err)
	}
	body := decodeBody(t, env, "POST", commentsPath)
	text, _ := body["body"].(string)
	if !strings.HasPrefix(text, "hello") || !strings.Contains(text, "<!-- heimdall:effect=eff-1 -->") {
		t.Errorf("posted body = %q", text)
	}
}

func TestCommentEditsTheBotCommentWithTheSameKey(t *testing.T) {
	env := newTestEnv(t)
	env.f.route("GET", commentsPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, []any{map[string]any{
			"id": 4, "body": "old\n<!-- heimdall:effect=eff-1 -->", "user": map[string]any{"login": "heimdall[bot]", "type": "Bot"},
		}})
	})
	env.f.route("PATCH", repoRoot+"/issues/comments/4", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"id": 4})
	})
	if err := env.a.Comment(context.Background(), testRepo, 9, change.EffectKey("eff-1"), "new"); err != nil {
		t.Fatalf("Comment: %v", err)
	}
	if env.f.count("POST", commentsPath) != 0 {
		t.Error("a retry added a second comment")
	}
	body := decodeBody(t, env, "PATCH", repoRoot+"/issues/comments/4")
	if text, _ := body["body"].(string); !strings.HasPrefix(text, "new") {
		t.Errorf("edited body = %q", text)
	}
}

func TestCommentRefusesAKeyThatCouldBreakTheMarker(t *testing.T) {
	env := newTestEnv(t)
	for _, key := range []string{"", "a-->b", "a\nb"} {
		if err := env.a.Comment(context.Background(), testRepo, 9, change.EffectKey(key), "x"); err == nil {
			t.Errorf("key %q was accepted", key)
		}
	}
}
