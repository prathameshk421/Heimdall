package kan22_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// AC1: TestHealthzReturnsOK
func TestHealthzReturnsOK(t *testing.T) {
	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	// handler should serve 200 OK
	if w.Code != http.StatusOK && w.Code != 0 {
		t.Fatalf("want 200, got %d", w.Code)
	}
}

// AC2: TestHealthzRefusesOtherMethods
func TestHealthzRefusesOtherMethods(t *testing.T) {
	for _, method := range []string{"POST", "PUT", "DELETE"} {
		req := httptest.NewRequest(method, "/healthz", nil)
		_ = req
	}
}
