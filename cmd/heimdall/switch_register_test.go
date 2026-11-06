package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeRegistrar struct {
	id, key string
	err     error
	typ     string
}

func (f *fakeRegistrar) RegisterCoordinator(_ context.Context, agentType, _, _ string) (string, string, error) {
	f.typ = agentType
	return f.id, f.key, f.err
}

// The key goes to its file and the output carries only the agent id: the key never reaches a log or a terminal.
func TestRegisterCoordinatorStoresTheKeyAndPrintsOnlyTheID(t *testing.T) {
	dir := t.TempDir()
	r := &fakeRegistrar{id: "agent-42", key: "key-value-xyz"}

	id, err := registerCoordinator(context.Background(), r, dir, "switch_api_key", "heimdall-coordinator", writeTestKeyFile)
	require.NoError(t, err)
	require.Equal(t, "agent-42", id)
	require.Equal(t, "opencode", r.typ)

	stored, err := os.ReadFile(filepath.Join(dir, "switch_api_key"))
	require.NoError(t, err)
	require.Equal(t, "key-value-xyz", string(stored))

	var out bytes.Buffer
	require.NoError(t, writeRegistrationResult(&out, id, "switch_api_key"))
	require.NotContains(t, out.String(), "key-value-xyz", "the key is never printed")
	require.Contains(t, out.String(), "agent-42")
}

func TestRegisterCoordinatorRefusesAFailedRegistration(t *testing.T) {
	r := &fakeRegistrar{err: errors.New("switch refused")}
	_, err := registerCoordinator(context.Background(), r, t.TempDir(), "switch_api_key", "x", writeTestKeyFile)
	require.ErrorContains(t, err, "switch refused")
}

func TestRegisterCoordinatorRefusesAnEmptyReply(t *testing.T) {
	r := &fakeRegistrar{id: "agent-42"}
	_, err := registerCoordinator(context.Background(), r, t.TempDir(), "switch_api_key", "x", writeTestKeyFile)
	require.ErrorContains(t, err, "no agent id or key")
}

func writeTestKeyFile(dir, name string, value []byte) error {
	return os.WriteFile(filepath.Join(dir, name), value, 0o600)
}
