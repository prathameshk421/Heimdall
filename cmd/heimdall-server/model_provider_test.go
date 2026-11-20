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

// The liveness probe asks the provider the agents really use (H11: the VM agents run on Deploy, and a probe of
// OpenRouter with an OpenRouter key said nothing about them).
func TestTheProbeTargetsTheAgentsModelProvider(t *testing.T) {
	deploy, err := modelProviderOf(config.Config{}, "deploy/deepseek-ai/deepseek-v4.1-flash-1017")
	require.NoError(t, err)
	require.Equal(t, modelProvider{
		url: "https://api.deploy.com/api/v1", keyFile: "deploy_api_key", model: "deepseek-ai/deepseek-v4.1-flash-1017",
	}, deploy)

	openRouter, err := modelProviderOf(config.Config{}, "openrouter/deepseek/deepseek-v4-flash")
	require.NoError(t, err)
	require.Equal(t, modelProvider{
		url: "https://openrouter.ai/api/v1", keyFile: "openrouter_api_key", model: "deepseek/deepseek-v4-flash",
	}, openRouter)
}

func TestAConfiguredProviderIsResolvedByItsPrefix(t *testing.T) {
	cfg := config.Config{ModelProviders: []config.ModelProvider{
		{Name: "ollama", BaseURL: "http://127.0.0.1:11434/v1"},
		{Name: "litellm", BaseURL: "https://llm.example.test/v1", KeyFile: "litellm_key", Headers: map[string]string{"X-Team": "a"}},
	}}
	got, err := modelProviderOf(cfg, "ollama/qwen2.5-coder:14b")
	require.NoError(t, err)
	require.Equal(t, modelProvider{url: "http://127.0.0.1:11434/v1", model: "qwen2.5-coder:14b"}, got)

	got, err = modelProviderOf(cfg, "litellm/gpt-4o")
	require.NoError(t, err)
	require.Equal(t, modelProvider{url: "https://llm.example.test/v1", keyFile: "litellm_key", headers: map[string]string{"X-Team": "a"}, model: "gpt-4o"}, got)
}

func TestAModelOfAnUnknownProviderIsRefused(t *testing.T) {
	for _, model := range []string{"", "deploy", "deploy/", "/deepseek", "acme/deepseek"} {
		_, err := modelProviderOf(config.Config{}, model)
		require.Error(t, err, model)
	}
}

func TestTheUnknownProviderErrorNamesTheConfiguredProviders(t *testing.T) {
	cfg := config.Config{ModelProviders: []config.ModelProvider{{Name: "ollama", BaseURL: "http://127.0.0.1:11434/v1"}}}
	_, err := modelProviderOf(cfg, "acme/deepseek")
	require.ErrorContains(t, err, `provider "acme"`)
	require.ErrorContains(t, err, "ollama")
	require.ErrorContains(t, err, "deploy")
	require.ErrorContains(t, err, "openrouter")
	require.ErrorContains(t, err, "[[model_providers]]")
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
