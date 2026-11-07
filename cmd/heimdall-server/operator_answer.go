package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/commands"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// operatorSecrets reads one configured secret by name.
type operatorSecrets interface {
	Secret(name string) (string, error)
}

// operatorAnswer is POST /admin/answer. The operator of the deployment answers a question an agent asked, with the
// Switch coordinator key, the same credential /admin/agents/ takes. It exists for a deployment whose policy has no
// chat sender mapped to a person yet: the answer is recorded as question.answered by the actor "operator", exactly as
// a chat answer is, and the agent that asked is woken.
type operatorAnswer struct {
	answer  func(ctx context.Context, cid id.ChangeID, question, text string) error
	secrets operatorSecrets
	keyName string
}

const operatorAnswerLimit = 64 << 10

func (h operatorAnswer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !h.authorized(r) {
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
		Change   string `json:"change"`
		Question string `json:"question"`
		Text     string `json:"text"`
	}
	if err := dec.Decode(&in); err != nil || strings.TrimSpace(in.Change) == "" || strings.TrimSpace(in.Question) == "" || strings.TrimSpace(in.Text) == "" {
		http.Error(w, "the body must be {\"change\":\"KAN-7\",\"question\":\"Q1\",\"text\":\"...\"}", http.StatusBadRequest)
		return
	}
	if err := h.answer(r.Context(), id.ChangeID(in.Change), in.Question, in.Text); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h operatorAnswer) authorized(r *http.Request) bool {
	return authorizedOperator(r, h.secrets, h.keyName)
}

// authorizedOperator checks the bearer token against the configured operator key.
func authorizedOperator(r *http.Request, secrets operatorSecrets, keyName string) bool {
	scheme, value, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || value == "" {
		return false
	}
	want, err := secrets.Secret(keyName)
	if err != nil || want == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(value), []byte(want)) == 1
}

// OperatorAnswer records the operator's answer to a question of the change and wakes the agent that asked. A repeat of
// the same answer is recorded once.
func (r *commandRuntime) OperatorAnswer(ctx context.Context, cid id.ChangeID, question, text string) error {
	state, err := r.events.Load(ctx, cid)
	if err != nil {
		return fmt.Errorf("load change %s: %w", cid, err)
	}
	sourceRef := "operator:" + string(cid) + ":" + question
	if hasCommandSource(state, commands.SourceChat, sourceRef) {
		return errors.New("that question already has the operator's answer")
	}
	actor := change.Actor{Kind: change.ActorHuman, ID: "operator"}
	events, err := r.answerEvents(cid, state, "operator", commands.SourceChat, sourceRef, actor, commands.Answer{Question: question, Text: text})
	if err != nil {
		return err
	}
	if _, err := r.engine.Advance(ctx, cid, events); err != nil {
		return fmt.Errorf("apply the answer: %w", err)
	}
	if err := r.wakeAsker(ctx, cid, state, question, sourceRef); err != nil {
		return fmt.Errorf("answer recorded but the agent was not woken: %w", err)
	}
	return nil
}
