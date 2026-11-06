package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	switchchat "github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/switch"
)

// coordinatorRegistrar registers the coordinator agent with Switch. It returns the agent id and the API key that Switch
// issues once. The Switch client is adapted to it by switchRegistrar.
type coordinatorRegistrar interface {
	RegisterCoordinator(ctx context.Context, agentType, name, description string) (agentID, apiKey string, err error)
}

// switchRegistrar adapts the Switch client to coordinatorRegistrar.
type switchRegistrar struct{ client *switchchat.Client }

func (s switchRegistrar) RegisterCoordinator(ctx context.Context, agentType, name, description string) (string, string, error) {
	reg, err := s.client.RegisterCoordinator(ctx, agentType, name, description)
	if err != nil {
		return "", "", err
	}
	return reg.AgentID, reg.APIKey.Value(), nil
}

// registerCoordinator registers the coordinator, stores its API key under keySecret in secretsDir, and returns only the
// agent id. The key never reaches the caller's output (WP1 step 2).
func registerCoordinator(ctx context.Context, r coordinatorRegistrar, secretsDir, keySecret, name string, store func(dir, name string, value []byte) error) (string, error) {
	if secretsDir == "" || keySecret == "" {
		return "", errors.New("switch register: the secrets directory and the key file name are required")
	}
	agentID, apiKey, err := r.RegisterCoordinator(ctx, "opencode", name, "Heimdall coordinator: posts change facts and reads commands in change rooms.")
	if err != nil {
		return "", fmt.Errorf("switch register: %w", err)
	}
	if agentID == "" || apiKey == "" {
		return "", errors.New("switch register: Switch returned no agent id or key")
	}
	if err := store(secretsDir, keySecret, []byte(apiKey)); err != nil {
		return "", fmt.Errorf("switch register: store the API key: %w", err)
	}
	return agentID, nil
}

// writeRegistrationResult prints the agent id and the file the key went to. It never prints the key.
func writeRegistrationResult(w io.Writer, agentID, keySecret string) error {
	_, err := fmt.Fprintf(w, "registered coordinator agent id %s; API key stored in %s\n", agentID, keySecret)
	return err
}
