package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/cardsfs"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport/mcp"
)

type cardTokens map[string]mcp.Principal

func (t cardTokens) Resolve(_ context.Context, hash string) (mcp.Principal, error) {
	p, ok := t[hash]
	if !ok {
		return mcp.Principal{}, ports.ErrUnauthorized
	}
	return p, nil
}

func cardsServer(t *testing.T) (http.Handler, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := cardsfs.Open(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	tokens := cardTokens{
		mcp.HashToken("bob-token"):   {Person: "bob", Human: true},
		mcp.HashToken("agent-token"): {Agent: "bob-reviewer", Role: "reviewer"},
	}
	return cardsHandler{tokens: tokens, store: store}, dir
}

func doCards(h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestCardsUploadStoresUnderTheTokensPersonAndListsOnlyTheirs(t *testing.T) {
	h, dir := cardsServer(t)
	require.Equal(t, http.StatusNoContent, doCards(h, http.MethodPut, "/cards/rules.md", "bob-token", "---\napplies_to: [\"server.js\"]\n---\nPUT and DELETE on an unknown id answer 404.").Code)
	got, err := os.ReadFile(filepath.Join(dir, "bob", "rules.md"))
	require.NoError(t, err)
	require.Contains(t, string(got), "answer 404")
	list := doCards(h, http.MethodGet, "/cards", "bob-token", "")
	require.Equal(t, http.StatusOK, list.Code)
	require.Contains(t, list.Body.String(), "rules.md")
	require.Equal(t, http.StatusNoContent, doCards(h, http.MethodDelete, "/cards/rules.md", "bob-token", "").Code)
	require.Equal(t, http.StatusNotFound, doCards(h, http.MethodDelete, "/cards/rules.md", "bob-token", "").Code)
}

func TestCardsUploadRefusesAnAgentTokenNoTokenAndTraversal(t *testing.T) {
	h, dir := cardsServer(t)
	require.Equal(t, http.StatusUnauthorized, doCards(h, http.MethodPut, "/cards/a.md", "", "x").Code)
	require.Equal(t, http.StatusUnauthorized, doCards(h, http.MethodPut, "/cards/a.md", "wrong", "x").Code)
	require.Equal(t, http.StatusUnauthorized, doCards(h, http.MethodPut, "/cards/a.md", "agent-token", "x").Code, "an agent can read cards through MCP but never write them")
	for _, path := range []string{"/cards/../x.md", "/cards/a/b.md", "/cards/x.sh", "/cards/"} {
		rec := doCards(h, http.MethodPut, path, "bob-token", "x")
		require.GreaterOrEqual(t, rec.Code, 400, path)
	}
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestCardsUploadRefusesSecretsBrokenFrontMatterAndOversizeBodies(t *testing.T) {
	h, _ := cardsServer(t)
	secret := doCards(h, http.MethodPut, "/cards/a.md", "bob-token", "deploy with AKIAABCDEFGHIJKLMNOP and mail bob@example.com")
	require.Equal(t, http.StatusUnprocessableEntity, secret.Code)
	require.Contains(t, secret.Body.String(), "cloud_key")
	require.NotContains(t, secret.Body.String(), "AKIAABCDEFGHIJKLMNOP", "the response never echoes the matched text")
	require.Equal(t, http.StatusBadRequest, doCards(h, http.MethodPut, "/cards/a.md", "bob-token", "---\napplies_to: [oops\n---\nbody").Code)
	require.Equal(t, http.StatusRequestEntityTooLarge, doCards(h, http.MethodPut, "/cards/a.md", "bob-token", strings.Repeat("x", cardsfs.MaxCardBytes+10)).Code)
	require.Equal(t, http.StatusBadRequest, doCards(h, http.MethodPut, "/cards/a.md", "bob-token", "   \n").Code, "an empty card is refused")
}

func TestCardsUploadReturnsOnlyTheTokensOwnCardText(t *testing.T) {
	h, dir := cardsServer(t)
	card := "---\nstatus: proposed\n---\nrefunds must count earlier refunds"
	require.Equal(t, http.StatusNoContent, doCards(h, http.MethodPut, "/cards/r.md", "bob-token", card).Code)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "alice"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "alice", "secret.md"), []byte("alice only"), 0o600))

	got := doCards(h, http.MethodGet, "/cards/r.md", "bob-token", "")
	require.Equal(t, http.StatusOK, got.Code)
	require.Equal(t, card, got.Body.String())
	require.Equal(t, http.StatusNotFound, doCards(h, http.MethodGet, "/cards/secret.md", "bob-token", "").Code, "another person's file is not visible")
	require.Equal(t, http.StatusBadRequest, doCards(h, http.MethodGet, "/cards/../alice/secret.md", "bob-token", "").Code)
	require.Equal(t, http.StatusUnauthorized, doCards(h, http.MethodGet, "/cards/r.md", "agent-token", "").Code)
	require.Equal(t, http.StatusUnauthorized, doCards(h, http.MethodGet, "/cards/r.md", "", "").Code)
}

func TestOnlyTheHumanOwnerConfirmsAScopedCheckableCard(t *testing.T) {
	h, _ := cardsServer(t)
	good := "---\napplies_to: [\"internal/pay/**\"]\ncheck: \"Holds?\"\nkind: invariant\n---\nA rule.\n"
	require.Equal(t, http.StatusNoContent, doCards(h, http.MethodPut, "/cards/rule.md", "bob-token", good).Code)
	require.Equal(t, http.StatusUnauthorized, doCards(h, http.MethodPost, "/cards/rule.md/confirm", "agent-token", "").Code, "an agent never confirms a card")
	require.Equal(t, http.StatusUnauthorized, doCards(h, http.MethodPost, "/cards/rule.md/confirm", "", "").Code)
	require.Equal(t, http.StatusNotFound, doCards(h, http.MethodPost, "/cards/none.md/confirm", "bob-token", "").Code)
	require.Equal(t, http.StatusNoContent, doCards(h, http.MethodPost, "/cards/rule.md/confirm", "bob-token", "").Code)
	require.Equal(t, http.StatusNoContent, doCards(h, http.MethodDelete, "/cards/rule.md/confirm", "bob-token", "").Code)
	require.Equal(t, http.StatusMethodNotAllowed, doCards(h, http.MethodPut, "/cards/rule.md/confirm", "bob-token", "forged").Code)

	for name, text := range map[string]string{
		"general.md":  "---\ncheck: \"Holds?\"\nkind: invariant\n---\nEvery change.\n",
		"nocheck.md":  "---\napplies_to: [\"a/**\"]\nkind: invariant\n---\nVague.\n",
		"proposed.md": "---\napplies_to: [\"a/**\"]\ncheck: \"Holds?\"\nkind: judgement\nstatus: proposed\n---\nLearned.\n",
	} {
		require.Equal(t, http.StatusNoContent, doCards(h, http.MethodPut, "/cards/"+name, "bob-token", text).Code, name)
		require.Equal(t, http.StatusUnprocessableEntity, doCards(h, http.MethodPost, "/cards/"+name+"/confirm", "bob-token", "").Code, name)
	}
}
