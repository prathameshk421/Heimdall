package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aarush323/heimdall-fixture-pay/internal/payments"
	"github.com/aarush323/heimdall-fixture-pay/internal/refunds"
)

// newRefundRoutes returns the API over one shared store, with ids id_1, id_2, ...
func newRefundRoutes() http.Handler {
	store := payments.NewMemoryStore()
	n := 0
	newID := func() string {
		n++
		return "id_" + strconv.Itoa(n)
	}
	now := func() time.Time { return testNow }
	return Routes(payments.NewService(store, now, newID), refunds.NewService(store, now, newID))
}

// send issues one request with the given merchant and idempotency key (either may be empty).
func send(t *testing.T, h http.Handler, method, path, merchant, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if merchant != "" {
		req.Header.Set(merchantHeader, merchant)
	}
	if key != "" {
		req.Header.Set(idempotencyHeader, key)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// newPayment creates a payment of amount for merchant through the API and returns its id.
func newPayment(t *testing.T, h http.Handler, merchant string, amount int) string {
	t.Helper()
	rec := send(t, h, http.MethodPost, "/payments", merchant, "", `{"amount": `+strconv.Itoa(amount)+`, "currency": "USD"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /payments = %d: %s", rec.Code, rec.Body)
	}
	var p paymentJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p.ID
}

func TestRefundLargerThanThePaymentGets422(t *testing.T) {
	h := newRefundRoutes()
	pay := newPayment(t, h, "m-1", 100)
	rec := send(t, h, http.MethodPost, "/refunds", "m-1", "k1", `{"payment_id": "`+pay+`", "amount": 150}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", rec.Code, rec.Body)
	}
}

func TestRepeatedIdempotencyKeyReturnsTheOriginalRefund(t *testing.T) {
	h := newRefundRoutes()
	pay := newPayment(t, h, "m-1", 100)
	body := `{"payment_id": "` + pay + `", "amount": 40}`
	first := send(t, h, http.MethodPost, "/refunds", "m-1", "k1", body)
	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d, want 201: %s", first.Code, first.Body)
	}
	second := send(t, h, http.MethodPost, "/refunds", "m-1", "k1", body)
	if second.Code != http.StatusOK {
		t.Fatalf("replay status = %d, want 200 (never 409): %s", second.Code, second.Body)
	}
	var a, b refundJSON
	mustDecode(t, first, &a)
	mustDecode(t, second, &b)
	if a.ID != b.ID {
		t.Errorf("replay returned refund %q, want the original %q", b.ID, a.ID)
	}
}

func TestOtherMerchantCannotRefundAPayment(t *testing.T) {
	h := newRefundRoutes()
	pay := newPayment(t, h, "m-1", 100)
	rec := send(t, h, http.MethodPost, "/refunds", "m-2", "k1", `{"payment_id": "`+pay+`", "amount": 10}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for another merchant's payment", rec.Code)
	}
}

func TestGetReturnsTheRefundToItsMerchant(t *testing.T) {
	h := newRefundRoutes()
	pay := newPayment(t, h, "m-1", 100)
	created := send(t, h, http.MethodPost, "/refunds", "m-1", "k1", `{"payment_id": "`+pay+`", "amount": 10}`)
	var r refundJSON
	mustDecode(t, created, &r)

	rec := send(t, h, http.MethodGet, "/refunds/"+r.ID, "m-1", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var got refundJSON
	mustDecode(t, rec, &got)
	if got.ID != r.ID || got.PaymentID != pay || got.Amount != 10 {
		t.Errorf("GET returned %+v, want the created refund", got)
	}
}

func TestRefundNeedsTheIdempotencyKey(t *testing.T) {
	h := newRefundRoutes()
	pay := newPayment(t, h, "m-1", 100)
	rec := send(t, h, http.MethodPost, "/refunds", "m-1", "", `{"payment_id": "`+pay+`", "amount": 10}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 without Idempotency-Key", rec.Code)
	}
}

func TestGetNeedsTheMerchantHeader(t *testing.T) {
	h := newRefundRoutes()
	rec := send(t, h, http.MethodGet, "/refunds/id_1", "", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// mustDecode decodes a recorded JSON body into v.
func mustDecode(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body, err)
	}
}
