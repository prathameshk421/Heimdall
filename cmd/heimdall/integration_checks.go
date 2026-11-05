package main

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/github"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/openrouter"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/registry"
	switchchat "github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/switch"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/tracker/jira"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const openCodeVersion = "1.19.5"

type cryptoJitter struct{}

func (cryptoJitter) Int63n(n int64) int64 {
	if n <= 0 {
		return 0
	}
	v, err := cryptorand.Int(cryptorand.Reader, big.NewInt(n))
	if err != nil {
		return 0
	}
	return v.Int64()
}

func checkGitHubIntegration(ctx context.Context, cfg config.Config) registry.CheckResult {
	appIDName := configuredSecretName(cfg.GitHub.AppIDSecret, "github_app_id")
	installationName := configuredSecretName(cfg.GitHub.InstallationIDSecret, "github_app_installation_id")
	privateKeyName := configuredSecretName(cfg.GitHub.PrivateKeySecret, "github_app_private_key")
	appID, err := parseSecretInt(cfg, appIDName)
	if err != nil {
		return missingCheck("github", err)
	}
	installationID, err := parseSecretInt(cfg, installationName)
	if err != nil {
		return missingCheck("github", err)
	}
	key, err := cfg.Secret(privateKeyName)
	if err != nil {
		return missingCheck("github", err)
	}
	adapter, err := github.New(github.Config{
		BaseURL:    "https://api.github.com",
		App:        github.App{AppID: appID, InstallationID: installationID, PrivateKeyPEM: []byte(key)},
		HTTPClient: &http.Client{Timeout: 15 * time.Second}, Clock: clock.System{}, Wait: clock.Wait,
	})
	if err != nil {
		return registry.CheckResult{Status: registry.StatusNeedsConfig, Detail: "GitHub App configuration is invalid; check the configured secret file names and PEM format"}
	}
	if _, err := adapter.Tokens().Installation(ctx); err != nil {
		return statusFromIntegrationError("github", err)
	}
	return registry.CheckResult{Status: registry.StatusConnected, Detail: "GitHub App installation answered; permissions and repository access are readable"}
}

func configuredSecretName(configured, fallback string) string {
	if configured != "" {
		return configured
	}
	return fallback
}

func checkJira(ctx context.Context, cfg config.Config) registry.CheckResult {
	if cfg.Jira.BaseURL == "" || len(cfg.Jira.ProjectKeys) == 0 {
		return registry.CheckResult{Status: registry.StatusNeedsConfig, Detail: "set jira.base_url and jira.project_keys in heimdall.toml"}
	}
	client, err := jira.New(jira.Config{
		BaseURL: cfg.Jira.BaseURL, Project: cfg.Jira.ProjectKeys[0],
		EmailSecret: cfg.Jira.EmailSecret, TokenSecret: cfg.Jira.APITokenSecret,
	}, cfg, clock.System{}, clock.Wait, cryptoJitter{}, &http.Client{Timeout: 15 * time.Second})
	if err != nil {
		return missingCheck("jira", err)
	}
	if _, _, err := client.Changes(ctx, ports.Cursor("")); err != nil {
		return statusFromIntegrationError("jira", err)
	}
	return registry.CheckResult{Status: registry.StatusConnected, Detail: "authenticated Jira poll succeeded for the configured project"}
}

func checkSwitch(ctx context.Context, cfg config.Config) registry.CheckResult {
	client, err := switchchat.New(switchchat.Config{
		BaseURL: cfg.Switch.BaseURL, AgentID: cfg.Switch.AgentID, AgentName: cfg.Switch.AgentName, BridgeID: cfg.Switch.BridgeID,
		Secrets: cfg, APIKeySecret: cfg.Switch.APIKeySecret, RegistrationSecret: cfg.Switch.RegistrationTokenSecret,
		HTTPClient: &http.Client{}, Clock: clock.System{}, Wait: clock.Wait,
	})
	if err != nil {
		return registry.CheckResult{Status: registry.StatusNeedsConfig, Detail: "Switch coordinator configuration is incomplete; check the [switch] fields in heimdall.toml"}
	}
	if err := client.Check(ctx); err != nil {
		return statusFromIntegrationError("switch", err)
	}
	return registry.CheckResult{Status: registry.StatusConnected, Detail: "authenticated Switch room listing succeeded"}
}

func checkOpenRouter(ctx context.Context, cfg config.Config) registry.CheckResult {
	if _, err := cfg.Secret("openrouter_api_key"); err != nil {
		return missingCheck("OpenRouter", err)
	}
	if _, err := (openrouter.Client{Secrets: costSecrets{cfg: cfg}, SecretName: openRouterFile}).Balance(ctx); err != nil {
		return statusFromIntegrationError("OpenRouter", err)
	}
	return registry.CheckResult{Status: registry.StatusConnected, Detail: "authenticated OpenRouter credit endpoint answered"}
}

func checkOpenCode(ctx context.Context, cfg config.Config) registry.CheckResult {
	if balance := checkOpenRouter(ctx, cfg); balance.Status != registry.StatusConnected {
		return balance
	}
	out, err := exec.CommandContext(ctx, "opencode", "--version").CombinedOutput()
	if err != nil {
		return registry.CheckResult{Status: registry.StatusNeedsConfig, Detail: "OpenCode is unavailable; install the pinned 1.19.5 runtime on the agent host"}
	}
	if strings.TrimSpace(string(out)) != openCodeVersion {
		return registry.CheckResult{Status: registry.StatusNeedsConfig, Detail: "OpenCode version does not match the pinned 1.19.5 runtime"}
	}
	return registry.CheckResult{Status: registry.StatusConnected, Detail: "OpenRouter credentials work and OpenCode 1.19.5 is installed; run admin init to check the controller service"}
}

func checkGraphitiContainer(ctx context.Context, _ config.Config) registry.CheckResult {
	status, err := exec.CommandContext(ctx, "docker", "ps", "--filter", "label=com.docker.compose.project=heimdall", "--filter", "label=com.docker.compose.service=graphiti", "--format", "{{.Status}}").Output()
	if err != nil {
		return registry.CheckResult{Status: registry.StatusUnreachable, Detail: "cannot query Docker for Graphiti; run doctor --host and make recover"}
	}
	containerStatus := strings.TrimSpace(string(status))
	if containerStatus == "" || strings.Contains(containerStatus, "\n") {
		return registry.CheckResult{Status: registry.StatusNeedsConfig, Detail: "Graphiti container is not running; complete deployment with make deploy"}
	}
	if !strings.Contains(containerStatus, "(healthy)") {
		return registry.CheckResult{Status: registry.StatusUnreachable, Detail: "Graphiti health check is not healthy; verify the OpenRouter key file and run make recover"}
	}
	return registry.CheckResult{Status: registry.StatusConnected, Detail: "Graphiti /health check is healthy in the Heimdall Compose project"}
}

func parseSecretInt(cfg config.Config, name string) (int64, error) {
	value, err := cfg.Secret(name)
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("configured secret file %s must contain a positive integer", name)
	}
	return parsed, nil
}

func missingCheck(system string, err error) registry.CheckResult {
	return registry.CheckResult{Status: registry.StatusNeedsConfig, Detail: system + " configuration needs attention: " + safeConfigError(err)}
}

func safeConfigError(err error) string {
	switch {
	case errors.Is(err, ports.ErrUnauthorized):
		return "credentials were rejected"
	case errors.Is(err, ports.ErrUnavailable):
		return "service is unavailable"
	default:
		return strings.ReplaceAll(err.Error(), "\n", " ")
	}
}

func statusFromIntegrationError(system string, err error) registry.CheckResult {
	switch {
	case errors.Is(err, ports.ErrUnauthorized):
		return registry.CheckResult{Status: registry.StatusNeedsConfig, Detail: system + " rejected its credential; check the named secret files and account permissions"}
	case errors.Is(err, ports.ErrUnavailable):
		return registry.CheckResult{Status: registry.StatusUnreachable, Detail: system + " is unreachable; check the configured URL and outbound network"}
	default:
		return registry.CheckResult{Status: registry.StatusNeedsConfig, Detail: system + " check failed; check the configured site, project and secret file names"}
	}
}
