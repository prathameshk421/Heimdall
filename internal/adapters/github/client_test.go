package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// prListPath is the open-PR list of the fake repository.
const prListPath = "/repos/aarush323/heimdall-fixture-pay/pulls"

func samplePR(n int) map[string]any {
	return map[string]any{
		"number": n, "title": "t", "body": "b", "state": "open", "merged": false,
		"head": map[string]any{"ref": "feature", "sha": "1111111111111111111111111111111111111111"},
		"base": map[string]any{"sha": "2222222222222222222222222222222222222222"},
		"user": map[string]any{"login": "ana"},
	}
}

func TestRateLimitHonoursRetryAfterThenSucceeds(t *testing.T) {
	env := newTestEnv(t)
	var calls atomic.Int32
	env.f.route("GET", prListPath, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "5")
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"message": "slow down"})
			return
		}
		writeJSON(w, http.StatusOK, []any{samplePR(1)})
	})
	prs, err := env.a.OpenPRs(context.Background(), testRepo)
	if err != nil {
		t.Fatalf("OpenPRs: %v", err)
	}
	if len(prs) != 1 {
		t.Errorf("prs = %d, want 1", len(prs))
	}
	if want := []time.Duration{5 * time.Second}; fmt.Sprint(env.wait.waits) != fmt.Sprint(want) {
		t.Errorf("waits = %v, want %v", env.wait.waits, want)
	}
}

func TestRateLimitResetHeaderSetsTheWait(t *testing.T) {
	env := newTestEnv(t)
	reset := env.clock.now.Add(10 * time.Second).Unix()
	var calls atomic.Int32
	env.f.route("GET", prListPath, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset, 10))
			writeJSON(w, http.StatusForbidden, map[string]string{"message": "API rate limit exceeded"})
			return
		}
		writeJSON(w, http.StatusOK, []any{})
	})
	if _, err := env.a.OpenPRs(context.Background(), testRepo); err != nil {
		t.Fatalf("OpenPRs: %v", err)
	}
	if want := []time.Duration{10 * time.Second}; fmt.Sprint(env.wait.waits) != fmt.Sprint(want) {
		t.Errorf("waits = %v, want %v", env.wait.waits, want)
	}
}

func TestLongRetryAfterReturnsTypedErrorWithoutWaiting(t *testing.T) {
	env := newTestEnv(t)
	var calls atomic.Int32
	env.f.route("GET", prListPath, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "3600")
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"message": "secondary"})
	})
	_, err := env.a.OpenPRs(context.Background(), testRepo)
	var rl *RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("error = %v, want *RateLimitError", err)
	}
	if rl.RetryAfter != time.Hour {
		t.Errorf("RetryAfter = %v, want 1h", rl.RetryAfter)
	}
	if !errors.Is(err, ports.ErrUnavailable) {
		t.Errorf("error does not wrap ports.ErrUnavailable: %v", err)
	}
	if calls.Load() != 1 || len(env.wait.waits) != 0 {
		t.Errorf("calls = %d, waits = %v; want one call and no wait", calls.Load(), env.wait.waits)
	}
}

func TestRateLimitGivesUpAfterThreeAttemptsWithDoublingBackoff(t *testing.T) {
	env := newTestEnv(t)
	var calls atomic.Int32
	env.f.route("GET", prListPath, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"message": "slow"})
	})
	_, err := env.a.OpenPRs(context.Background(), testRepo)
	var rl *RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("error = %v, want *RateLimitError", err)
	}
	if calls.Load() != 3 {
		t.Errorf("calls = %d, want 3", calls.Load())
	}
	if want := []time.Duration{time.Minute, 2 * time.Minute}; fmt.Sprint(env.wait.waits) != fmt.Sprint(want) {
		t.Errorf("waits = %v, want %v", env.wait.waits, want)
	}
}

func TestStatusTranslation(t *testing.T) {
	cases := []struct {
		name   string
		status int
		header map[string]string
		is     error
		isNot  []error
	}{
		{"not found", 404, nil, ports.ErrNotFound, []error{ports.ErrUnauthorized, ports.ErrUnavailable}},
		{"401 is unauthorized", 401, nil, ports.ErrUnauthorized, []error{ports.ErrUnavailable, ports.ErrNotFound}},
		{"403 without rate headers is unauthorized", 403, nil, ports.ErrUnauthorized, []error{ports.ErrUnavailable}},
		{"500 is unavailable", 500, nil, ports.ErrUnavailable, []error{ports.ErrUnauthorized}},
		{"502 is unavailable", 502, nil, ports.ErrUnavailable, nil},
		{"422 is neither", 422, nil, nil, []error{ports.ErrUnavailable, ports.ErrUnauthorized, ports.ErrNotFound}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestEnv(t)
			env.f.route("GET", prListPath, func(w http.ResponseWriter, _ *http.Request) {
				for k, v := range tc.header {
					w.Header().Set(k, v)
				}
				writeJSON(w, tc.status, map[string]string{"message": "Not Found"})
			})
			_, err := env.a.OpenPRs(context.Background(), testRepo)
			if err == nil {
				t.Fatal("expected an error")
			}
			if tc.is != nil && !errors.Is(err, tc.is) {
				t.Errorf("error = %v, want it to wrap %v", err, tc.is)
			}
			for _, bad := range tc.isNot {
				if errors.Is(err, bad) {
					t.Errorf("error = %v must not wrap %v", err, bad)
				}
			}
		})
	}
}

func TestTransportFailureIsUnavailable(t *testing.T) {
	env := newTestEnv(t)
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	a, err := New(Config{
		BaseURL: dead.URL, App: env.a.tokens.app, HTTPClient: dead.Client(), Clock: env.clock, Wait: env.wait.wait,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// The token mint fails first, so the failure is still a transport failure wrapped as unavailable.
	_, err = a.OpenPRs(context.Background(), testRepo)
	if !errors.Is(err, ports.ErrUnavailable) {
		t.Errorf("error = %v, want ports.ErrUnavailable", err)
	}
}

func TestBreakerOpensAfterFiveFailuresAndProbesAfterThirtySeconds(t *testing.T) {
	env := newTestEnv(t)
	var calls atomic.Int32
	healthy := atomic.Bool{}
	env.f.route("GET", prListPath, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		if healthy.Load() {
			writeJSON(w, http.StatusOK, []any{})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "boom"})
	})
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, err := env.a.OpenPRs(ctx, testRepo); !errors.Is(err, ports.ErrUnavailable) {
			t.Fatalf("call %d: %v, want unavailable", i, err)
		}
	}
	before := calls.Load()
	if _, err := env.a.OpenPRs(ctx, testRepo); !errors.Is(err, ports.ErrUnavailable) {
		t.Fatalf("open breaker: %v, want unavailable", err)
	}
	if calls.Load() != before {
		t.Errorf("an open breaker still sent a request")
	}

	env.clock.advance(30 * time.Second)
	healthy.Store(true)
	if _, err := env.a.OpenPRs(ctx, testRepo); err != nil {
		t.Fatalf("probe after cooldown failed: %v", err)
	}
	if _, err := env.a.OpenPRs(ctx, testRepo); err != nil {
		t.Errorf("breaker should be closed after a good probe: %v", err)
	}
}

func TestFailedProbeReopensTheBreaker(t *testing.T) {
	env := newTestEnv(t)
	env.f.route("GET", prListPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusBadGateway, map[string]string{"message": "down"})
	})
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		_, _ = env.a.OpenPRs(ctx, testRepo)
	}
	env.clock.advance(30 * time.Second)
	_, _ = env.a.OpenPRs(ctx, testRepo) // the probe fails
	before := len(env.f.requests())
	if _, err := env.a.OpenPRs(ctx, testRepo); !errors.Is(err, ports.ErrUnavailable) {
		t.Fatalf("after failed probe: %v, want unavailable", err)
	}
	if len(env.f.requests()) != before {
		t.Errorf("breaker did not reopen after the failed probe")
	}
}

func TestClientErrorsDoNotTripTheBreaker(t *testing.T) {
	env := newTestEnv(t)
	env.f.route("GET", prListPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
	})
	for i := 0; i < 10; i++ {
		_, _ = env.a.OpenPRs(context.Background(), testRepo)
	}
	env.f.route("GET", prListPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, []any{})
	})
	if _, err := env.a.OpenPRs(context.Background(), testRepo); err != nil {
		t.Errorf("404s opened the breaker: %v", err)
	}
}

func TestEveryCallCarriesADeadline(t *testing.T) {
	env := newTestEnv(t)
	var sawDeadline atomic.Bool
	env.a.c.http = &http.Client{Transport: deadlineRecorder{base: env.f.srv.Client().Transport, saw: &sawDeadline}}
	env.f.route("GET", prListPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, []any{})
	})
	if _, err := env.a.OpenPRs(context.Background(), testRepo); err != nil {
		t.Fatalf("OpenPRs: %v", err)
	}
	if !sawDeadline.Load() {
		t.Error("request had no deadline")
	}
}

type deadlineRecorder struct {
	base http.RoundTripper
	saw  *atomic.Bool
}

func (d deadlineRecorder) RoundTrip(r *http.Request) (*http.Response, error) {
	if _, ok := r.Context().Deadline(); ok {
		d.saw.Store(true)
	}
	return d.base.RoundTrip(r)
}

func TestOpenPRsPaginatesThroughLinkHeaders(t *testing.T) {
	env := newTestEnv(t)
	next := env.f.srv.URL + prListPath + "?state=open&per_page=100&page=2"
	env.f.route("GET", prListPath, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			writeJSON(w, http.StatusOK, []any{samplePR(2)})
			return
		}
		w.Header().Set("Link", `<`+next+`>; rel="next", <`+next+`>; rel="last"`)
		writeJSON(w, http.StatusOK, []any{samplePR(1)})
	})
	prs, err := env.a.OpenPRs(context.Background(), testRepo)
	if err != nil {
		t.Fatalf("OpenPRs: %v", err)
	}
	if len(prs) != 2 || prs[0].Number != 1 || prs[1].Number != 2 {
		t.Errorf("prs = %+v, want numbers 1 and 2", prs)
	}
}

func TestPaginationNeverFollowsALinkOffTheAPIHost(t *testing.T) {
	env := newTestEnv(t)
	env.f.route("GET", prListPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Link", `<https://evil.example/steal>; rel="next"`)
		writeJSON(w, http.StatusOK, []any{samplePR(1)})
	})
	if _, err := env.a.OpenPRs(context.Background(), testRepo); err == nil {
		t.Fatal("a next link off the API host was followed")
	}
	for _, r := range env.f.requests() {
		if r.Path == "/steal" {
			t.Fatal("the foreign link was requested")
		}
	}
}

func TestOpenPRsUsesETagAndServesCacheOn304(t *testing.T) {
	env := newTestEnv(t)
	var calls atomic.Int32
	env.f.route("GET", prListPath, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		writeJSON(w, http.StatusOK, []any{samplePR(5)})
	})
	ctx := context.Background()
	first, err := env.a.OpenPRs(ctx, testRepo)
	if err != nil || len(first) != 1 {
		t.Fatalf("first OpenPRs: %v, %d prs", err, len(first))
	}
	second, err := env.a.OpenPRs(ctx, testRepo)
	if err != nil {
		t.Fatalf("second OpenPRs: %v", err)
	}
	if len(second) != 1 || second[0].Number != 5 {
		t.Errorf("304 did not return the cached list: %+v", second)
	}
	if calls.Load() != 2 {
		t.Errorf("calls = %d, want 2", calls.Load())
	}
}
