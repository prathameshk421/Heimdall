package webhooks

import (
	"strings"
	"testing"
	"time"
)

func TestEventValidateReportsEveryProblem(t *testing.T) {
	err := Event{Type: "payment.exploded", OccurredAt: time.Time{}}.Validate()
	if err == nil {
		t.Fatal("Validate accepted an event with no id, an unknown type, no merchant, no time and no data")
	}
	for _, field := range []string{"id", "type", "merchant_id", "occurred_at", "data"} {
		if !strings.Contains(err.Error(), field) {
			t.Errorf("error does not name %s: %v", field, err)
		}
	}
}

func TestEventPayloadIsItsJSON(t *testing.T) {
	e := Event{ID: "evt_1", Type: PaymentCreated, MerchantID: "m-1", OccurredAt: t0, Data: []byte(`{"amount":5}`)}
	if err := e.Validate(); err != nil {
		t.Fatalf("Validate(valid) = %v", err)
	}
	payload, err := e.Payload()
	if err != nil || !strings.Contains(string(payload), `"type":"payment.created"`) {
		t.Fatalf("Payload() = %s, %v", payload, err)
	}
}
