package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The liveness probe asks the provider the agents really use (H11: the VM agents run on Deploy, and a probe of
// OpenRouter with an OpenRouter key said nothing about them).
func TestTheProbeTargetsTheAgentsModelProvider(t *testing.T) {
	deploy, err := modelProviderOf("deploy/deepseek-ai/deepseek-v4.1-flash-1017")
	require.NoError(t, err)
	require.Equal(t, modelProvider{
		url: "https://api.deploy.com/api/v1", keyFile: "deploy_api_key", model: "deepseek-ai/deepseek-v4.1-flash-1017",
	}, deploy)

	openRouter, err := modelProviderOf("openrouter/deepseek/deepseek-v4-flash")
	require.NoError(t, err)
	require.Equal(t, modelProvider{
		url: "https://openrouter.ai/api/v1", keyFile: "openrouter_api_key", model: "deepseek/deepseek-v4-flash",
	}, openRouter)
}

func TestAModelOfAnUnknownProviderIsRefused(t *testing.T) {
	for _, model := range []string{"", "deploy", "deploy/", "/deepseek", "acme/deepseek"} {
		_, err := modelProviderOf(model)
		require.Error(t, err, model)
	}
}
