// Package exchange serves POST /exchange (KAN-35).
package exchange

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
)

// Rates gives the rate for a pair: units of to per one unit of from.
type Rates interface {
	Rate(from, to string) (float64, bool)
}

type request struct {
	From   string  `json:"from"`
	To     string  `json:"to"`
	Amount float64 `json:"amount"`
}

func supported(code string) bool {
	switch code {
	case "EUR", "USD", "GBP", "JPY", "CHF":
		return true
	}
	return false
}

// convert returns amount*rate with exactly 4 decimal places, ties rounded to even.
func convert(amount, rate float64) string {
	rounded := math.RoundToEven(amount*rate*1e12) / 1e12
	return strconv.FormatFloat(rounded, 'f', -1, 64)
}

// Handler serves POST /exchange.
func Handler(rates Rates) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if !supported(req.From) || !supported(req.To) {
			http.Error(w, "unsupported currency", http.StatusBadRequest)
			return
		}
		rate, ok := rates.Rate(req.From, req.To)
		if !ok {
			http.Error(w, "no rate for pair", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"converted": convert(req.Amount, rate)})
	})
}
