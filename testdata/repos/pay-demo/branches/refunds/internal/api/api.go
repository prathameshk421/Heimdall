// Package api maps HTTP requests onto the services. It owns the wire format; the rules live in the services.
package api

import (
	"encoding/json"
	"net/http"

	"github.com/aarush323/heimdall-fixture-pay/internal/payments"
	"github.com/aarush323/heimdall-fixture-pay/internal/refunds"
)

// merchantHeader names the calling merchant. Every request carries it.
const merchantHeader = "X-Merchant-ID"

// maxBodyBytes bounds a request body; a payment or refund request is a few dozen bytes.
const maxBodyBytes = 1 << 16

// Routes returns the handler for the payments and refunds API.
func Routes(pays *payments.Service, refs *refunds.Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /payments", createPayment(pays))
	mux.HandleFunc("POST /refunds", createRefund(refs))
	mux.HandleFunc("GET /refunds/{id}", getRefund(refs))
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
