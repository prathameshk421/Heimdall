package main

import (
	"fmt"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/openrouter"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
)

// modelProvider is where the liveness probe asks for one token: the provider's API root, the secret file that holds
// the server's key for it (empty when the endpoint takes no key), the extra request headers, and the model id
// without the provider prefix.
type modelProvider struct {
	url     string
	keyFile string
	headers map[string]string
	model   string
}

// modelProviderOf reads the agents' model id (`agent_runtime.model`, `<provider>/<model>`) and names the provider the
// probe must ask. The prefix is looked up in the configured [[model_providers]], then in the two shipped defaults
// (`deploy`, `openrouter`). An unknown provider is an error at start-up: a probe of another provider would say
// nothing about the agents.
func modelProviderOf(cfg config.Config, agentModel string) (modelProvider, error) {
	prefix, model, ok := strings.Cut(agentModel, "/")
	if !ok || prefix == "" || model == "" {
		return modelProvider{}, fmt.Errorf("agent model %q is not <provider>/<model>", agentModel)
	}
	p, known := cfg.ModelProvider(prefix)
	if !known {
		return modelProvider{}, fmt.Errorf("agent model %q: provider %q is not configured (known providers: %s); add a [[model_providers]] entry named %q",
			agentModel, prefix, strings.Join(cfg.ModelProviderNames(), ", "), prefix)
	}
	return modelProvider{url: p.BaseURL, keyFile: p.KeyFile, headers: p.Headers, model: model}, nil
}

// proberFor builds the liveness probe for a resolved provider. It sends a key only when the provider names a key file.
func proberFor(p modelProvider, secrets openrouter.Secrets) openrouter.Prober {
	return openrouter.Prober{BaseURL: p.url, Secrets: secrets, KeyName: p.keyFile, Headers: p.headers, Model: p.model}
}
