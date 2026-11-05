package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSecretsSyncMapsFilesWithoutPrintingValuesAndUses0600(t *testing.T) {
	dir := t.TempDir()
	env := filepath.Join(dir, "test.env")
	pem := filepath.Join(dir, "app.pem")
	canary := "CANARY-ONLY-DO-NOT-PRINT-9f3a"
	require.NoError(t, os.WriteFile(pem, []byte("PEM-"+canary), 0o600))
	require.NoError(t, os.WriteFile(env, []byte(strings.Join([]string{
		"OPENROUTER_API_KEY=" + canary,
		"ATLASSIAN_EMAIL=dev@example.invalid",
		"ATLASSIAN_API_KEY=jira-canary",
		"GITHUB_APP_ID=123",
		"GITHUB_APP_INSTALLATION_ID=456",
		"GITHUB_APP_PRIVATE_KEY_PATH=" + pem,
	}, "\n")), 0o600))
	out := filepath.Join(dir, "secrets")
	var stdout, stderr bytes.Buffer
	require.Equal(t, 0, runSecretsSync([]string{"--env", env, "--out", out, "--require"}, &stdout, &stderr))
	require.NotContains(t, stdout.String(), canary)
	require.NotContains(t, stderr.String(), canary)
	want := map[string]string{
		"openrouter_api_key":         canary,
		"jira_email":                 "dev@example.invalid",
		"jira_api_token":             "jira-canary",
		"github_app_id":              "123",
		"github_app_installation_id": "456",
		"github_app_private_key":     "PEM-" + canary,
	}
	for name, contents := range want {
		path := filepath.Join(out, name)
		actual, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, contents, string(actual))
		info, err := os.Stat(path)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
}

func TestSecretsSyncRequiresExplicitEnvAndNamesMissingKeys(t *testing.T) {
	var stdout, stderr bytes.Buffer
	require.Equal(t, 2, runSecretsSync([]string{"--out", t.TempDir()}, &stdout, &stderr))
	require.NotContains(t, stderr.String(), "CANARY")

	dir := t.TempDir()
	env, out := filepath.Join(dir, "empty.env"), filepath.Join(dir, "secrets")
	require.NoError(t, os.WriteFile(env, nil, 0o600))
	stderr.Reset()
	require.Equal(t, 1, runSecretsSync([]string{"--env", env, "--out", out, "--require"}, &stdout, &stderr))
	require.Contains(t, stderr.String(), "OPENROUTER_API_KEY")
	require.Contains(t, stderr.String(), "GITHUB_APP_PRIVATE_KEY_PATH")
}

func TestExampleConfigUsesRegisteredSwitchChatBridge(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "deploy", "heimdall.example.toml"))
	require.NoError(t, err)
	require.Contains(t, string(data), "bridge = \"mattermost\"")
}
