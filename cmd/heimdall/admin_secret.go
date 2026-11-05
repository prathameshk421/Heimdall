package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"golang.org/x/term"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
)

type hiddenSecretPrompt func(io.Writer) ([]byte, error)

type adminSecretOptions struct {
	configPath string
	secretsDir string
	name       string
	fromFile   string
}

var errSecretNotConfigured = errors.New("name is not required by this configuration")

func runAdminSecret(args []string, stdout, stderr io.Writer, getenv func(string) string, prompt hiddenSecretPrompt) int {
	opts, code, err := parseAdminSecret(args, getenv, stderr)
	if err != nil {
		writeDiagnostic(stderr, err.Error()+"\n")
		return code
	}
	secretsDir, err := prepareAdminSecretDir(opts)
	if err != nil {
		writeDiagnostic(stderr, "heimdall admin secret set: "+err.Error()+"\n")
		if errors.Is(err, errSecretNotConfigured) {
			return 2
		}
		return 1
	}
	value, err := getAdminSecretValue(opts, stderr, prompt)
	if err != nil {
		clear(value)
		writeDiagnostic(stderr, "heimdall admin secret set: secret input failed or confirmations did not match\n")
		return 1
	}
	defer clear(value)
	if err := validateSecretValue(opts.name, value); err != nil {
		writeDiagnostic(stderr, "heimdall admin secret set: secret is empty, too large, or has invalid line breaks\n")
		return 1
	}
	if err := writeSecretFile(secretsDir, opts.name, value); err != nil {
		writeDiagnostic(stderr, "heimdall admin secret set: could not store the secret file securely\n")
		return 1
	}
	if _, err := fmt.Fprintf(stdout, "stored secret file %s with mode 0600\n", opts.name); err != nil {
		writeDiagnostic(stderr, "heimdall admin secret set: output failed\n")
		return 1
	}
	return 0
}

func parseAdminSecret(args []string, getenv func(string) string, stderr io.Writer) (adminSecretOptions, int, error) {
	fs := flag.NewFlagSet("heimdall admin secret set", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "path to the operator's heimdall.toml")
	secretsPath := fs.String("secrets-dir", "", "directory for secret files (default: secrets_dir in config)")
	name := fs.String("name", "", "configured secret file name")
	fromFile := fs.String("from-file", "", "private source file for multiline secrets such as a PEM key")
	if err := fs.Parse(args); err != nil {
		return adminSecretOptions{}, 2, err
	}
	if fs.NArg() != 0 || *name == "" {
		return adminSecretOptions{}, 2, errors.New("usage: heimdall admin secret set --name NAME [--config PATH] [--secrets-dir DIR] [--from-file PATH]")
	}
	if *configPath == "" {
		*configPath = getenv("HEIMDALL_CONFIG")
	}
	if *configPath == "" {
		return adminSecretOptions{}, 2, errors.New("heimdall admin secret set: --config or HEIMDALL_CONFIG is required")
	}
	return adminSecretOptions{configPath: *configPath, secretsDir: *secretsPath, name: *name, fromFile: *fromFile}, 0, nil
}

func prepareAdminSecretDir(opts adminSecretOptions) (string, error) {
	cfg, err := config.Load(opts.configPath)
	if err != nil {
		return "", errors.New("config does not load; check the path and TOML fields")
	}
	if !slices.Contains(setupSecretNames(cfg), opts.name) {
		return "", errSecretNotConfigured
	}
	if opts.secretsDir == "" {
		opts.secretsDir = cfg.SecretsDir
	}
	if err := preparePrivateDir(opts.secretsDir); err != nil {
		return "", errors.New("cannot prepare secrets directory; check ownership and symlinks")
	}
	return opts.secretsDir, nil
}

func getAdminSecretValue(opts adminSecretOptions, stderr io.Writer, prompt hiddenSecretPrompt) ([]byte, error) {
	if opts.fromFile != "" {
		return readPrivateSecretFile(opts.fromFile, opts.name)
	}
	value, err := prompt(stderr)
	if err != nil {
		return value, err
	}
	confirmation, err := prompt(stderr)
	if err == nil && !bytes.Equal(value, confirmation) {
		err = errors.New("secret confirmations did not match")
	}
	clear(confirmation)
	return value, err
}

func readHiddenSecret(stderr io.Writer) ([]byte, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil, errors.New("secret prompt requires a terminal")
	}
	if _, err := io.WriteString(stderr, "Secret (input hidden): "); err != nil {
		return nil, err
	}
	value, err := term.ReadPassword(fd)
	_, writeErr := io.WriteString(stderr, "\n")
	if err != nil {
		return nil, err
	}
	return value, writeErr
}

func validateSecretValue(name string, value []byte) error {
	if len(value) == 0 {
		return errors.New("secret is empty")
	}
	maxBytes := 64 << 10
	if name == "github_app_private_key" {
		maxBytes = 16 << 10
	} else if bytes.ContainsAny(value, "\r\n") {
		return errors.New("single-line secret contains a line break")
	}
	if len(value) > maxBytes {
		return errors.New("secret is too large")
	}
	return nil
}

func readPrivateSecretFile(path, name string) ([]byte, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(filepath.Dir(absolute))
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	base := filepath.Base(absolute)
	info, err := root.Lstat(base)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("source must be a private regular file")
	}
	limit := int64(64 << 10)
	if name == "github_app_private_key" {
		limit = 16 << 10
	}
	file, err := root.Open(base)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	value, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	return value, nil
}

func writeSecretFile(dir, name string, value []byte) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if info, err := root.Lstat(name); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("destination is a symlink")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	tempName, err := adminTempName(".secret-")
	if err != nil {
		return err
	}
	tmp, err := root.OpenFile(tempName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = root.Remove(tempName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(value); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return root.Rename(tempName, name)
}
func preparePrivateDir(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(filepath.Dir(absolute))
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	name := filepath.Base(absolute)
	if info, err := root.Lstat(name); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("secrets directory is a symlink")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := root.MkdirAll(name, 0o700); err != nil {
		return err
	}
	return root.Chmod(name, 0o700)
}

func adminTempName(prefix string) (string, error) {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(entropy[:]), nil
}
