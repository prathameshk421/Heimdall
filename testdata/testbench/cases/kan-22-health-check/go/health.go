// Package health serves GET /healthz (KAN-22).
package health

import (
	"context"
	"net/http"
)

// DB is the database handle given to the handler. The endpoint never uses it.
type DB interface {
	Ping(ctx context.Context) error
}

// Handler serves GET /healthz with 200 and the body OK, and 405 for any other method.
func Handler(db DB) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})
}
