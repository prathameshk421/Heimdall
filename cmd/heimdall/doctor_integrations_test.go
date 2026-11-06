package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/registry"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/stretchr/testify/require"
)

func TestDoctorRunsTheSelectedProviderCheckAndMapsItsStatus(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "heimdall.toml")
	require.NoError(t, os.WriteFile(configPath, []byte("secrets_dir = \""+filepath.Join(root, "secrets")+"\"\n\n[chat]\nbridge = \"switch\"\n"), 0o600))
	cfg, err := config.Load(configPath)
	require.NoError(t, err)
	for _, tc := range []struct {
		integrationStatus registry.Status
		doctorStatus      string
	}{
		{registry.StatusConnected, statusOK},
		{registry.StatusNeedsConfig, statusMissing},
		{registry.StatusUnreachable, statusFail},
	} {
		reg, err := registry.Default([]registry.Entry{{
			Name: "switch", Kind: registry.KindChat,
			Factory: func(config.Config) (any, error) { return struct{}{}, nil },
			Check: func(context.Context, config.Config) registry.CheckResult {
				return registry.CheckResult{Status: tc.integrationStatus, Detail: "check ran"}
			},
		}})
		require.NoError(t, err)
		rows := configuredIntegrationChecks(context.Background(), cfg, func() (*registry.Registry, error) { return reg, nil }, time.Second)
		require.Len(t, rows, 3)
		require.Equal(t, checkResult{Check: "integration.chat.switch", Status: tc.doctorStatus, Detail: "check ran"}, rows[2])
	}
}

func TestDoctorReportsMissingRequiredProviderSelections(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "heimdall.toml")
	require.NoError(t, os.WriteFile(configPath, []byte("secrets_dir = \""+filepath.Join(root, "secrets")+"\"\n"), 0o600))
	cfg, err := config.Load(configPath)
	require.NoError(t, err)
	rows := configuredIntegrationChecks(context.Background(), cfg, defaultRegistry, time.Second)
	require.Equal(t, []checkResult{
		{Check: "integration.vcs", Status: statusMissing, Detail: "vcs.provider is required; run heimdall connect to choose an integration"},
		{Check: "integration.tracker", Status: statusMissing, Detail: "tracker.provider is required; run heimdall connect to choose an integration"},
		{Check: "integration.chat", Status: statusMissing, Detail: "chat.bridge is required; run heimdall connect to choose an integration"},
	}, rows)
}

func TestControllerServiceCheckRequiresValidUIDAndActiveUnit(t *testing.T) {
	called := false
	active := func(_ context.Context, service string) error {
		called = true
		require.Equal(t, "switch-agent-controller-1000.service", service)
		return nil
	}
	require.Equal(t, registry.StatusNeedsConfig, controllerServiceCheck(context.Background(), "bad-uid", active).Status)
	require.False(t, called, "invalid uid must not reach systemctl")
	require.Equal(t, registry.StatusConnected, controllerServiceCheck(context.Background(), "1000", active).Status)
	require.True(t, called)
	called = false
	result := controllerServiceCheck(context.Background(), "1000", func(context.Context, string) error {
		called = true
		return errors.New("systemd unit is stopped")
	})
	require.True(t, called)
	require.Equal(t, registry.StatusNeedsConfig, result.Status)
	require.Contains(t, result.Detail, "install-controller.sh")
}
