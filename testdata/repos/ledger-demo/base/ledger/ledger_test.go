package ledger

import (
	"os"
	"strings"
	"testing"
)

func loadGolden(t *testing.T) RefundResponse {
	t.Helper()
	body, err := os.ReadFile("testdata/refund_response.json")
	if err != nil {
		t.Fatal(err)
	}
	r, err := ParseRefund(body)
	if err != nil {
		t.Fatalf("ParseRefund(golden): %v", err)
	}
	return r
}

func TestParseRefundReadsRefundRef(t *testing.T) {
	r := loadGolden(t)
	if r.RefundRef != "id_7" || r.Amount != 60 || r.Currency != "USD" {
		t.Errorf("parsed %+v, want refund_ref id_7, amount 60, currency USD", r)
	}
}

func TestParseRefundRefusesAResponseWithoutRefundRef(t *testing.T) {
	_, err := ParseRefund([]byte(`{"payment_id":"id_3","amount":60,"currency":"USD"}`))
	if err == nil || !strings.Contains(err.Error(), "refund_ref") {
		t.Fatalf("ParseRefund(no refund_ref) err = %v, want a refund_ref problem", err)
	}
}

func TestParseRefundReportsEveryMissingField(t *testing.T) {
	_, err := ParseRefund([]byte(`{}`))
	for _, field := range []string{"refund_ref", "amount", "currency"} {
		if err == nil || !strings.Contains(err.Error(), field) {
			t.Errorf("error %v does not name %s", err, field)
		}
	}
}

func TestPostIsOncePerRefundRef(t *testing.T) {
	l := New()
	r := loadGolden(t)
	if !l.Post(r) {
		t.Fatal("first post was reported as a duplicate")
	}
	if l.Post(r) {
		t.Error("a repeated refund_ref was posted twice")
	}
	if got := l.Balance("USD"); got != -60 {
		t.Errorf("balance = %d, want -60 after one refund of 60", got)
	}
}
