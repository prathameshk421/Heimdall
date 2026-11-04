package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aarush323/heimdall-fixture-pay/internal/payments"
)

var testNow = time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)

// newTestHandler returns the payments API over a fresh store with a fixed clock.
func newTestHandler() http.Handler {
	newID := func() string { return "id_1" }
	svc := payments.NewService(payments.NewMemoryStore(), func() time.Time { return testNow }, newID)
	return Routes(Deps{Payments: svc})
}

func post(t *testing.T, h http.Handler, merchant, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/payments", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if merchant != "" {
		req.Header.Set(merchantHeader, merchant)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestCreatePaymentReturnsTheCreatedPayment(t *testing.T) {
	rec := post(t, newTestHandler(), "m-1", `{"amount": 1200, "currency": "USD"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %s", rec.Code, rec.Body)
	}
	var got paymentJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not a payment: %v", err)
	}
	if got.ID == "" || got.MerchantID != "m-1" || got.Amount != 1200 || got.Currency != "USD" {
		t.Errorf("payment = %+v, want merchant m-1, amount 1200, currency USD and an id", got)
	}
	if !got.CreatedAt.Equal(testNow) {
		t.Errorf("created_at = %v, want %v", got.CreatedAt, testNow)
	}
}

func TestCreatePaymentNeedsTheMerchantHeader(t *testing.T) {
	rec := post(t, newTestHandler(), "", `{"amount": 1200, "currency": "USD"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 without X-Merchant-ID", rec.Code)
	}
}

func TestCreatePaymentRejectsBadBodies(t *testing.T) {
	cases := map[string]string{
		"not json":         `{"amount":`,
		"zero amount":      `{"amount": 0, "currency": "USD"}`,
		"lower-case curr.": `{"amount": 10, "currency": "usd"}`,
		"missing currency": `{"amount": 10}`,
		"negative amount":  `{"amount": -1, "currency": "EUR"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			rec := post(t, newTestHandler(), "m-1", body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d for %s, want 400", rec.Code, body)
			}
			var e errorJSON
			if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil || e.Error == "" {
				t.Errorf("400 body %q does not carry an error message", rec.Body)
			}
		})
	}
}
