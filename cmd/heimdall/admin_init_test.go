package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/stretchr/testify/require"
)

func TestAdminCommandIsRegistered(t *testing.T) {
	_, ok := command("admin")
	require.True(t, ok, "admin init must be available from the CLI")
}

func TestAdminInitCreatesValidatedConfigAndResumesWithoutOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "install", "heimdall.toml")
	answers := "\n\n\nhttps://jira.acme.test\nENG\n\n\nhttp://127.0.0.1:8000\ncoord-id\nheimdall-coordinator\nbridge-id\n\n\n\n\n\n\n\n\nacme/payments\nmain\n"
	var stdout, stderr bytes.Buffer
	code := runAdmin([]string{"init", "--config", path}, bytes.NewBufferString(answers), &stdout, &stderr)
	require.Equal(t, 0, code, stderr.String())
	cfg, err := config.Load(path)
	require.NoError(t, err)
	require.Equal(t, "ENG", cfg.Jira.ProjectKeys[0])
	require.Equal(t, "acme/payments", cfg.Projects[0].Repo)
	require.Contains(t, stdout.String(), "github_app_private_key")
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(body), "secret value")
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	before := string(body)
	stdout.Reset()
	code = runAdmin([]string{"init", "--config", path}, bytes.NewBuffer(nil), &stdout, &stderr)
	require.Equal(t, 0, code, stderr.String())
	require.Contains(t, stdout.String(), "resumed")
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, string(after))
}
