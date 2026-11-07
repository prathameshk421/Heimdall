package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/commands"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// operatorReject is POST /admin/reject. The operator closes a change with the same bearer key /admin/answer takes.
// It records the same decision.recorded a chat "heimdall reject" records, as actor "operator". It exists for a
// change cut from a base whose policy maps no chat sender to an approver.
type operatorReject struct {
	reject  func(ctx context.Context, cid id.ChangeID, reason string) error
	secrets operatorSecrets
	keyName string
}

func (h operatorReject) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !authorizedOperator(r, h.secrets, h.keyName) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, operatorAnswerLimit)
	defer func() { _ = r.Body.Close() }()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var in struct {
		Change string `json:"change"`
		Reason string `json:"reason"`
	}
	if err := dec.Decode(&in); err != nil || strings.TrimSpace(in.Change) == "" || strings.TrimSpace(in.Reason) == "" {
		http.Error(w, "the body must be {\"change\":\"KAN-9\",\"reason\":\"...\"}", http.StatusBadRequest)
		return
	}
	if err := h.reject(r.Context(), id.ChangeID(in.Change), in.Reason); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// OperatorReject records the operator's rejection of a change. A repeat is refused, so one change gets one decision.
func (r *commandRuntime) OperatorReject(ctx context.Context, cid id.ChangeID, reason string) error {
	state, err := r.events.Load(ctx, cid)
	if err != nil {
		return fmt.Errorf("load change %s: %w", cid, err)
	}
	if len(state) == 0 {
		return fmt.Errorf("no change %s", cid)
	}
	sourceRef := "operator:reject:" + string(cid)
	if hasCommandSource(state, commands.SourceChat, sourceRef) {
		return errors.New("that change is already rejected by the operator")
	}
	actor := change.Actor{Kind: change.ActorHuman, ID: "operator"}
	events, err := r.decisionEvents(cid, state, "operator", commands.SourceChat, sourceRef, actor, commands.Reject{Reason: reason})
	if err != nil {
		return err
	}
	if _, err := r.engine.Advance(ctx, cid, events); err != nil {
		return fmt.Errorf("apply the rejection: %w", err)
	}
	return nil
}
