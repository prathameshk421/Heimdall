// Package payouts serves POST /payouts with Idempotency-Key replay (KAN-42).
package payouts

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

const keyHeader = "Idempotency-Key"

// Payout is one payout request.
type Payout struct {
	MerchantID string `json:"merchant_id"`
	Amount     int64  `json:"amount"`
}

// Ledger records payouts and returns the new payout's id.
type Ledger interface {
	InsertPayout(p Payout) string
}

type response struct {
	PayoutID   string `json:"payout_id"`
	MerchantID string `json:"merchant_id"`
	Amount     int64  `json:"amount"`
}

type stored struct {
	status int
	body   []byte
}

// Handler returns the POST /payouts handler. It keeps the stored reply for each key.
func Handler(ledger Ledger) http.Handler {
	var mu sync.Mutex
	replies := map[string]stored{}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		key := r.Header.Get(keyHeader)
		if key == "" {
			http.Error(w, fmt.Sprintf("%s header is required", keyHeader), http.StatusBadRequest)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if s, ok := replies[key]; ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(s.status)
			_, _ = w.Write(s.body)
			return
		}
		var p Payout
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		id := ledger.InsertPayout(p)
		body, err := json.Marshal(response{PayoutID: id, MerchantID: p.MerchantID, Amount: p.Amount})
		if err != nil {
			http.Error(w, "encode", http.StatusInternalServerError)
			return
		}
		replies[key] = stored{status: http.StatusCreated, body: body}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(body)
	})
}
