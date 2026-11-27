package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminCommandIsRegistered(t *testing.T) {
	_, ok := command("admin")
	require.True(t, ok, "admin init must be available from the CLI")
}

func TestAdminInitCanonicalizesRelativeConfigPath(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	path := "./install/../heimdall.toml"
	secretsDir := filepath.Join(root, "secrets")
	answers := "\n" + secretsDir + "\n\nhttps://jira.acme.test\nENG\n\n\nhttp://127.0.0.1:8000\ncoord-id\nheimdall-coordinator\nbridge-id\n\n\n\n\n\n\n\n\nacme/payments\nmain\n"
	var stdout, stderr bytes.Buffer
	code := runAdmin([]string{"init", "--config", path}, bytes.NewBufferString(answers), &stdout, &stderr)
	require.Zero(t, code, stderr.String())
	expected := filepath.Join(root, "heimdall.toml")
	require.Contains(t, stdout.String(), expected)
	state, err := os.ReadFile(filepath.Join(root, "heimdall-setup.json"))
	require.NoError(t, err)
	require.Contains(t, string(state), expected)
}
