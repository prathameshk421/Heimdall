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

// TestRefundReadIsScopedToItsMerchant reproduces refund-read-authz: merchant m-2 reads a refund made by m-1 and
// gets 200. The read must be answered 404, as if the refund did not exist for m-2.
func TestRefundReadIsScopedToItsMerchant(t *testing.T) {
	srv := httptest.NewServer(app.New(app.Config{}))
	defer srv.Close()

	do := func(method, path, merchant, key, body string) (int, []byte) {
		t.Helper()
		req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Merchant-ID", merchant)
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
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

	status, body := do(http.MethodPost, "/payments", "m-1", "", `{"amount": 100, "currency": "USD"}`)
	if status != http.StatusCreated {
		t.Fatalf("POST /payments by m-1 = %d: %s", status, body)
	}
	var pay struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &pay); err != nil {
		t.Fatal(err)
	}
	status, body = do(http.MethodPost, "/refunds", "m-1", "k1", `{"payment_id": "`+pay.ID+`", "amount": 10}`)
	if status != http.StatusCreated {
		t.Fatalf("refund by m-1 = %d: %s", status, body)
	}
	var ref struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &ref); err != nil {
		t.Fatal(err)
	}

	if status, body := do(http.MethodGet, "/refunds/"+ref.ID, "m-2", "", ""); status != http.StatusNotFound {
		t.Fatalf("m-2 read m-1's refund = %d, want 404: %s", status, body)
	}
}
