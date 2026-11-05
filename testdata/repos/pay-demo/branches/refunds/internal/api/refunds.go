package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/aarush323/heimdall-fixture-pay/internal/refunds"
)

// idempotencyHeader carries the client's key. A repeated request with the same key returns the original refund.
const idempotencyHeader = "Idempotency-Key"

// createRefundJSON is the body of POST /refunds.
type createRefundJSON struct {
	PaymentID string `json:"payment_id"`
	Amount    int64  `json:"amount"`
}

// refundJSON is the wire form of a refund, as documented in api/openapi.yaml.
type refundJSON struct {
	ID         string    `json:"id"`
	PaymentID  string    `json:"payment_id"`
	MerchantID string    `json:"merchant_id"`
	Amount     int64     `json:"amount"`
	Currency   string    `json:"currency"`
	Reference  string    `json:"reference"`
	CreatedAt  time.Time `json:"created_at"`
}

// toRefundJSON maps a refund to its wire form.
func toRefundJSON(r refunds.Refund) refundJSON {
	return refundJSON{
		ID:         r.ID,
		PaymentID:  r.PaymentID,
		MerchantID: r.MerchantID,
		Amount:     r.Amount,
		Currency:   r.Currency,
		Reference:  r.ID,
		CreatedAt:  r.CreatedAt,
	}
}

// createRefund handles POST /refunds. A replayed key answers 200 with the original refund, a new refund 201.
func createRefund(svc *refunds.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		merchant := r.Header.Get(merchantHeader)
		if merchant == "" {
			writeError(w, http.StatusUnauthorized, "missing X-Merchant-ID header")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		var body createRefundJSON
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "body must be JSON with payment_id and amount")
			return
		}
		ref, replayed, err := svc.Create(refunds.Request{
			MerchantID:     merchant,
			PaymentID:      body.PaymentID,
			Amount:         body.Amount,
			IdempotencyKey: r.Header.Get(idempotencyHeader),
		})
		if err != nil {
			writeRefundError(w, err)
			return
		}
		status := http.StatusCreated
		if replayed {
			status = http.StatusOK
		}
		writeJSON(w, status, toRefundJSON(ref))
	}
}

// getRefund handles GET /refunds/{id} for any caller that names a merchant.
func getRefund(svc *refunds.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(merchantHeader) == "" {
			writeError(w, http.StatusUnauthorized, "missing X-Merchant-ID header")
			return
		}
		ref, err := svc.Get(r.PathValue("id"))
		if err != nil {
			writeRefundError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toRefundJSON(ref))
	}
}

// writeRefundError maps a refunds error to its status: 400 invalid, 404 unknown, 422 over the payment.
func writeRefundError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, refunds.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, refunds.ErrPaymentNotFound), errors.Is(err, refunds.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, refunds.ErrExceedsPayment):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}
