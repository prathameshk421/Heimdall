package github

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const repoBranchPath = repoRoot + "/commits/main"

// scriptDefaultBranch answers GET /repos/{owner}/{repo} with a default branch of main.
func scriptDefaultBranch(env *testEnv, branch string) {
	env.f.route("GET", repoRoot, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"default_branch": branch})
	})
}

func TestDefaultHeadReadsTheDefaultBranchHead(t *testing.T) {
	env := newTestEnv(t)
	scriptDefaultBranch(env, "main")
	env.f.route("GET", repoBranchPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != acceptJSON {
			t.Errorf("accept = %q, want %q", r.Header.Get("Accept"), acceptJSON)
		}
		writeJSON(w, http.StatusOK, map[string]any{"sha": shaHead})
	})
	got, err := env.a.DefaultHead(context.Background(), testRepo)
	if err != nil || string(got) != shaHead {
		t.Fatalf("DefaultHead = %q, %v; want %s", got, err, shaHead)
	}
	if n := env.f.count("GET", repoBranchPath); n != 1 {
		t.Errorf("commit requests = %d, want 1", n)
	}
}

func TestDefaultHeadUsesTheReaderTokenNotTheWriter(t *testing.T) {
	env := newTestEnv(t)
	scriptDefaultBranch(env, "main")
	env.f.route("GET", repoBranchPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"sha": shaHead})
	})
	if _, err := env.a.DefaultHead(context.Background(), testRepo); err != nil {
		t.Fatalf("DefaultHead: %v", err)
	}
	bodies := mintBodies(t, env.f)
	if len(bodies) != 1 {
		t.Fatalf("mint requests = %d, want 1 (the reader only)", len(bodies))
	}
	want := map[string]any{"permissions": map[string]any{
		"contents": "read", "pull_requests": "read", "issues": "read", "checks": "read",
	}}
	if !reflect.DeepEqual(bodies[0], want) {
		t.Errorf("token body = %v, want the reader permissions %v", bodies[0], want)
	}
	for _, r := range env.f.requests() {
		if strings.HasPrefix(r.Path, repoRoot) && r.Auth != "Bearer ghs_1" {
			t.Errorf("%s %s used auth %q, want the reader token ghs_1", r.Method, r.Path, r.Auth)
		}
	}
}

func TestDefaultHeadOnAnEmptyRepositoryIsErrNotFound(t *testing.T) {
	for _, status := range []int{http.StatusConflict, http.StatusNotFound} {
		env := newTestEnv(t)
		scriptDefaultBranch(env, "main")
		env.f.route("GET", repoBranchPath, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, status, map[string]string{"message": "Git Repository is empty."})
		})
		_, err := env.a.DefaultHead(context.Background(), testRepo)
		if !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("status %d: error = %v, want ports.ErrNotFound", status, err)
		}
	}
}

func TestConflictOnOtherCallsStaysAnOrdinaryError(t *testing.T) {
	env := newTestEnv(t)
	env.f.route("GET", "/repos/aarush323/heimdall-fixture-pay/pulls/409", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusConflict, map[string]string{"message": "conflict"})
	})
	_, err := env.a.PR(context.Background(), testRepo, 409)
	if err == nil {
		t.Fatal("a 409 on a pull request read was accepted")
	}
	if errors.Is(err, ports.ErrNotFound) {
		t.Errorf("a 409 on a read mapped to ErrNotFound: %v", err)
	}
}

func TestDefaultHeadWithNoDefaultBranchIsErrNotFound(t *testing.T) {
	env := newTestEnv(t)
	scriptDefaultBranch(env, "")
	_, err := env.a.DefaultHead(context.Background(), testRepo)
	if !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("error = %v, want ports.ErrNotFound", err)
	}
	if n := env.f.count("GET", repoBranchPath); n != 0 {
		t.Errorf("commit requests = %d, want 0 when the branch name is empty", n)
	}
}

func TestDefaultHeadUnauthorizedIsErrUnauthorized(t *testing.T) {
	env := newTestEnv(t)
	env.f.route("GET", repoRoot, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
	})
	_, err := env.a.DefaultHead(context.Background(), testRepo)
	if !errors.Is(err, ports.ErrUnauthorized) {
		t.Errorf("error = %v, want ports.ErrUnauthorized", err)
	}
}

func TestDefaultHeadRefusesAMalformedSHA(t *testing.T) {
	for _, bad := range []string{"not-a-sha", "ABCDEF1234", "../../etc", "9f1c2ab\n"} {
		env := newTestEnv(t)
		scriptDefaultBranch(env, "main")
		env.f.route("GET", repoBranchPath, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"sha": bad})
		})
		got, err := env.a.DefaultHead(context.Background(), testRepo)
		if err == nil {
			t.Errorf("sha %q was accepted as %q", bad, got)
		}
		if errors.Is(err, ports.ErrNotFound) {
			t.Errorf("sha %q gave ErrNotFound; a malformed reply is not a missing branch", bad)
		}
	}
}

func TestDefaultHeadRefusesABranchNameThatClimbsOut(t *testing.T) {
	env := newTestEnv(t)
	scriptDefaultBranch(env, "../secrets")
	if _, err := env.a.DefaultHead(context.Background(), testRepo); err == nil {
		t.Fatal("a default branch with .. was accepted")
	}
	if n := env.f.count("GET", "/repos/aarush323/heimdall-fixture-pay/commits/../secrets"); n != 0 {
		t.Errorf("commit requests = %d, want 0", n)
	}
}

func TestDefaultHeadErrorCarriesNoToken(t *testing.T) {
	env := newTestEnv(t)
	scriptDefaultBranch(env, "main")
	env.f.route("GET", repoBranchPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "ghs_1 is bad"})
	})
	_, err := env.a.DefaultHead(context.Background(), testRepo)
	if err == nil {
		t.Fatal("a 401 was accepted")
	}
	if strings.Contains(err.Error(), "ghs_") {
		t.Errorf("error text carries a token: %v", err)
	}
}

// A repository with a configured base branch is cut from that branch, not from the repository's default branch, and
// the repository itself is not read to find a default (the owner's policy and approvers live on the base branch).
func TestDefaultHeadReadsTheConfiguredBaseBranch(t *testing.T) {
	env := newTestEnv(t, func(c *Config) { c.BaseBranches = map[change.Repo]string{testRepo: "heimdall-test/demo-base"} })
	env.f.route("GET", repoRoot+"/commits/heimdall-test/demo-base", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"sha": shaHead})
	})
	got, err := env.a.DefaultHead(context.Background(), testRepo)
	if err != nil || string(got) != shaHead {
		t.Fatalf("DefaultHead = %q, %v; want %s", got, err, shaHead)
	}
	if n := env.f.count("GET", repoRoot); n != 0 {
		t.Errorf("repository reads = %d, want 0 when a base branch is configured", n)
	}
}
