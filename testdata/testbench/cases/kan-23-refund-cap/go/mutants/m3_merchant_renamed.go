// Package refunds serves POST /refunds (KAN-23).
package refunds

import (
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
)

// Payment is a captured payment. Amounts are in minor units.
type Payment struct {
	ID         string
	MerchantID string
	Captured   int64
}

// Payments finds captured payments by id.
type Payments interface {
	Lookup(id string) (Payment, bool)
}

type refundRequest struct {
	PaymentID string `json:"payment_id"`
	Amount    int64  `json:"amount"`
}

type refundResponse struct {
	ID         string `json:"id"`
	PaymentID  string `json:"payment_id"`
	Amount     int64  `json:"amount"`
	MerchantID string `json:"merchant"`
}

// NewHandler returns the POST /refunds handler. It remembers the refunds it accepted,
// so the cap applies to the running total per payment.
func NewHandler(payments Payments) http.Handler {
	var mu sync.Mutex
	refunded := map[string]int64{}
	next := 0
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		var req refundRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		p, ok := payments.Lookup(req.PaymentID)
		if !ok {
			http.Error(w, "unknown payment", http.StatusNotFound)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if refunded[p.ID]+req.Amount > p.Captured {
			http.Error(w, "refund exceeds captured amount", http.StatusBadRequest)
			return
		}
		refunded[p.ID] += req.Amount
		next++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(refundResponse{
			ID:         "rf_" + strconv.Itoa(next),
			PaymentID:  p.ID,
			Amount:     req.Amount,
			MerchantID: p.MerchantID,
		})
	})
}
