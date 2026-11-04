// Package refunds holds the refund rules. A refund takes money back from one payment. A repeated request with the
// same Idempotency-Key returns the original refund, and a refund is refused when it is larger than what is left.
package refunds

import (
	"errors"
	"time"
)

// ErrInvalid marks a request that breaks a field rule. The API answers 400.
var ErrInvalid = errors.New("invalid refund request")

// ErrPaymentNotFound means the payment does not exist for the caller. It is also the answer when the payment
// belongs to another merchant, so the API never reveals that someone else's payment exists.
var ErrPaymentNotFound = errors.New("payment not found")

// ErrExceedsPayment means the refund is larger than what is left of its payment. The API answers 422.
var ErrExceedsPayment = errors.New("refund exceeds what is left of the payment")

// ErrNotFound means the refund does not exist for the caller.
var ErrNotFound = errors.New("refund not found")

// Refund is money taken back from a payment. Amount is in the smallest unit of Currency.
type Refund struct {
	ID         string
	PaymentID  string
	MerchantID string
	Amount     int64
	Currency   string
	CreatedAt  time.Time
}
