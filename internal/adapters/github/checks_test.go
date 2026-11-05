package github

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

func TestCheckRunsSuccessAndConclusionMapping(t *testing.T) {
	env := newTestEnv(t)
	path := "/repos/aarush323/heimdall-fixture-pay/commits/" + shaHead + "/check-runs"

	env.f.route("GET", path, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"total_count": 8,
			"check_runs": []any{
				map[string]any{
					"id":         1,
					"name":       "ci/success",
					"head_sha":   shaHead,
					"status":     "completed",
					"conclusion": "success",
				},
				map[string]any{
					"id":         2,
					"name":       "ci/failure",
					"head_sha":   shaHead,
					"status":     "completed",
					"conclusion": "failure",
				},
				map[string]any{
					"id":         3,
					"name":       "ci/neutral",
					"head_sha":   shaHead,
					"status":     "completed",
					"conclusion": "neutral",
				},
				map[string]any{
					"id":         4,
					"name":       "ci/cancelled",
					"head_sha":   shaHead,
					"status":     "completed",
					"conclusion": "cancelled",
				},
				map[string]any{
					"id":         5,
					"name":       "ci/skipped",
					"head_sha":   shaHead,
					"status":     "completed",
					"conclusion": "skipped",
				},
				map[string]any{
					"id":         6,
					"name":       "ci/timed_out",
					"head_sha":   shaHead,
					"status":     "completed",
					"conclusion": "timed_out",
				},
				map[string]any{
					"id":         7,
					"name":       "ci/action_required",
					"head_sha":   shaHead,
					"status":     "completed",
					"conclusion": "action_required",
				},
				map[string]any{
					"id":         8,
					"name":       "heimdall/gate",
					"head_sha":   shaHead,
					"status":     "completed",
					"conclusion": "success",
				},
			},
		})
	})

	got, err := env.a.CheckRuns(context.Background(), testRepo, id.SHA(shaHead))
	if err != nil {
		t.Fatalf("CheckRuns: %v", err)
	}
	// heimdall/gate must be ignored, so len is 7
	if len(got) != 7 {
		t.Fatalf("CheckRuns len = %d, want 7 (gate run dropped)", len(got))
	}

	expected := []struct {
		id         string
		name       string
		status     ports.CheckStatus
		conclusion change.CIConclusion
	}{
		{"1", "ci/success", ports.CheckCompleted, change.CISuccess},
		{"2", "ci/failure", ports.CheckCompleted, change.CIFailure},
		{"3", "ci/neutral", ports.CheckCompleted, change.CINeutral},
		{"4", "ci/cancelled", ports.CheckCompleted, change.CICancelled},
		{"5", "ci/skipped", ports.CheckCompleted, change.CISkipped},
		{"6", "ci/timed_out", ports.CheckCompleted, change.CITimedOut},
		{"7", "ci/action_required", ports.CheckCompleted, change.CIActionRequired},
	}

	for i, exp := range expected {
		if got[i].ID != exp.id || got[i].Name != exp.name || got[i].Status != exp.status || got[i].Conclusion != exp.conclusion {
			t.Errorf("check[%d] = %+v, want ID=%s, Name=%s, Status=%s, Conclusion=%s",
				i, got[i], exp.id, exp.name, exp.status, exp.conclusion)
		}
		if string(got[i].SHA) != shaHead {
			t.Errorf("check[%d].SHA = %s, want %s", i, got[i].SHA, shaHead)
		}
	}
}

func TestCheckRunsInProgressAndQueuedHaveNoConclusion(t *testing.T) {
	env := newTestEnv(t)
	path := "/repos/aarush323/heimdall-fixture-pay/commits/" + shaHead + "/check-runs"

	env.f.route("GET", path, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"total_count": 2,
			"check_runs": []any{
				map[string]any{
					"id":         10,
					"name":       "ci/in-progress",
					"head_sha":   shaHead,
					"status":     "in_progress",
					"conclusion": nil,
				},
				map[string]any{
					"id":         11,
					"name":       "ci/queued",
					"head_sha":   shaHead,
					"status":     "queued",
					"conclusion": nil,
				},
			},
		})
	})

	got, err := env.a.CheckRuns(context.Background(), testRepo, id.SHA(shaHead))
	if err != nil {
		t.Fatalf("CheckRuns: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("CheckRuns len = %d, want 2", len(got))
	}
	if got[0].Status != ports.CheckInProgress || got[0].Conclusion != "" {
		t.Errorf("in_progress run: status %s conclusion %q", got[0].Status, got[0].Conclusion)
	}
	if got[1].Status != ports.CheckQueued || got[1].Conclusion != "" {
		t.Errorf("queued run: status %s conclusion %q", got[1].Status, got[1].Conclusion)
	}
}

func TestCheckRunsPaginatesAcrossMultiplePages(t *testing.T) {
	env := newTestEnv(t)
	base := "/repos/aarush323/heimdall-fixture-pay/commits/" + shaHead + "/check-runs"
	next := env.f.srv.URL + base + "?page=2"

	env.f.route("GET", base, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			writeJSON(w, http.StatusOK, map[string]any{
				"total_count": 2,
				"check_runs": []any{
					map[string]any{
						"id":         52,
						"name":       "ci/page2",
						"head_sha":   shaHead,
						"status":     "completed",
						"conclusion": "success",
					},
				},
			})
			return
		}
		w.Header().Set("Link", `<`+next+`>; rel="next"`)
		writeJSON(w, http.StatusOK, map[string]any{
			"total_count": 2,
			"check_runs": []any{
				map[string]any{
					"id":         51,
					"name":       "ci/page1",
					"head_sha":   shaHead,
					"status":     "completed",
					"conclusion": "success",
				},
			},
		})
	})

	got, err := env.a.CheckRuns(context.Background(), testRepo, id.SHA(shaHead))
	if err != nil {
		t.Fatalf("CheckRuns: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("CheckRuns len = %d, want 2", len(got))
	}
	if got[0].ID != "51" || got[1].ID != "52" {
		t.Errorf("got IDs %s, %s; want 51, 52", got[0].ID, got[1].ID)
	}
}

func TestCheckRunsRefusesAMalformedSHA(t *testing.T) {
	for _, bad := range []string{"not-a-sha", "ABCDEF1234", "9f1c2ab\n"} {
		env := newTestEnv(t)
		path := "/repos/aarush323/heimdall-fixture-pay/commits/" + shaHead + "/check-runs"
		env.f.route("GET", path, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{
				"total_count": 1,
				"check_runs": []any{
					map[string]any{
						"id":         61,
						"name":       "ci/bad-sha",
						"head_sha":   bad,
						"status":     "completed",
						"conclusion": "success",
					},
				},
			})
		})
		_, err := env.a.CheckRuns(context.Background(), testRepo, id.SHA(shaHead))
		if err == nil {
			t.Errorf("head_sha %q was accepted", bad)
		}
	}
}

func TestCheckRunsRefusesAnUnknownConclusion(t *testing.T) {
	env := newTestEnv(t)
	path := "/repos/aarush323/heimdall-fixture-pay/commits/" + shaHead + "/check-runs"
	env.f.route("GET", path, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"total_count": 1,
			"check_runs": []any{
				map[string]any{
					"id":         71,
					"name":       "ci/weird",
					"head_sha":   shaHead,
					"status":     "completed",
					"conclusion": "exploded",
				},
			},
		})
	})
	_, err := env.a.CheckRuns(context.Background(), testRepo, id.SHA(shaHead))
	if err == nil {
		t.Fatal("unknown conclusion was accepted")
	}
}

func TestCheckRunsUnauthorizedIsErrUnauthorized(t *testing.T) {
	env := newTestEnv(t)
	path := "/repos/aarush323/heimdall-fixture-pay/commits/" + shaHead + "/check-runs"
	env.f.route("GET", path, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
	})
	_, err := env.a.CheckRuns(context.Background(), testRepo, id.SHA(shaHead))
	if !errors.Is(err, ports.ErrUnauthorized) {
		t.Errorf("error = %v, want ports.ErrUnauthorized", err)
	}
}

func TestCheckRunsNotFoundIsErrNotFound(t *testing.T) {
	env := newTestEnv(t)
	path := "/repos/aarush323/heimdall-fixture-pay/commits/" + shaHead + "/check-runs"
	env.f.route("GET", path, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
	})
	_, err := env.a.CheckRuns(context.Background(), testRepo, id.SHA(shaHead))
	if !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("error = %v, want ports.ErrNotFound", err)
	}
}

func TestCheckRunsUsesTheReaderTokenNotTheWriter(t *testing.T) {
	env := newTestEnv(t)
	path := "/repos/aarush323/heimdall-fixture-pay/commits/" + shaHead + "/check-runs"
	env.f.route("GET", path, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"total_count": 0, "check_runs": []any{}})
	})
	if _, err := env.a.CheckRuns(context.Background(), testRepo, id.SHA(shaHead)); err != nil {
		t.Fatalf("CheckRuns: %v", err)
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

func TestCheckRunsErrorCarriesNoToken(t *testing.T) {
	env := newTestEnv(t)
	path := "/repos/aarush323/heimdall-fixture-pay/commits/" + shaHead + "/check-runs"
	env.f.route("GET", path, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "ghs_1 is bad"})
	})
	_, err := env.a.CheckRuns(context.Background(), testRepo, id.SHA(shaHead))
	if err == nil {
		t.Fatal("401 was accepted")
	}
	if strings.Contains(err.Error(), "ghs_") {
		t.Errorf("error text carries token: %v", err)
	}
}

func TestCheckRunsRefusesBadInputSHA(t *testing.T) {
	env := newTestEnv(t)
	_, err := env.a.CheckRuns(context.Background(), testRepo, id.SHA("not-a-sha"))
	if err == nil {
		t.Fatal("bad input SHA was accepted")
	}
}
