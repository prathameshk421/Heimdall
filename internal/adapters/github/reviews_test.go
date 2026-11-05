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

func TestPRReviewsSuccessAndStateMapping(t *testing.T) {
	env := newTestEnv(t)
	env.f.route("GET", "/repos/aarush323/heimdall-fixture-pay/pulls/10/reviews", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, []any{
			map[string]any{
				"id":           101,
				"state":        "APPROVED",
				"commit_id":    shaHead,
				"submitted_at": "2026-11-04T12:00:00Z",
				"user":         map[string]any{"login": "alice"},
			},
			map[string]any{
				"id":           102,
				"state":        "CHANGES_REQUESTED",
				"commit_id":    shaHead,
				"submitted_at": "2026-11-04T12:05:00Z",
				"user":         map[string]any{"login": "bob"},
			},
			map[string]any{
				"id":           103,
				"state":        "COMMENTED",
				"commit_id":    shaHead,
				"submitted_at": "2026-11-04T12:10:00Z",
				"user":         map[string]any{"login": "charlie"},
			},
			map[string]any{
				"id":           104,
				"state":        "DISMISSED",
				"commit_id":    shaHead,
				"submitted_at": "2026-11-04T12:15:00Z",
				"user":         map[string]any{"login": "dave"},
			},
			map[string]any{
				"id":           105,
				"state":        "PENDING",
				"commit_id":    shaHead,
				"submitted_at": "2026-11-04T12:20:00Z",
				"user":         map[string]any{"login": "eve"},
			},
		})
	})

	got, err := env.a.PRReviews(context.Background(), testRepo, 10)
	if err != nil {
		t.Fatalf("PRReviews: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("PRReviews len = %d, want 4 (PENDING skipped)", len(got))
	}

	expected := []struct {
		id    string
		login string
		state ports.ReviewState
	}{
		{"101", "alice", ports.ReviewStateApproved},
		{"102", "bob", ports.ReviewStateChangesRequested},
		{"103", "charlie", ports.ReviewStateCommented},
		{"104", "dave", ports.ReviewStateDismissed},
	}

	for i, exp := range expected {
		if got[i].ID != exp.id || string(got[i].Login) != exp.login || got[i].State != exp.state {
			t.Errorf("review[%d] = %+v, want ID=%s, Login=%s, State=%s", i, got[i], exp.id, exp.login, exp.state)
		}
		if string(got[i].CommitSHA) != shaHead {
			t.Errorf("review[%d].CommitSHA = %s, want %s", i, got[i].CommitSHA, shaHead)
		}
		if got[i].SubmittedAt.IsZero() {
			t.Errorf("review[%d].SubmittedAt is zero", i)
		}
	}
}

func TestPRReviewsPaginatesAcrossMultiplePages(t *testing.T) {
	env := newTestEnv(t)
	base := "/repos/aarush323/heimdall-fixture-pay/pulls/10/reviews"
	next := env.f.srv.URL + base + "?page=2"

	env.f.route("GET", base, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			writeJSON(w, http.StatusOK, []any{
				map[string]any{
					"id":           202,
					"state":        "APPROVED",
					"commit_id":    shaHead,
					"submitted_at": "2026-11-04T13:00:00Z",
					"user":         map[string]any{"login": "bob"},
				},
			})
			return
		}
		w.Header().Set("Link", `<`+next+`>; rel="next"`)
		writeJSON(w, http.StatusOK, []any{
			map[string]any{
				"id":           201,
				"state":        "APPROVED",
				"commit_id":    shaHead,
				"submitted_at": "2026-11-04T12:00:00Z",
				"user":         map[string]any{"login": "alice"},
			},
		})
	})

	got, err := env.a.PRReviews(context.Background(), testRepo, 10)
	if err != nil {
		t.Fatalf("PRReviews: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("PRReviews len = %d, want 2 across pages", len(got))
	}
	if got[0].ID != "201" || got[1].ID != "202" {
		t.Errorf("got IDs %s, %s; want 201, 202", got[0].ID, got[1].ID)
	}
}

func TestPRReviewsRefusesAMalformedSHA(t *testing.T) {
	for _, bad := range []string{"not-a-sha", "ABCDEF1234", "short", "9f1c2ab\n"} {
		env := newTestEnv(t)
		env.f.route("GET", "/repos/aarush323/heimdall-fixture-pay/pulls/10/reviews", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, []any{
				map[string]any{
					"id":           301,
					"state":        "APPROVED",
					"commit_id":    bad,
					"submitted_at": "2026-11-04T12:00:00Z",
					"user":         map[string]any{"login": "alice"},
				},
			})
		})
		_, err := env.a.PRReviews(context.Background(), testRepo, 10)
		if err == nil {
			t.Errorf("commit_id %q was accepted", bad)
		}
	}
}

func TestPRReviewsRefusesAnUnknownState(t *testing.T) {
	env := newTestEnv(t)
	env.f.route("GET", "/repos/aarush323/heimdall-fixture-pay/pulls/10/reviews", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, []any{
			map[string]any{
				"id":           401,
				"state":        "MYSTERY_STATE",
				"commit_id":    shaHead,
				"submitted_at": "2026-11-04T12:00:00Z",
				"user":         map[string]any{"login": "alice"},
			},
		})
	})
	_, err := env.a.PRReviews(context.Background(), testRepo, 10)
	if err == nil {
		t.Fatal("unknown review state was accepted")
	}
}

func TestPRReviewsUnauthorizedIsErrUnauthorized(t *testing.T) {
	env := newTestEnv(t)
	env.f.route("GET", "/repos/aarush323/heimdall-fixture-pay/pulls/10/reviews", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
	})
	_, err := env.a.PRReviews(context.Background(), testRepo, 10)
	if !errors.Is(err, ports.ErrUnauthorized) {
		t.Errorf("error = %v, want ports.ErrUnauthorized", err)
	}
}

func TestPRReviewsNotFoundIsErrNotFound(t *testing.T) {
	env := newTestEnv(t)
	env.f.route("GET", "/repos/aarush323/heimdall-fixture-pay/pulls/10/reviews", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
	})
	_, err := env.a.PRReviews(context.Background(), testRepo, 10)
	if !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("error = %v, want ports.ErrNotFound", err)
	}
}

func TestPRReviewsUsesTheReaderTokenNotTheWriter(t *testing.T) {
	env := newTestEnv(t)
	env.f.route("GET", "/repos/aarush323/heimdall-fixture-pay/pulls/10/reviews", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, []any{})
	})
	if _, err := env.a.PRReviews(context.Background(), testRepo, 10); err != nil {
		t.Fatalf("PRReviews: %v", err)
	}
	bodies := mintBodies(t, env.f)
	if len(bodies) != 1 {
		t.Fatalf("mint requests = %d, want 1 (reader only)", len(bodies))
	}
	want := map[string]any{"permissions": map[string]any{
		"contents": "read", "pull_requests": "read", "issues": "read", "checks": "read",
	}}
	if !reflect.DeepEqual(bodies[0], want) {
		t.Errorf("token body = %v, want reader permissions %v", bodies[0], want)
	}
	for _, r := range env.f.requests() {
		if strings.HasPrefix(r.Path, repoRoot) && r.Auth != "Bearer ghs_1" {
			t.Errorf("%s %s used auth %q, want reader token ghs_1", r.Method, r.Path, r.Auth)
		}
	}
}

func TestPRReviewsErrorCarriesNoToken(t *testing.T) {
	env := newTestEnv(t)
	env.f.route("GET", "/repos/aarush323/heimdall-fixture-pay/pulls/10/reviews", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "ghs_1 is bad"})
	})
	_, err := env.a.PRReviews(context.Background(), testRepo, 10)
	if err == nil {
		t.Fatal("401 was accepted")
	}
	if strings.Contains(err.Error(), "ghs_") {
		t.Errorf("error text carries token: %v", err)
	}
}
