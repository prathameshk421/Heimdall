package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/cardsfs"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/dsse"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/rulecard"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/secretscan"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport/mcp"
)

const (
	confirmPath    = "/confirm"
	outcomesPath   = "outcomes"
	envelopeSuffix = ".dsse.json"
)

// cardOutcomesFunc returns the owner-only outcome calibration summaries for one person's cards.
type cardOutcomesFunc func(ctx context.Context, person string, cards []rulecard.Card) ([]rulecard.CardOutcome, error)

// cardsHandler lets a mapped human manage their own personal cards: GET /cards lists them, GET /cards/<file>
// returns one, PUT /cards/<file> stores one, DELETE /cards/<file> removes one, GET/PUT /cards/<file>.dsse.json
// syncs DSSE signature sidecars, and GET /cards/outcomes returns owner-only card calibration outcomes.
// The owner is always resolved from the authenticated human bearer token, never from path or query parameters.
type cardsHandler struct {
	tokens   mcp.Tokens
	store    *cardsfs.Store
	outcomes cardOutcomesFunc
}

// newCardsHandler returns nil when no cards directory is configured, which leaves the endpoint off.
func newCardsHandler(cfg config.Config, tokens mcp.Tokens, outcomes cardOutcomesFunc) (http.Handler, error) {
	if cfg.CardsDir == "" || tokens == nil {
		return nil, nil
	}
	store, err := cardsfs.Open(cfg.CardsDir)
	if err != nil {
		return nil, err
	}
	return cardsHandler{tokens: tokens, store: store, outcomes: outcomes}, nil
}

func (h cardsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	person, ok := h.person(r)
	if !ok {
		http.Error(w, "a mapped human token is required", http.StatusUnauthorized)
		return
	}
	name := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/cards"), "/")
	if name == outcomesPath {
		h.serveOutcomes(w, r, person)
		return
	}
	if card, ok := strings.CutSuffix(name, confirmPath); ok && card != "" {
		h.confirmation(w, r, person, card)
		return
	}
	if card, ok := strings.CutSuffix(name, envelopeSuffix); ok && card != "" {
		h.envelope(w, r, person, card)
		return
	}
	h.serveCard(w, r, person, name)
}

func (h cardsHandler) serveCard(w http.ResponseWriter, r *http.Request, person, name string) {
	switch {
	case r.Method == http.MethodGet && name == "":
		h.list(w, person)
	case r.Method == http.MethodGet && name != "":
		h.read(w, r, person, name)
	case r.Method == http.MethodPut && name != "":
		h.put(w, r, person, name)
	case r.Method == http.MethodDelete && name != "":
		h.remove(w, person, name)
	default:
		http.Error(w, "use GET /cards, GET /cards/<file>, PUT /cards/<file> or DELETE /cards/<file>", http.StatusMethodNotAllowed)
	}
}

func (h cardsHandler) serveOutcomes(w http.ResponseWriter, r *http.Request, person string) {
	if r.Method != http.MethodGet {
		http.Error(w, "use GET /cards/outcomes", http.StatusMethodNotAllowed)
		return
	}
	cards, err := h.store.All(person)
	if err != nil {
		http.Error(w, "cards could not be read", http.StatusServiceUnavailable)
		return
	}
	var results []rulecard.CardOutcome
	if h.outcomes != nil {
		results, err = h.outcomes(r.Context(), person, cards)
		if err != nil {
			http.Error(w, "card outcomes unavailable", http.StatusServiceUnavailable)
			return
		}
	} else {
		results = rulecard.SummarizeOutcomes(cards, nil)
	}
	if results == nil {
		results = []rulecard.CardOutcome{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_ = json.NewEncoder(w).Encode(map[string]any{"outcomes": results})
}

func (h cardsHandler) envelope(w http.ResponseWriter, r *http.Request, person, cardName string) {
	switch r.Method {
	case http.MethodGet:
		h.readEnvelope(w, person, cardName)
	case http.MethodPut:
		h.putEnvelope(w, r, person, cardName)
	default:
		http.Error(w, "use GET or PUT /cards/<file>.dsse.json", http.StatusMethodNotAllowed)
	}
}

func (h cardsHandler) readEnvelope(w http.ResponseWriter, person, cardName string) {
	env, err := h.store.ReadEnvelope(person, cardName)
	switch {
	case errors.Is(err, cardsfs.ErrNotFound):
		http.Error(w, "no envelope for card", http.StatusNotFound)
	case err != nil:
		http.Error(w, "envelope could not be read", http.StatusBadRequest)
	default:
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_ = json.NewEncoder(w).Encode(env)
	}
}

func (h cardsHandler) putEnvelope(w http.ResponseWriter, r *http.Request, person, cardName string) {
	cardText, err := h.store.Read(person, cardName)
	if errors.Is(err, cardsfs.ErrNotFound) {
		http.Error(w, "upload the card before its DSSE envelope", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "card could not be read", http.StatusBadRequest)
		return
	}
	env, status, msg := decodeCheckedEnvelope(w, r, []byte(cardText))
	if status != 0 {
		http.Error(w, msg, status)
		return
	}
	if err := h.store.WriteEnvelope(person, cardName, env); err != nil {
		http.Error(w, "envelope could not be stored", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeCheckedEnvelope(w http.ResponseWriter, r *http.Request, cardBytes []byte) (dsse.Envelope, int, string) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, cardsfs.MaxCardBytes+1))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return dsse.Envelope{}, http.StatusRequestEntityTooLarge, "envelope is too large"
		}
		return dsse.Envelope{}, http.StatusBadRequest, "envelope body could not be read"
	}
	var env dsse.Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return dsse.Envelope{}, http.StatusBadRequest, "envelope is not valid JSON"
	}
	if env.PayloadType != dsse.PayloadTypeRuleCard || len(env.Signatures) == 0 {
		return dsse.Envelope{}, http.StatusUnprocessableEntity, "envelope must have rulecard payloadType and at least one signature"
	}
	decoded, err := base64.StdEncoding.DecodeString(env.Payload)
	if err != nil || !bytes.Equal(decoded, cardBytes) {
		return dsse.Envelope{}, http.StatusUnprocessableEntity, "envelope payload does not match stored card bytes"
	}
	return env, 0, ""
}

// confirmation lets a mapped human give their own card force, or take it back: POST /cards/<file>/confirm and DELETE
// /cards/<file>/confirm. The token picks the person, as for every card route, so nobody can confirm another person's
// card, and an agent token never reaches here. A proposed card must be accepted first.
func (h cardsHandler) confirmation(w http.ResponseWriter, r *http.Request, person, name string) {
	switch r.Method {
	case http.MethodPost:
		h.confirm(w, person, name)
	case http.MethodDelete:
		if err := h.store.Unconfirm(person, name); err != nil {
			http.Error(w, "confirmation could not be ended", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "use POST or DELETE /cards/<file>/confirm", http.StatusMethodNotAllowed)
	}
}

func (h cardsHandler) confirm(w http.ResponseWriter, person, name string) {
	text, err := h.store.Read(person, name)
	if errors.Is(err, cardsfs.ErrNotFound) {
		http.Error(w, "no such card", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "card could not be read", http.StatusBadRequest)
		return
	}
	card, err := cardsfs.Parse(name, text)
	if err != nil {
		http.Error(w, "card front matter is invalid", http.StatusUnprocessableEntity)
		return
	}
	if err := rulecard.Confirmable(card); err != nil {
		http.Error(w, "card cannot be confirmed: "+err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if err := h.store.Confirm(person, name); err != nil {
		http.Error(w, "card could not be confirmed", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h cardsHandler) person(r *http.Request) (string, bool) {
	token, ok := uploadBearer(r.Header.Get("Authorization"))
	if !ok {
		return "", false
	}
	p, err := h.tokens.Resolve(r.Context(), mcp.HashToken(token))
	if err != nil || !p.Human || p.Person == "" || p.Agent != "" {
		return "", false
	}
	return p.Person, true
}

func (h cardsHandler) list(w http.ResponseWriter, person string) {
	names, err := h.store.List(person)
	if err != nil {
		http.Error(w, "cards could not be listed", http.StatusServiceUnavailable)
		return
	}
	if names == nil {
		names = []string{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string][]string{"cards": names})
}

func (h cardsHandler) read(w http.ResponseWriter, r *http.Request, person, name string) {
	text, err := h.store.Read(person, name)
	switch {
	case errors.Is(err, cardsfs.ErrNotFound):
		http.Error(w, "no such card", http.StatusNotFound)
	case err != nil:
		http.Error(w, "card could not be read: use a plain .md, .yaml, .yml or .txt file name", http.StatusBadRequest)
	default:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, name, time.Time{}, strings.NewReader(text))
	}
}

func (h cardsHandler) put(w http.ResponseWriter, r *http.Request, person, name string) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, cardsfs.MaxCardBytes+1))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			http.Error(w, "card is too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "card body could not be read", http.StatusBadRequest)
		return
	}
	text := string(body)
	if strings.TrimSpace(text) == "" {
		http.Error(w, "card is empty", http.StatusBadRequest)
		return
	}
	if _, err := cardsfs.Parse(name, text); err != nil {
		http.Error(w, "card front matter is invalid: "+err.Error(), http.StatusBadRequest)
		return
	}
	if _, found := secretscan.RedactReport(text); len(found) > 0 {
		kinds := make([]string, 0, len(found))
		for _, f := range found {
			kinds = append(kinds, f.Kind)
		}
		http.Error(w, "card looks like it contains secrets or personal data ("+strings.Join(kinds, ", ")+"); remove them and send it again", http.StatusUnprocessableEntity)
		return
	}
	if err := h.store.Write(person, name, body); err != nil {
		http.Error(w, "card could not be stored: use a plain .md, .yaml, .yml or .txt file name", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h cardsHandler) remove(w http.ResponseWriter, person, name string) {
	err := h.store.Delete(person, name)
	switch {
	case errors.Is(err, cardsfs.ErrNotFound):
		http.Error(w, "no such card", http.StatusNotFound)
	case err != nil:
		http.Error(w, "card could not be deleted", http.StatusBadRequest)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// uploadBearer reads a bearer token from an Authorization header.
func uploadBearer(header string) (string, bool) {
	scheme, token, found := strings.Cut(header, " ")
	return token, found && strings.EqualFold(scheme, "Bearer") && token != "" && !strings.ContainsAny(token, " \t\r\n")
}
