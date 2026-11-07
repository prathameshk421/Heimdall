package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

// operatorRoom is POST /admin/rooms. The operator, with the same bearer key as /admin/answer, makes a standing
// channel on one of the chat bridges (a Discord or Mattermost channel for the test or review story) and gets its room
// id back, to list under [[feeds]] in heimdall.toml.
type operatorRoom struct {
	create  func(ctx context.Context, bridgeID, name, description string) (change.RoomID, error)
	secrets operatorSecrets
	keyName string
}

func (h operatorRoom) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
		BridgeID    string `json:"bridge_id"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := dec.Decode(&in); err != nil || strings.TrimSpace(in.BridgeID) == "" || strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Description) == "" {
		http.Error(w, "the body must be {\"bridge_id\":\"...\",\"name\":\"heimdall-tests\",\"description\":\"...\"}", http.StatusBadRequest)
		return
	}
	room, err := h.create(r.Context(), in.BridgeID, in.Name, in.Description)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"room_id": string(room)})
}
