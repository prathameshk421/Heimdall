package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"

	switchchat "github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/switch"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
)

const (
	switchRegisterOK    = 0
	switchRegisterFail  = 1
	switchRegisterUsage = 2
	// registeringAgentID fills the client's required id field. Registration does not use it.
	registeringAgentID = "registering"
)

// switchRegistrar adapts the Switch client to coordinatorRegistrar.
type switchRegistrar struct{ client *switchchat.Client }

func (s switchRegistrar) Register(ctx context.Context, agentType, name, description string) (string, string, error) {
	reg, err := s.client.RegisterCoordinator(ctx, agentType, name, description)
	if err != nil {
		return "", "", err
	}
	return reg.AgentID, reg.APIKey.Value(), nil
}

// runSwitchRegister registers the coordinator with Switch. It reads the registration token from the secrets
// directory, writes the returned API key there with mode 0600, and prints only the agent id.
func runSwitchRegister(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("heimdall switch-register", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "path to heimdall.toml (required)")
	secretsDir := fs.String("secrets-dir", "", "host secrets directory, replacing secrets_dir from the config")
	name := fs.String("name", "", "coordinator agent name (required)")
	agentType := fs.String("type", "opencode", "Switch agent runtime type")
	if err := fs.Parse(args); err != nil {
		return switchRegisterUsage
	}
	if fs.NArg() != 0 || *configPath == "" || *name == "" {
		writeDiagnostic(stderr, "usage: heimdall switch-register --config PATH --name NAME [--secrets-dir DIR] [--type opencode]\n")
		return switchRegisterUsage
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		writeDiagnostic(stderr, "heimdall switch-register: load config failed\n")
		return switchRegisterFail
	}
	if *secretsDir != "" {
		cfg.SecretsDir = *secretsDir
	}
	client, err := switchchat.New(switchchat.Config{
		BaseURL: cfg.Switch.BaseURL, AgentID: registeringAgentID, AgentName: *name,
		Secrets: cfg, APIKeySecret: cfg.Switch.APIKeySecret, RegistrationSecret: cfg.Switch.RegistrationTokenSecret,
		HTTPClient: &http.Client{}, Clock: clock.System{}, Wait: clock.Wait,
	})
	if err != nil {
		writeDiagnostic(stderr, "heimdall switch-register: Switch configuration is incomplete\n")
		return switchRegisterFail
	}
	store := func(secret string, value []byte) error { return writeSecretFile(cfg.SecretsDir, secret, value) }
	id, err := registerCoordinator(context.Background(), switchRegistrar{client: client}, store, cfg.Switch.APIKeySecret, *agentType, *name)
	if err != nil {
		writeDiagnostic(stderr, fmt.Sprintf("heimdall switch-register: %v\n", err))
		return switchRegisterFail
	}
	_, _ = fmt.Fprintf(stdout, "registered coordinator %s; agent id %s; key stored as %s\n", *name, id, cfg.Switch.APIKeySecret)
	return switchRegisterOK
}
