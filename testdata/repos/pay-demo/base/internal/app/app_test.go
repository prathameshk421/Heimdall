package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestNewServesPaymentsThroughTheSeam drives the service only through app.New, as acceptance tests do.
func TestNewServesPaymentsThroughTheSeam(t *testing.T) {
	fixed := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(New(Config{Now: func() time.Time { return fixed }}))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/payments", strings.NewReader(`{"amount": 500, "currency": "GBP"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Merchant-ID", "m-9")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var body struct {
		ID        string    `json:"id"`
		CreatedAt time.Time `json:"created_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.ID != "id_1" || !body.CreatedAt.Equal(fixed) {
		t.Errorf("body = %+v, want id id_1 at the injected time", body)
	}
}

func TestSequentialIDsCountUp(t *testing.T) {
	next := sequentialIDs()
	if a, b := next(), next(); a != "id_1" || b != "id_2" {
		t.Errorf("ids = %q, %q, want id_1, id_2", a, b)
	}
}
