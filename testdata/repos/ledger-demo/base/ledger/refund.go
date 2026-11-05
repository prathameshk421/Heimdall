// Package ledger consumes refund responses and posts each refund once. The refund_ref field keys a posting, so a
// response that lacks it cannot be posted and is refused.
package ledger

import (
	"encoding/json"
	"errors"
	"fmt"
)

// RefundResponse is the part of a refund response the ledger reads.
type RefundResponse struct {
	RefundRef string `json:"refund_ref"`
	PaymentID string `json:"payment_id"`
	Amount    int64  `json:"amount"`
	Currency  string `json:"currency"`
}

// ParseRefund decodes a refund response and checks the fields a posting needs. Every missing field is reported.
func ParseRefund(body []byte) (RefundResponse, error) {
	var r RefundResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return RefundResponse{}, fmt.Errorf("parse refund response: %w", err)
	}
	var errs []error
	if r.RefundRef == "" {
		errs = append(errs, errors.New("refund_ref: is missing"))
	}
	if r.Amount < 1 {
		errs = append(errs, fmt.Errorf("amount: %d is below 1", r.Amount))
	}
	if r.Currency == "" {
		errs = append(errs, errors.New("currency: is missing"))
	}
	if err := errors.Join(errs...); err != nil {
		return RefundResponse{}, fmt.Errorf("refund response: %w", err)
	}
	return r, nil
}
