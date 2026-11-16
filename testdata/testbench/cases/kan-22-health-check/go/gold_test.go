package health_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	health "benchcase/kan-22-health-check"
)

type spyDB struct{ pings int }

func (s *spyDB) Ping(context.Context) error {
	s.pings++
	return nil
}

func serve(h http.Handler, method string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, "/healthz", nil))
	return rec
}

// AC1: GET /healthz returns 200 with the body OK and does no database I/O.
func TestHealthzReturnsOK(t *testing.T) {
	db := &spyDB{}
	rec := serve(health.Handler(db), http.MethodGet)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /healthz: status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != "OK" {
		t.Fatalf("GET /healthz: body = %q, want %q", got, "OK")
	}
	if db.pings != 0 {
		t.Fatalf("GET /healthz: database pinged %d times, want 0", db.pings)
	}
}

// AC2: any method other than GET, HEAD included, returns 405.
func TestHealthzRefusesOtherMethods(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodHead} {
		rec := serve(health.Handler(&spyDB{}), method)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s /healthz: status = %d, want %d", method, rec.Code, http.StatusMethodNotAllowed)
		}
	}
}
