package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
)

// Checker is one named readiness check. Check returns nil when the dependency is healthy.
type Checker struct {
	Name  string
	Check func(ctx context.Context) error
}

const (
	statusOK          = "ok"
	statusUnavailable = "unavailable"
)

// healthBody is the /healthz response. Failing is omitted when every check passes.
type healthBody struct {
	Status  string   `json:"status"`
	Failing []string `json:"failing,omitempty"`
}

// NewRouter returns the inbound HTTP handler. It serves GET /healthz from the given checkers. Names must
// be non-empty and unique, and every Check must be set; otherwise NewRouter refuses to build the router.
// The logger is injected: the package never uses the process-wide default logger.
func NewRouter(logger *slog.Logger, checkers ...Checker) (http.Handler, error) {
	if logger == nil {
		return nil, errors.New("router logger must not be nil")
	}
	seen := make(map[string]bool, len(checkers))
	for _, c := range checkers {
		switch {
		case c.Name == "":
			return nil, errors.New("health checker name must not be empty")
		case c.Check == nil:
			return nil, fmt.Errorf("health checker %q has a nil Check", c.Name)
		case seen[c.Name]:
			return nil, fmt.Errorf("duplicate health checker %q", c.Name)
		}
		seen[c.Name] = true
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz(logger, slices.Clone(checkers)))
	return mux, nil
}

func healthz(logger *slog.Logger, checkers []Checker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var failing []string
		for _, c := range checkers {
			if err := c.Check(r.Context()); err != nil {
				failing = append(failing, c.Name)
				// The error text is not logged here: a checker may hold a credential in it. The checker
				// logs its own redacted detail; this line records only which check failed.
				logger.WarnContext(r.Context(), "health check failed", "check", c.Name)
			}
		}
		if len(failing) == 0 {
			writeJSON(w, http.StatusOK, healthBody{Status: statusOK})
			return
		}
		writeJSON(w, http.StatusServiceUnavailable, healthBody{Status: statusUnavailable, Failing: failing})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// A failed write means the client has gone away; there is no one left to tell.
	_ = json.NewEncoder(w).Encode(v)
}
