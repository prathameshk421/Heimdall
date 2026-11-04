// Package api maps HTTP requests onto the services. It owns the wire format; the rules live in the services.
package api

import (
	"encoding/json"
	"net/http"

	"github.com/aarush323/heimdall-fixture-pay/internal/payments"
	"github.com/aarush323/heimdall-fixture-pay/internal/webhooks"
)

// merchantHeader names the calling merchant. Every request carries it.
const merchantHeader = "X-Merchant-ID"

// maxBodyBytes bounds a request body; every request here is a few hundred bytes.
const maxBodyBytes = 1 << 16

// Deps are the services the routes serve.
type Deps struct {
	Payments *payments.Service
	Webhooks *webhooks.Service
}

// Routes returns the handler for the payments and webhooks API.
func Routes(d Deps) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /payments", createPayment(d.Payments))
	mux.HandleFunc("POST /webhooks/subscriptions", createSubscription(d.Webhooks))
	mux.HandleFunc("GET /webhooks/subscriptions", listSubscriptions(d.Webhooks))
	mux.HandleFunc("DELETE /webhooks/subscriptions/{id}", deleteSubscription(d.Webhooks))
	mux.HandleFunc("GET /webhooks/deliveries", listDeliveries(d.Webhooks))
	return mux
}

// errorJSON is the body of every rejected request.
type errorJSON struct {
	Error string `json:"error"`
}

// writeJSON writes v as the JSON body of a response with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// A failed write means the client has gone away, so there is nothing left to report to it.
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes a rejected request as errorJSON.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorJSON{Error: msg})
}

// requireMerchant returns the calling merchant, or answers 401 and reports false.
func requireMerchant(w http.ResponseWriter, r *http.Request) (string, bool) {
	merchant := r.Header.Get(merchantHeader)
	if merchant == "" {
		writeError(w, http.StatusUnauthorized, "missing X-Merchant-ID header")
		return "", false
	}
	return merchant, true
}
