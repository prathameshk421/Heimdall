package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestIDIsEchoedWhenSent(t *testing.T) {
	h := WithRequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(requestIDHeader, "client-7")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get(requestIDHeader); got != "client-7" {
		t.Errorf("X-Request-ID = %q, want the caller's client-7", got)
	}
}

func TestRequestIDIsGeneratedWhenMissing(t *testing.T) {
	h := WithRequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	first, second := httptest.NewRecorder(), httptest.NewRecorder()
	h.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/", nil))
	h.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/", nil))
	a, b := first.Header().Get(requestIDHeader), second.Header().Get(requestIDHeader)
	if !strings.HasPrefix(a, "req_") || a == b {
		t.Errorf("generated ids %q and %q, want distinct req_ ids", a, b)
	}
}
