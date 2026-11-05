package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminSecretSetPromptsTwiceAndNeverPrintsTheValue(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "heimdall.toml")
	secretsDir := filepath.Join(root, "secrets")
	require.NoError(t, copySetupConfig(configPath))
	var calls int
	prompt := func(io.Writer) ([]byte, error) {
		calls++
		return []byte("test-only-secret-value"), nil
	}
	var out, errOut bytes.Buffer
	code := runAdminSecret([]string{"--config", configPath, "--secrets-dir", secretsDir, "--name", "database_url"}, &out, &errOut, func(string) string { return "" }, prompt)
	require.Zero(t, code, errOut.String())
	require.Equal(t, 2, calls)
	require.Contains(t, out.String(), "database_url")
	require.NotContains(t, out.String()+errOut.String(), "test-only-secret-value")
	value, err := os.ReadFile(filepath.Join(secretsDir, "database_url"))
	require.NoError(t, err)
	require.Equal(t, "test-only-secret-value", string(value))
	info, err := os.Stat(filepath.Join(secretsDir, "database_url"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestAdminSecretSetRefusesMismatchUnknownNameAndInsecureSource(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "heimdall.toml")
	secretsDir := filepath.Join(root, "secrets")
	source := filepath.Join(root, "private-key.pem")
	require.NoError(t, copySetupConfig(configPath))
	require.NoError(t, os.WriteFile(source, []byte("test-only-private-key"), 0o644))
	var out, errOut bytes.Buffer
	values := [][]byte{[]byte("first"), []byte("second")}
	index := 0
	prompt := func(io.Writer) ([]byte, error) {
		value := values[index]
		index++
		return value, nil
	}
	code := runAdminSecret([]string{"--config", configPath, "--secrets-dir", secretsDir, "--name", "database_url"}, &out, &errOut, func(string) string { return "" }, prompt)
	require.Equal(t, 1, code)
	require.NotContains(t, out.String()+errOut.String(), "first")

	out.Reset()
	errOut.Reset()
	code = runAdminSecret([]string{"--config", configPath, "--secrets-dir", secretsDir, "--name", "unknown"}, &out, &errOut, func(string) string { return "" }, prompt)
	require.Equal(t, 2, code)

	out.Reset()
	errOut.Reset()
	code = runAdminSecret([]string{"--config", configPath, "--secrets-dir", secretsDir, "--name", "github_app_private_key", "--from-file", source}, &out, &errOut, func(string) string { return "" }, prompt)
	require.Equal(t, 1, code)
	require.NotContains(t, out.String()+errOut.String(), "test-only-private-key")
}

func copySetupConfig(path string) error {
	data, err := os.ReadFile("../../deploy/heimdall.example.toml")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
