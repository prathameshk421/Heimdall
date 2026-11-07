package main

import (
	"context"
	"errors"
	"fmt"
)

// coordinatorRegistrar registers the Heimdall coordinator with Switch. The key comes back as a plain string so a test
// can fake the call. The production adapter wraps the Switch client.
type coordinatorRegistrar interface {
	Register(ctx context.Context, agentType, name, description string) (agentID, apiKey string, err error)
}

// registerCoordinator registers the coordinator, stores the returned key through store, and returns only the agent id.
// The key never reaches a return value, an error or a log line.
func registerCoordinator(ctx context.Context, reg coordinatorRegistrar, store func(name string, value []byte) error, apiKeySecret, agentType, name string) (string, error) {
	agentID, apiKey, err := reg.Register(ctx, agentType, name, "Heimdall coordinator")
	if err != nil {
		return "", fmt.Errorf("register coordinator: %w", err)
	}
	if err := store(apiKeySecret, []byte(apiKey)); err != nil {
		return "", errors.New("store coordinator key: write failed")
	}
	return agentID, nil
}
