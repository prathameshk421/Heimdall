package github

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// installationRoute scripts GET /app/installations/42. It verifies the App JWT as GitHub does, then answers body.
func installationRoute(env *testEnv, body any) {
	env.f.route("GET", "/app/installations/42", func(w http.ResponseWriter, r *http.Request) {
		if err := verifyJWT(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), &env.key.PublicKey, env.clock.now); err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
			return
		}
		writeJSON(w, http.StatusOK, body)
	})
}

func TestInstallationReadsPermissionsAndAllRepositories(t *testing.T) {
	env := newTestEnv(t)
	installationRoute(env, map[string]any{
		"permissions":          map[string]string{"checks": "write", "contents": "write", "metadata": "read"},
		"repository_selection": "all",
	})
	got, err := env.a.Tokens().Installation(context.Background())
	if err != nil {
		t.Fatalf("Installation: %v", err)
	}
	want := Installation{
		Permissions:  map[string]string{"checks": "write", "contents": "write", "metadata": "read"},
		Repositories: Repositories{All: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Installation = %+v, want %+v", got, want)
	}
}

func TestInstallationListsSelectedRepositoriesOnEveryPage(t *testing.T) {
	env := newTestEnv(t)
	installationRoute(env, map[string]any{
		"permissions":          map[string]string{"contents": "write"},
		"repository_selection": "selected",
	})
	next := env.f.srv.URL + "/installation/repositories?page=2"
	env.f.route("GET", "/installation/repositories", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ghs_") {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "want an installation token"})
			return
		}
		if r.URL.Query().Get("page") == "2" {
			writeJSON(w, http.StatusOK, map[string]any{"repositories": []any{map[string]string{"full_name": "aarush323/b"}}})
			return
		}
		w.Header().Set("Link", `<`+next+`>; rel="next"`)
		writeJSON(w, http.StatusOK, map[string]any{"repositories": []any{map[string]string{"full_name": "aarush323/a"}}})
	})
	got, err := env.a.Tokens().Installation(context.Background())
	if err != nil {
		t.Fatalf("Installation: %v", err)
	}
	want := Repositories{All: false, Names: []string{"aarush323/a", "aarush323/b"}}
	if !reflect.DeepEqual(got.Repositories, want) {
		t.Errorf("Repositories = %+v, want %+v", got.Repositories, want)
	}
}

func TestInstallationRefusesAnUnknownPermissionLevel(t *testing.T) {
	env := newTestEnv(t)
	installationRoute(env, map[string]any{
		"permissions":          map[string]string{"contents": "maintain"},
		"repository_selection": "all",
	})
	if _, err := env.a.Tokens().Installation(context.Background()); err == nil {
		t.Fatal("an unknown level was accepted")
	}
}

func TestInstallationRefusesAnUnknownRepositorySelection(t *testing.T) {
	env := newTestEnv(t)
	installationRoute(env, map[string]any{
		"permissions":          map[string]string{"contents": "write"},
		"repository_selection": "everything",
	})
	if _, err := env.a.Tokens().Installation(context.Background()); err == nil {
		t.Fatal("an unknown repository selection was accepted")
	}
}

func TestInstallationErrorCarriesNoBodyOrKey(t *testing.T) {
	env := newTestEnv(t)
	env.f.route("GET", "/app/installations/42", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "ghs_LEAKSECRET123 Bad credentials"})
	})
	_, err := env.a.Tokens().Installation(context.Background())
	if !errors.Is(err, ports.ErrUnauthorized) {
		t.Fatalf("error = %v, want ports.ErrUnauthorized", err)
	}
	if strings.Contains(err.Error(), "ghs_LEAKSECRET123") || strings.Contains(err.Error(), "PRIVATE KEY") {
		t.Errorf("error text carries a secret: %v", err)
	}
}

func TestRequiredPermissionsAreTheFiveAgreedOnes(t *testing.T) {
	want := map[string]string{
		"checks": "write", "contents": "write", "pull_requests": "write", "issues": "write", "metadata": "read",
	}
	if got := RequiredPermissions(); !reflect.DeepEqual(got, want) {
		t.Errorf("RequiredPermissions = %v, want %v", got, want)
	}
}

func TestRequiredPermissionsCoverEveryOperationAtItsLevel(t *testing.T) {
	need := RequiredPermissions()
	grants := []map[string]string{
		{"contents": "read", "pull_requests": "read", "issues": "read", "checks": "read"}, // the reader token
	}
	for _, op := range []Op{OpCheckWrite, OpBranchWrite, OpCommentWrite} {
		perms, ok := writerPermissions(op)
		if !ok {
			t.Fatalf("writerPermissions(%s) is unknown", op)
		}
		grants = append(grants, perms)
	}
	for _, g := range grants {
		for name, level := range g {
			have, ok := need[name]
			if !ok {
				t.Errorf("a token grants %s:%s but RequiredPermissions lacks %s", name, level, name)
				continue
			}
			if levelRank(level) > levelRank(have) {
				t.Errorf("a token grants %s:%s, more than the product needs (%s)", name, level, have)
			}
		}
	}
}
