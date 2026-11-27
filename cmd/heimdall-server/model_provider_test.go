package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/openrouter"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/liveness"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
)

func TestAModelOfAnUnknownProviderIsRefused(t *testing.T) {
	for _, model := range []string{"", "deploy", "deploy/", "/deepseek", "acme/deepseek"} {
		_, err := modelProviderOf(config.Config{}, model)
		require.Error(t, err, model)
	}
}

type keyFiles map[string]string

func (k keyFiles) Get(name string) (string, error) { return k[name], nil }

// A resolved provider drives the probe: a key file adds the bearer header, no key file sends none, and the
// configured extra headers go out either way.
func TestTheProbeSendsTheKeyOnlyWhenAKeyFileIsSet(t *testing.T) {
	var auth, team string
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		calls++
		auth, team = r.Header.Get("Authorization"), r.Header.Get("X-Team")
	}))
	defer srv.Close()

	cfg := config.Config{ModelProviders: []config.ModelProvider{
		{Name: "local", BaseURL: srv.URL, Headers: map[string]string{"X-Team": "a"}},
		{Name: "gateway", BaseURL: srv.URL, KeyFile: "gateway_key"},
	}}
	keys := keyFiles{"gateway_key": "k-123"}

	local, err := modelProviderOf(cfg, "local/llama3")
	require.NoError(t, err)
	health, err := proberFor(local, keys).Probe(context.Background())
	require.NoError(t, err)
	require.Equal(t, liveness.Healthy, health)
	require.Empty(t, auth, "a provider with no key file gets no Authorization header")
	require.Equal(t, "a", team)

	gateway, err := modelProviderOf(cfg, "gateway/llama3")
	require.NoError(t, err)
	health, err = proberFor(gateway, keys).Probe(context.Background())
	require.NoError(t, err)
	require.Equal(t, liveness.Healthy, health)
	require.Equal(t, "Bearer k-123", auth)
	require.Empty(t, team)
	require.Equal(t, 2, calls)
}

var _ openrouter.Secrets = keyFiles{}
