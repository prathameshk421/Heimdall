package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// mintBodies returns the decoded body of every token request the fake received.
func mintBodies(t *testing.T, f *fakeGitHub) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, r := range f.requests() {
		if r.Method != "POST" || r.Path != "/app/installations/42/access_tokens" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(r.Body, &m); err != nil {
			t.Fatalf("token body is not JSON: %v", err)
		}
		out = append(out, m)
	}
	return out
}

func TestWriterRequestIsNarrowedToOnePermissionAndOneRepo(t *testing.T) {
	env := newTestEnv(t)
	cases := []struct {
		op    Op
		perms map[string]any
	}{
		{OpCheckWrite, map[string]any{"checks": "write"}},
		{OpBranchWrite, map[string]any{"contents": "write"}},
		{OpCommentWrite, map[string]any{"pull_requests": "write", "issues": "write"}},
	}
	for _, tc := range cases {
		if _, err := env.a.Tokens().Writer(context.Background(), tc.op, testRepo); err != nil {
			t.Fatalf("Writer(%s): %v", tc.op, err)
		}
	}
	bodies := mintBodies(t, env.f)
	if len(bodies) != len(cases) {
		t.Fatalf("mint requests = %d, want %d", len(bodies), len(cases))
	}
	for i, tc := range cases {
		want := map[string]any{"permissions": tc.perms, "repositories": []any{"heimdall-fixture-pay"}}
		if !reflect.DeepEqual(bodies[i], want) {
			t.Errorf("op %s body = %v, want %v", tc.op, bodies[i], want)
		}
	}
}

func TestReaderIsReadOnlyAndNotNarrowedToRepositories(t *testing.T) {
	env := newTestEnv(t)
	if _, err := env.a.Tokens().Reader(context.Background()); err != nil {
		t.Fatalf("Reader: %v", err)
	}
	bodies := mintBodies(t, env.f)
	if len(bodies) != 1 {
		t.Fatalf("mint requests = %d, want 1", len(bodies))
	}
	want := map[string]any{"permissions": map[string]any{
		"contents": "read", "pull_requests": "read", "issues": "read", "checks": "read",
	}}
	if !reflect.DeepEqual(bodies[0], want) {
		t.Errorf("reader body = %v, want %v", bodies[0], want)
	}
}

func TestUnknownOpIsRefusedWithoutAnyRequest(t *testing.T) {
	env := newTestEnv(t)
	if _, err := env.a.Tokens().Writer(context.Background(), Op("admin"), testRepo); err == nil {
		t.Fatal("unknown op was accepted")
	}
	if n := len(env.f.requests()); n != 0 {
		t.Errorf("requests = %d, want 0", n)
	}
}

func TestTokenCacheHitMissAndExpiryWithFakeClock(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	ts := env.a.Tokens()

	first, _ := ts.Writer(ctx, OpCheckWrite, testRepo)
	second, _ := ts.Writer(ctx, OpCheckWrite, testRepo)
	if first != second || *env.mints != 1 {
		t.Fatalf("hit: tokens %q %q, mints %d; want one mint and the same token", first, second, *env.mints)
	}

	env.clock.advance(49 * time.Minute)
	if again, _ := ts.Writer(ctx, OpCheckWrite, testRepo); again != first || *env.mints != 1 {
		t.Fatalf("before 50 minutes: token %q, mints %d; want the cached token", again, *env.mints)
	}

	env.clock.advance(2 * time.Minute)
	fresh, err := ts.Writer(ctx, OpCheckWrite, testRepo)
	if err != nil {
		t.Fatalf("after expiry: %v", err)
	}
	if fresh == first || *env.mints != 2 {
		t.Fatalf("after 51 minutes: token %q (old %q), mints %d; want a new mint", fresh, first, *env.mints)
	}
}

func TestCacheIsKeyedByOperationAndRepository(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	ts := env.a.Tokens()
	_, _ = ts.Writer(ctx, OpCheckWrite, testRepo)
	_, _ = ts.Writer(ctx, OpBranchWrite, testRepo)
	_, _ = ts.Writer(ctx, OpCheckWrite, change.Repo("aarush323/other-repo"))
	if *env.mints != 3 {
		t.Errorf("mints = %d, want 3 (one per op and repo)", *env.mints)
	}
}

func TestTokenNeverAppearsInErrorText(t *testing.T) {
	env := newTestEnv(t)
	env.f.route("POST", "/app/installations/42/access_tokens", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "ghs_LEAKSECRET123 Bad credentials"})
	})
	_, err := env.a.Tokens().Writer(context.Background(), OpCheckWrite, testRepo)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, ports.ErrUnauthorized) {
		t.Errorf("error = %v, want ports.ErrUnauthorized", err)
	}
	if strings.Contains(err.Error(), "ghs_LEAKSECRET123") {
		t.Errorf("error text carries the token-like value: %v", err)
	}
}

func TestAppJWTIsRS256SignedAndTimeBounded(t *testing.T) {
	env := newTestEnv(t)
	// The fake verifies the signature, issuer and time window on every mint; a failure there returns 401.
	if _, err := env.a.Tokens().Reader(context.Background()); err != nil {
		t.Fatalf("mint with the App JWT failed: %v", err)
	}
	reqs := env.f.requests()
	auth := reqs[len(reqs)-1].Auth
	if err := verifyJWT(strings.TrimPrefix(auth, "Bearer "), &env.key.PublicKey, env.clock.now); err != nil {
		t.Fatalf("JWT check: %v", err)
	}
}
