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
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// operatorRecheck asks the server to repeat one finding's evidence on the current head. It exists for a run that gave
// a wrong answer: on KAN-23 a busy runner dropped four true findings to E4, and the verifier does not run twice on one
// head without a recorded request.
type operatorRecheck struct {
	recheck func(ctx context.Context, cid id.ChangeID, ref, reason string) error
	secrets operatorSecrets
	keyName string
}

func (h operatorRecheck) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
		Change  string `json:"change"`
		Finding string `json:"finding"`
		Reason  string `json:"reason"`
	}
	if err := dec.Decode(&in); err != nil || strings.TrimSpace(in.Change) == "" || strings.TrimSpace(in.Finding) == "" ||
		strings.TrimSpace(in.Reason) == "" {
		http.Error(w, "the body must be {\"change\":\"KAN-23\",\"finding\":\"F4\",\"reason\":\"...\"}", http.StatusBadRequest)
		return
	}
	if err := h.recheck(r.Context(), id.ChangeID(in.Change), in.Finding, in.Reason); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// OperatorRecheck records a recheck request for one finding of a change. The verify step then repeats the finding's
// evidence on the current head, and the new result replaces the old one. A finding that is not filed, or one whose
// evidence the server cannot repeat, is refused here, so nothing is recorded for it.
func (r *commandRuntime) OperatorRecheck(ctx context.Context, cid id.ChangeID, ref, reason string) error {
	state, err := r.events.Load(ctx, cid)
	if err != nil {
		return fmt.Errorf("load change %s: %w", cid, err)
	}
	if len(state) == 0 {
		return fmt.Errorf("no change %s", cid)
	}
	record, ok, err := latestFindingRecord(state, ref)
	if err != nil {
		return fmt.Errorf("read finding %s: %w", ref, err)
	}
	if !ok {
		return fmt.Errorf("change %s has no finding %s", cid, ref)
	}
	filed, err := filedTier(state, record.FindingID)
	if err != nil {
		return err
	}
	if filed != finding.TierE1 && filed != finding.TierE2 && filed != finding.TierE3 {
		return fmt.Errorf("finding %s was filed as %s, so the server has no evidence to repeat", ref, filed)
	}
	sourceRef := "operator:recheck:" + string(cid) + ":" + ref + ":" + r.clock.Now().UTC().Format("20060102T150405Z")
	ev, err := r.event(cid, change.Actor{Kind: change.ActorHuman, ID: "operator"}, commands.SourceChat,
		change.EventFindingRecheckRequested,
		change.FindingRecheckRequested{FindingID: record.FindingID, Ref: record.Ref, Reason: reason, By: "operator"}, sourceRef)
	if err != nil {
		return err
	}
	if _, err := r.engine.Advance(ctx, cid, []change.Event{ev}); err != nil {
		return fmt.Errorf("record the recheck of %s: %w", ref, err)
	}
	return nil
}

// filedTier returns the tier a finding was filed with: the evidence it carries, which a later downgrade does not remove.
func filedTier(events []change.Event, fndID id.FindingID) (finding.Tier, error) {
	for _, ev := range events {
		if ev.Type != change.EventFindingFiled {
			continue
		}
		payload, err := change.Decode(ev)
		if err != nil {
			return "", fmt.Errorf("decode %s: %w", ev.Type, err)
		}
		if filed, ok := payload.(change.FindingFiled); ok && filed.FindingID == fndID {
			return filed.Tier, nil
		}
	}
	return "", errors.New("the finding has no finding.filed event")
}
