package missingroute_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMissingRoute(t *testing.T) {
	server := httptest.NewServer(http.NewServeMux())
	defer server.Close()

	response, err := http.Get(server.URL + "/missing")
	if err != nil {
		t.Fatalf("request missing route: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Errorf("missing route status = %d, want %d", response.StatusCode, http.StatusOK)
	}
}
