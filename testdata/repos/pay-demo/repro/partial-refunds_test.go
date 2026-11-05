// Package repro_test holds the repro tests for the planted findings. Each file stands alone, because the fixture
// tests copy one file at a time into tests/heimdall/repro.
package repro_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aarush323/heimdall-fixture-pay/internal/app"
)

// TestPartialRefundsCannotExceedPayment reproduces partial-refunds: two refunds of 60 on a payment of 100 both
// succeed, so the refunds total 120. The second refund must be refused with 422.
func TestPartialRefundsCannotExceedPayment(t *testing.T) {
	srv := httptest.NewServer(app.New(app.Config{}))
	defer srv.Close()

	do := func(path, key, body string) (int, []byte) {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Merchant-ID", "m-1")
		req.Header.Set("Idempotency-Key", key)
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, raw
	}

	status, body := do("/payments", "", `{"amount": 100, "currency": "USD"}`)
	if status != http.StatusCreated {
		t.Fatalf("POST /payments = %d: %s", status, body)
	}
	var pay struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &pay); err != nil {
		t.Fatal(err)
	}
	refund := `{"payment_id": "` + pay.ID + `", "amount": 60}`

	if status, body := do("/refunds", "k1", refund); status != http.StatusCreated {
		t.Fatalf("first refund of 60 = %d: %s", status, body)
	}
	if status, body := do("/refunds", "k2", refund); status != http.StatusUnprocessableEntity {
		t.Fatalf("second refund of 60 on a payment of 100 = %d, want 422: the refunds now total 120: %s", status, body)
	}
}
