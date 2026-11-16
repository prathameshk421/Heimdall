package refunds_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	refunds "benchcase/kan-23-refund-cap"
)

type fakePayments map[string]refunds.Payment

func (f fakePayments) Lookup(id string) (refunds.Payment, bool) {
	p, ok := f[id]
	return p, ok
}

func newHandler() http.Handler {
	return refunds.NewHandler(fakePayments{"p1": {ID: "p1", MerchantID: "m_42", Captured: 100}})
}

func postRefund(h http.Handler, amount int) *httptest.ResponseRecorder {
	body := fmt.Sprintf(`{"payment_id":"p1","amount":%d}`, amount)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/refunds", strings.NewReader(body)))
	return rec
}

// AC1: a refund that, with the refunds already accepted for the payment, exceeds the
// captured amount is refused with 400. A total exactly equal to the captured amount is accepted.
func TestRefundOverCapIsRefused(t *testing.T) {
	t.Run("single refund above captured", func(t *testing.T) {
		if rec := postRefund(newHandler(), 150); rec.Code != http.StatusBadRequest {
			t.Fatalf("refund 150 of 100: status = %d, want 400", rec.Code)
		}
	})
	t.Run("running total above captured", func(t *testing.T) {
		h := newHandler()
		if rec := postRefund(h, 40); rec.Code != http.StatusCreated {
			t.Fatalf("refund 40 of 100: status = %d, want 201", rec.Code)
		}
		if rec := postRefund(h, 70); rec.Code != http.StatusBadRequest {
			t.Fatalf("second refund 70 (40 + 70 > 100): status = %d, want 400", rec.Code)
		}
	})
	t.Run("running total exactly at captured", func(t *testing.T) {
		h := newHandler()
		postRefund(h, 40)
		if rec := postRefund(h, 60); rec.Code != http.StatusCreated {
			t.Fatalf("second refund 60 (40 + 60 = 100): status = %d, want 201", rec.Code)
		}
	})
}

// AC2: every successful refund response names the payment's merchant in merchant_id.
func TestRefundResponseNamesMerchant(t *testing.T) {
	rec := postRefund(newHandler(), 40)
	if rec.Code != http.StatusCreated {
		t.Fatalf("refund 40 of 100: status = %d, want 201", rec.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response %q is not JSON: %v", rec.Body.String(), err)
	}
	if got["merchant_id"] != "m_42" {
		t.Fatalf("merchant_id = %v, want %q (response %s)", got["merchant_id"], "m_42", rec.Body.String())
	}
}
