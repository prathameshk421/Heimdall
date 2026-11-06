package main

import (
	"context"
	"fmt"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/registry"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
)

func configuredIntegrationChecks(
	ctx context.Context,
	cfg config.Config,
	buildRegistry func() (*registry.Registry, error),
	deadline time.Duration,
) []checkResult {
	reg, err := buildRegistry()
	if err != nil {
		return []checkResult{result("integration.registry", statusFail, "cannot load integration registry")}
	}
	if err := registry.ValidateSelections(cfg, reg); err != nil {
		return []checkResult{result("integration.registry", statusFail, fmt.Sprintf("configured provider selection is invalid: %s", err))}
	}
	var results []checkResult
	for _, kind := range []registry.Kind{registry.KindVCS, registry.KindTracker, registry.KindChat} {
		if _, selected := cfg.Selection(string(kind)); !selected {
			results = append(results, result("integration."+string(kind), statusMissing, registry.SelectionField(kind)+" is required; run heimdall connect to choose an integration"))
		}
	}
	for _, kind := range registry.Kinds() {
		name, selected := cfg.Selection(string(kind))
		if !selected {
			continue
		}
		entry, err := reg.Select(kind, name)
		if err != nil {
			results = append(results, result("integration."+string(kind)+"."+name, statusFail, "configured integration is not registered"))
			continue
		}
		check := checkWithDeadline(ctx, entry, cfg, deadline)
		status := statusFail
		switch check.Status {
		case registry.StatusConnected:
			status = statusOK
		case registry.StatusNeedsConfig:
			status = statusMissing
		case registry.StatusUnreachable:
			status = statusFail
		}
		results = append(results, result("integration."+string(kind)+"."+name, status, check.Detail))
	}
	return results
}
