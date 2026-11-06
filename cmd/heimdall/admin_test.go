package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminInitIsResumableAndRecordsNamesOnly(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "heimdall.toml")
	secretsDir := filepath.Join(root, "secrets")
	require.NoError(t, copySetupConfig(configPath))
	run := func() (int, string, string) {
		var out, errOut bytes.Buffer
		code := runAdmin([]string{"init", "--config", configPath, "--secrets-dir", secretsDir}, &out, &errOut, func(string) string { return "" })
		return code, out.String(), errOut.String()
	}

	code, first, errOut := run()
	require.Zero(t, code, errOut)
	require.Contains(t, first, "setup prepared")
	require.Contains(t, first, "resume with: heimdall admin init")
	require.Contains(t, first, "heimdall controller enroll")
	require.Contains(t, first, "heimdall connect --check")
	require.Contains(t, first, "database_url")
	require.Contains(t, first, "agent_token_test-author")
	statePath := filepath.Join(root, "heimdall-setup.json")
	stateBefore, err := os.ReadFile(statePath)
	require.NoError(t, err)
	stateInfo, err := os.Stat(statePath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), stateInfo.Mode().Perm())
	dirInfo, err := os.Stat(secretsDir)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())

	code, second, errOut := run()
	require.Zero(t, code, errOut)
	stateAfter, err := os.ReadFile(statePath)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Equal(t, stateBefore, stateAfter)
}

func TestAdminInitOnlyInspectsSecretFileMetadataAndRejectsUnsafeFiles(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "heimdall.toml")
	secretsDir := filepath.Join(root, "secrets")
	require.NoError(t, copySetupConfig(configPath))
	require.NoError(t, os.Mkdir(secretsDir, 0o700))
	secretPath := filepath.Join(secretsDir, "database_url")
	require.NoError(t, os.WriteFile(secretPath, []byte("test-only-canary-secret"), 0o644))

	var out, errOut bytes.Buffer
	code := runAdmin([]string{"init", "--config", configPath, "--secrets-dir", secretsDir}, &out, &errOut, func(string) string { return "" })
	require.Equal(t, 1, code)
	require.Contains(t, errOut.String(), "private regular file")
	require.NotContains(t, out.String()+errOut.String(), "test-only-canary-secret")

	require.NoError(t, os.Chmod(secretPath, 0o600))
	require.NoError(t, os.Remove(secretPath))
	require.NoError(t, os.Symlink(filepath.Join(root, "outside"), secretPath))
	out.Reset()
	errOut.Reset()
	code = runAdmin([]string{"init", "--config", configPath, "--secrets-dir", secretsDir}, &out, &errOut, func(string) string { return "" })
	require.Equal(t, 1, code)
	require.NotContains(t, out.String()+errOut.String(), "outside")
}

func TestAdminInitNeedsConfigAndRefusesUnknownOperations(t *testing.T) {
	var out, errOut bytes.Buffer
	require.Equal(t, 2, runAdmin(nil, &out, &errOut, func(string) string { return "" }))
	errOut.Reset()
	require.Equal(t, 2, runAdmin([]string{"upgrade"}, &out, &errOut, func(string) string { return "" }))
	require.Contains(t, errOut.String(), "usage:")
	errOut.Reset()
	require.Equal(t, 2, runAdmin([]string{"init"}, &out, &errOut, func(string) string { return "" }))
	require.Contains(t, errOut.String(), "--config or HEIMDALL_CONFIG is required")
}

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
