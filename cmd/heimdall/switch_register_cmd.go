package main

import (
	"context"
	"flag"
	"io"
	"net/http"
	"time"

	switchchat "github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/switch"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
)

// Exit statuses of switch-register.
const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

// runSwitchRegister registers the Heimdall coordinator with Switch using the config's Switch settings and stores the
// API key it returns in the key file the config names. It prints the agent id, never the key (WP1 step 2).
func runSwitchRegister(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("switch-register", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "path to heimdall.toml")
	dirFlag := fs.String("secrets-dir", "", "host directory for the key file (default: the config secrets_dir)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if *configPath == "" {
		writeDiagnostic(stderr, "switch-register: --config must name heimdall.toml\n")
		return exitUsage
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		writeDiagnostic(stderr, "switch-register: load config: "+err.Error()+"\n")
		return exitFailure
	}
	if *dirFlag != "" {
		cfg.SecretsDir = *dirFlag
	}
	secretsDir := cfg.SecretsDir
	client, err := switchchat.New(switchchat.Config{
		BaseURL:            cfg.Switch.BaseURL,
		AgentID:            "registration",
		AgentName:          cfg.Switch.AgentName,
		BridgeID:           cfg.Switch.BridgeID,
		Secrets:            cfg,
		APIKeySecret:       cfg.Switch.APIKeySecret,
		RegistrationSecret: cfg.Switch.RegistrationTokenSecret,
		HTTPClient:         &http.Client{Timeout: 30 * time.Second},
		Clock:              clock.System{},
		Wait:               clock.Wait,
	})
	if err != nil {
		writeDiagnostic(stderr, "switch-register: build Switch client: "+err.Error()+"\n")
		return exitFailure
	}
	agentID, err := registerCoordinator(context.Background(), switchRegistrar{client: client}, secretsDir, cfg.Switch.APIKeySecret, cfg.Switch.AgentName, writeSecretFile)
	if err != nil {
		writeDiagnostic(stderr, "switch-register: "+err.Error()+"\n")
		return exitFailure
	}
	if err := writeRegistrationResult(stdout, agentID, cfg.Switch.APIKeySecret); err != nil {
		writeDiagnostic(stderr, "switch-register: write result failed\n")
		return exitFailure
	}
	return exitOK
}
