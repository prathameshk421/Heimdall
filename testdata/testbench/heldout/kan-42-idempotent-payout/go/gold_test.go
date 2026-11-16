package payouts_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	payouts "benchcase/kan-42-idempotent-payout"
)

type spyLedger struct{ inserted []payouts.Payout }

func (s *spyLedger) InsertPayout(p payouts.Payout) string {
	s.inserted = append(s.inserted, p)
	return fmt.Sprintf("po_%d", len(s.inserted))
}

func post(h http.Handler, key string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/payouts", strings.NewReader(`{"merchant_id":"m_1","amount":500}`))
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// AC1: a request with a new key creates a payout and returns 201 Created with a payout_id.
func TestInitialPayoutCreated(t *testing.T) {
	ledger := &spyLedger{}
	rec := post(payouts.Handler(ledger), "k1")
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if !strings.Contains(rec.Body.String(), `"payout_id":"po_1"`) {
		t.Fatalf("body %q has no payout_id po_1", rec.Body.String())
	}
	if n := len(ledger.inserted); n != 1 {
		t.Fatalf("payouts inserted = %d, want 1", n)
	}
}

// AC2: a repeated request with the same key returns the stored response and creates no payout.
func TestDuplicateKeyReturnsCached(t *testing.T) {
	ledger := &spyLedger{}
	h := payouts.Handler(ledger)
	first := post(h, "k1")
	second := post(h, "k1")
	if second.Code != first.Code {
		t.Fatalf("replay status = %d, want %d (the stored status)", second.Code, first.Code)
	}
	if !bytes.Equal(second.Body.Bytes(), first.Body.Bytes()) {
		t.Fatalf("replay body = %q, want the stored body %q", second.Body.String(), first.Body.String())
	}
	if n := len(ledger.inserted); n != 1 {
		t.Fatalf("payouts inserted after replay = %d, want 1", n)
	}
}

// AC3: a request without an Idempotency-Key header is refused with 400 and creates no payout.
func TestMissingIdempotencyKeyRefused(t *testing.T) {
	ledger := &spyLedger{}
	if rec := post(payouts.Handler(ledger), ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if n := len(ledger.inserted); n != 0 {
		t.Fatalf("payouts inserted = %d, want 0", n)
	}
}
