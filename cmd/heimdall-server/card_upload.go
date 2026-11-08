package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/cardsfs"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/rulecard"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/pack"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport/mcp"
)

// cardsHandler lets a mapped human manage their own personal cards: GET /cards lists them, GET /cards/<file>
// returns one, PUT /cards/<file>
// stores one, DELETE /cards/<file> removes one. The folder is chosen by the bearer token, never by the path, so a
// person can touch only their own cards; an agent token is refused, because agents read cards but never write them.
// The server checks every card itself (size, front matter, secrets and personal data) whatever the client did.
type cardsHandler struct {
	tokens mcp.Tokens
	store  *cardsfs.Store
}

// newCardsHandler returns nil when no cards directory is configured, which leaves the endpoint off.
func newCardsHandler(cfg config.Config, tokens mcp.Tokens) (http.Handler, error) {
	if cfg.CardsDir == "" || tokens == nil {
		return nil, nil
	}
	store, err := cardsfs.Open(cfg.CardsDir)
	if err != nil {
		return nil, err
	}
	return cardsHandler{tokens: tokens, store: store}, nil
}

func (h cardsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	person, ok := h.person(r)
	if !ok {
		http.Error(w, "a mapped human token is required", http.StatusUnauthorized)
		return
	}
	name := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/cards"), "/")
	if card, ok := strings.CutSuffix(name, confirmPath); ok && card != "" {
		h.confirmation(w, r, person, card)
		return
	}
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

// confirmPath ends the address of a card's confirmation.
const confirmPath = "/confirm"

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
	if _, found := pack.RedactReport(text); len(found) > 0 {
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
