package main

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/dashboard"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// operatorOps is GET /admin/ops (O6, R6, P4): exposes outbox queue depth, dead/in-doubt counts, dead chat messages,
// open changes, and the per-role quality scorecard without requiring SQL access.
type operatorOps struct {
	ops       ports.OpsReader
	scorecard func(ctx context.Context) (dashboard.RoleScorecard, error)
	secrets   operatorSecrets
	keyName   string
}

type opsResponse struct {
	Outbox    ports.OpsSnapshot       `json:"outbox"`
	Scorecard dashboard.RoleScorecard `json:"scorecard"`
}

func (h operatorOps) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !authorizedOperator(r, h.secrets, h.keyName) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	snap, err := h.ops.OpsSnapshot(r.Context())
	if err != nil {
		http.Error(w, "failed to read ops snapshot", http.StatusInternalServerError)
		return
	}
	var sc dashboard.RoleScorecard
	if h.scorecard != nil {
		sc, err = h.scorecard(r.Context())
		if err != nil {
			http.Error(w, "failed to read role scorecard", http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(opsResponse{Outbox: snap, Scorecard: sc})
}
