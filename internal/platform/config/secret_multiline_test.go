package config

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The header and footer are built from two halves so that secret scanners do not flag the test file. The body is
// obviously fake text, not a key shape.
const (
	fakePEMHeader = "-----BEGIN " + "FAKE TEST BLOCK-----"
	fakePEMFooter = "-----END " + "FAKE TEST BLOCK-----"
	fakePEMBody   = "not-a-real-key-" + "body-line-of-fake-text"
	fakePEM       = fakePEMHeader + "\n" + fakePEMBody + "\n" + fakePEMFooter + "\n"
	canary        = "CANARY-PEM-9d1f"
)

// writeSecretFile writes content into dir as name with an exact mode (the umask does not change it).
func writeSecretFile(t *testing.T, dir, name, content string, mode os.FileMode) {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	require.NoError(t, os.Chmod(path, mode))
}

func TestSecretMultilineReadsThreeLinePEMShape(t *testing.T) {
	dir := t.TempDir()
	writeSecretFile(t, dir, "app_key", fakePEM, 0o600)
	cfg := Config{SecretsDir: dir}
	got, err := cfg.SecretMultiline("app_key")
	require.NoError(t, err)
	require.Equal(t, fakePEMHeader+"\n"+fakePEMBody+"\n"+fakePEMFooter, string(got),
		"the newlines between lines are kept and only the trailing newline is removed")
}

func TestSecretMultilineAcceptsOwnerOnlyModes(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{SecretsDir: dir}
	for _, mode := range []os.FileMode{0o600, 0o400} {
		writeSecretFile(t, dir, "key", fakePEM, mode)
		got, err := cfg.SecretMultiline("key")
		require.NoError(t, err, "mode %o", mode)
		require.NotEmpty(t, got)
	}
}

func TestSecretMultilineRefusesGroupOrWorldAccess(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{SecretsDir: dir}
	for _, mode := range []os.FileMode{0o644, 0o640, 0o604, 0o620, 0o660, 0o666} {
		writeSecretFile(t, dir, "key", canary+"\n"+fakePEM, mode)
		got, err := cfg.SecretMultiline("key")
		require.Error(t, err, "mode %o must be refused", mode)
		require.Nil(t, got)
		require.ErrorContains(t, err, "group or others", "mode %o", mode)
		require.NotContains(t, err.Error(), canary, "mode %o error leaked content", mode)
	}
}

func TestSecretMultilineRefusesSymlinkOutOfDirectory(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	writeSecretFile(t, outside, "outside", canary+"\n"+fakePEM, 0o600)
	require.NoError(t, os.Symlink(filepath.Join(outside, "outside"), filepath.Join(dir, "linked")))
	cfg := Config{SecretsDir: dir}
	got, err := cfg.SecretMultiline("linked")
	require.Error(t, err)
	require.Nil(t, got)
	require.ErrorContains(t, err, "linked")
	require.NotContains(t, err.Error(), canary, "symlink error leaked content")
}

func TestSecretMultilineRefusesNamedPipeWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, syscall.Mkfifo(filepath.Join(dir, "pipe"), 0o600))
	cfg := Config{SecretsDir: dir}
	type result struct {
		got []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		got, err := cfg.SecretMultiline("pipe")
		done <- result{got, err}
	}()
	select {
	case r := <-done:
		require.Error(t, r.err)
		require.Nil(t, r.got)
		require.ErrorContains(t, r.err, "not a regular file")
	case <-time.After(5 * time.Second):
		t.Fatal("SecretMultiline blocked on a named pipe instead of refusing it")
	}
}

func TestSecretMultilineSizeLimit(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{SecretsDir: dir}

	atLimit := strings.Repeat("A", maxMultilineSecretBytes)
	writeSecretFile(t, dir, "atlimit", atLimit, 0o600)
	got, err := cfg.SecretMultiline("atlimit")
	require.NoError(t, err, "a file of exactly the limit is accepted")
	require.Len(t, got, maxMultilineSecretBytes)

	overLimit := canary + strings.Repeat("A", maxMultilineSecretBytes)
	writeSecretFile(t, dir, "oversized", overLimit, 0o600)
	got, err = cfg.SecretMultiline("oversized")
	require.Error(t, err)
	require.Nil(t, got)
	require.ErrorContains(t, err, "larger than 16384 bytes")
	require.NotContains(t, err.Error(), canary, "size error leaked content")
}

func TestSecretMultilineRefusals(t *testing.T) {
	dir := t.TempDir()
	writeSecretFile(t, dir, "empty", "", 0o600)
	writeSecretFile(t, dir, "newlines", "\n\r\n", 0o600)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "adir"), 0o700))
	cfg := Config{SecretsDir: dir}

	cases := []struct {
		name    string
		secret  string
		wantErr string
	}{
		{"parent traversal", "../key", "../key"},
		{"nested path", "sub/key", "sub/key"},
		{"empty name", "", "name"},
		{"missing file", "absent", "absent"},
		{"empty file", "empty", "empty"},
		{"only newlines", "newlines", "empty"},
		{"directory instead of file", "adir", "adir"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := cfg.SecretMultiline(tc.secret)
			require.Error(t, err)
			require.Nil(t, got)
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestSecretStaysSingleLineAndIgnoresTheModeRule(t *testing.T) {
	dir := t.TempDir()
	writeSecretFile(t, dir, "token", "tok-live-123\n", 0o644)
	writeSecretFile(t, dir, "multi", "one\ntwo\n", 0o600)
	cfg := Config{SecretsDir: dir}
	got, err := cfg.Secret("token")
	require.NoError(t, err, "Secret keeps its own rules and does not check the mode")
	require.Equal(t, "tok-live-123", got)
	_, err = cfg.Secret("multi")
	require.ErrorContains(t, err, "single line")
}
