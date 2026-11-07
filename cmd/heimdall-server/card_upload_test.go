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
