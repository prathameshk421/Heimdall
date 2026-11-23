package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/cardsfs"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/dsse"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/rulecard"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport/mcp"
)

func TestCardsDSSEAndOwnerOnlyOutcomes(t *testing.T) {
	dir := t.TempDir()
	store, err := cardsfs.Open(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	var seenPerson string
	tokens := cardTokens{
		mcp.HashToken("bob-token"):   {Person: "bob", Human: true},
		mcp.HashToken("agent-token"): {Agent: "bob-reviewer", Role: "reviewer"},
	}
	h := cardsHandler{
		tokens: tokens,
		store:  store,
		outcomes: func(_ context.Context, person string, cards []rulecard.Card) ([]rulecard.CardOutcome, error) {
			seenPerson = person
			return rulecard.SummarizeOutcomes(cards, map[string]rulecard.OutcomeCounts{
				"rule.md": {Kept: 3, Proven: 2},
			}), nil
		},
	}

	cardBody := "---\napplies_to: [\"internal/pay/**\"]\ncheck: \"Holds?\"\nkind: invariant\n---\nA rule.\n"
	require.Equal(t, http.StatusNoContent, doCards(h, http.MethodPut, "/cards/rule.md", "bob-token", cardBody).Code)

	// DSSE sidecar upload validates exact payload bytes
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	badEnv, err := dsse.Sign(dsse.PayloadTypeRuleCard, []byte("different bytes"), "bob-1", priv)
	require.NoError(t, err)
	badJSON, _ := json.Marshal(badEnv)
	require.Equal(t, http.StatusUnprocessableEntity, doCards(h, http.MethodPut, "/cards/rule.md.dsse.json", "bob-token", string(badJSON)).Code)

	goodEnv, err := dsse.Sign(dsse.PayloadTypeRuleCard, []byte(cardBody), "bob-1", priv)
	require.NoError(t, err)
	goodJSON, _ := json.Marshal(goodEnv)
	require.Equal(t, http.StatusNoContent, doCards(h, http.MethodPut, "/cards/rule.md.dsse.json", "bob-token", string(goodJSON)).Code)
	gotEnv := doCards(h, http.MethodGet, "/cards/rule.md.dsse.json", "bob-token", "")
	require.Equal(t, http.StatusOK, gotEnv.Code)
	require.Contains(t, gotEnv.Body.String(), `"keyid":"bob-1"`)

	// Owner-only outcomes: agent and unauthenticated tokens refused; query params ignored
	require.Equal(t, http.StatusUnauthorized, doCards(h, http.MethodGet, "/cards/outcomes", "agent-token", "").Code)
	require.Equal(t, http.StatusUnauthorized, doCards(h, http.MethodGet, "/cards/outcomes", "", "").Code)
	rec := doCards(h, http.MethodGet, "/cards/outcomes?person=alice", "bob-token", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "bob", seenPerson, "owner must come strictly from bearer token, never query params")
	require.Contains(t, rec.Body.String(), `"card_name":"rule.md"`)
	require.Contains(t, rec.Body.String(), `"status":"strong"`)
}
