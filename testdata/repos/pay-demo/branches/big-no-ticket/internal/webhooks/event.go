// Package webhooks delivers events to merchant endpoints. Every delivery is signed with the subscription's secret,
// retried on failure with capped backoff, and recorded, so a merchant can verify an event and see what was sent.
package webhooks

import (
	"encoding/json"
	"errors"
	"time"
)

// EventType names an event a merchant can subscribe to.
type EventType string

// The event types this service emits.
const (
	PaymentCreated EventType = "payment.created"
	RefundCreated  EventType = "refund.created"
)

// Known reports whether t is an event type this service emits.
func (t EventType) Known() bool {
	return t == PaymentCreated || t == RefundCreated
}

// Event is something that happened to a merchant's money. Data is the JSON object the event describes.
type Event struct {
	ID         string          `json:"id"`
	Type       EventType       `json:"type"`
	MerchantID string          `json:"merchant_id"`
	OccurredAt time.Time       `json:"occurred_at"`
	Data       json.RawMessage `json:"data"`
}

// Validate reports every problem with e, so a caller sees all of them at once.
func (e Event) Validate() error {
	var errs []error
	if e.ID == "" {
		errs = append(errs, errors.New("id: is empty"))
	}
	if !e.Type.Known() {
		errs = append(errs, errors.New("type: is not an event this service emits"))
	}
	if e.MerchantID == "" {
		errs = append(errs, errors.New("merchant_id: is empty"))
	}
	if e.OccurredAt.IsZero() {
		errs = append(errs, errors.New("occurred_at: is unset"))
	}
	if !json.Valid(e.Data) {
		errs = append(errs, errors.New("data: is not valid JSON"))
	}
	return errors.Join(errs...)
}

// Payload is the JSON body sent to a subscriber.
func (e Event) Payload() ([]byte, error) {
	return json.Marshal(e)
}
