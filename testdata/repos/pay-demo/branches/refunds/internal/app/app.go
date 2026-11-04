// Package app wires the services behind HTTP. New is the test seam (D39): tests reach the service only through it.
package app

import (
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/aarush323/heimdall-fixture-pay/internal/api"
	"github.com/aarush323/heimdall-fixture-pay/internal/payments"
	"github.com/aarush323/heimdall-fixture-pay/internal/refunds"
)

// Config holds the injectable parts of the service. A nil field means the real clock or sequential ids.
type Config struct {
	Now   func() time.Time
	NewID func() string
}

// New returns the HTTP handler of the payments and refunds services over a fresh in-memory store.
func New(cfg Config) http.Handler {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	newID := cfg.NewID
	if newID == nil {
		newID = sequentialIDs()
	}
	store := payments.NewMemoryStore()
	pays := payments.NewService(store, now, newID)
	refs := refunds.NewService(store, now, newID)
	return api.Routes(pays, refs)
}

// sequentialIDs returns an id generator that counts 1, 2, 3 and so on. Ids only need to be unique in one process.
func sequentialIDs() func() string {
	var n atomic.Uint64
	return func() string {
		return "id_" + strconv.FormatUint(n.Add(1), 10)
	}
}
