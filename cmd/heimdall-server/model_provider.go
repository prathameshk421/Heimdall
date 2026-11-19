package main

import (
	"fmt"
	"strings"
)

// modelProvider is where the liveness probe asks for one token: the provider's API root, the secret file that holds
// the server's key for it, and the model id without the provider prefix.
type modelProvider struct {
	url     string
	keyFile string
	model   string
}

// knownProvider maps the provider prefix of an agent model id (`agent_runtime.model`, `<provider>/<model>`) to its
// OpenAI-compatible API root and key file. The keys are the files the deploy already ships (`deploy/compose.yaml`).
func knownProvider(prefix string) (modelProvider, bool) {
	switch prefix {
	case "deploy":
		return modelProvider{url: "https://api.deploy.com/api/v1", keyFile: "deploy_api_key"}, true
	case "openrouter":
		return modelProvider{url: "https://openrouter.ai/api/v1", keyFile: "openrouter_api_key"}, true
	default:
		return modelProvider{}, false
	}
}

// modelProviderOf reads the agents' model id and names the provider the probe must ask. An unknown provider is an
// error at start-up: a probe of another provider would say nothing about the agents.
func modelProviderOf(agentModel string) (modelProvider, error) {
	prefix, model, ok := strings.Cut(agentModel, "/")
	if !ok || prefix == "" || model == "" {
		return modelProvider{}, fmt.Errorf("agent model %q is not <provider>/<model>", agentModel)
	}
	p, known := knownProvider(prefix)
	if !known {
		return modelProvider{}, fmt.Errorf("agent model %q: the liveness probe does not know provider %q", agentModel, prefix)
	}
	p.model = model
	return p, nil
}
