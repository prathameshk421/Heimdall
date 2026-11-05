package main

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type dotenvSecretMapping struct{ key, file string }

func secretsSyncMappings() []dotenvSecretMapping {
	// These are the only dotenv KEY to secrets_dir file mappings. The PEM path
	// is handled separately and its bytes are copied to github_app_private_key.
	return []dotenvSecretMapping{
		{"OPENROUTER_API_KEY", "openrouter_api_key"},
		{"ATLASSIAN_EMAIL", "jira_email"},
		{"ATLASSIAN_API_KEY", "jira_api_token"},
		{"GITHUB_APP_ID", "github_app_id"},
		{"GITHUB_APP_INSTALLATION_ID", "github_app_installation_id"},
	}
}

func runSecretsSync(args []string, stdout, stderr io.Writer) int {
	_ = stdout
	fs := flag.NewFlagSet("heimdall secrets-sync", flag.ContinueOnError)
	fs.SetOutput(stderr)
	envPath := fs.String("env", "", "dotenv file path (required)")
	outDir := fs.String("out", "", "secrets directory (required)")
	requireAll := fs.Bool("require", false, "fail if a mapped key is missing")
	bootstrap := fs.Bool("bootstrap-generated", false, "create fresh database and agent credentials")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 || *envPath == "" || *outDir == "" {
		writeDiagnostic(stderr, "heimdall secrets-sync: --env and --out are required\n")
		return 2
	}
	values, err := readDotenv(*envPath)
	if err != nil {
		writeDiagnostic(stderr, "heimdall secrets-sync: cannot read dotenv file\n")
		return 1
	}
	if err := os.MkdirAll(*outDir, 0o700); err != nil {
		writeDiagnostic(stderr, "heimdall secrets-sync: cannot prepare output directory\n")
		return 1
	}
	if *bootstrap {
		if err := bootstrapGeneratedSecrets(*outDir); err != nil {
			writeDiagnostic(stderr, "heimdall secrets-sync: cannot create generated credentials\n")
			return 1
		}
	}
	missing, err := copyMappedSecrets(values, *outDir)
	if err != nil {
		writeDiagnostic(stderr, err.Error()+"\n")
		return 1
	}
	if len(missing) > 0 {
		writeDiagnostic(stderr, "heimdall secrets-sync: missing keys: "+strings.Join(missing, ", ")+"\n")
		if *requireAll {
			return 1
		}
	}
	return 0
}

func bootstrapGeneratedSecrets(outDir string) error {
	passwordBytes := make([]byte, 32)
	if _, err := rand.Read(passwordBytes); err != nil {
		return err
	}
	password := base64.RawURLEncoding.EncodeToString(passwordBytes)
	if err := writeSecret(outDir, "postgres_password", []byte(password)); err != nil {
		return err
	}
	if err := writeSecret(outDir, "database_url", []byte("postgres://heimdall:"+password+"@postgres:5432/heimdall?sslmode=disable")); err != nil {
		return err
	}
	for _, name := range []string{"agent_token_test-author", "agent_token_reviewer-1", "agent_token_reviewer-2", "agent_token_reviewer-3", "agent_token_resolver"} {
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			return err
		}
		if err := writeSecret(outDir, name, []byte(base64.RawURLEncoding.EncodeToString(buf))); err != nil {
			return err
		}
	}
	return nil
}

func copyMappedSecrets(values map[string]string, outDir string) ([]string, error) {
	missing := make([]string, 0)
	for _, mapping := range secretsSyncMappings() {
		value, ok := values[mapping.key]
		if !ok || value == "" {
			missing = append(missing, mapping.key)
			continue
		}
		if err := writeSecret(outDir, mapping.file, []byte(value)); err != nil {
			return nil, fmt.Errorf("heimdall secrets-sync: %s: cannot write secret file", mapping.key)
		}
	}
	path, ok := values["GITHUB_APP_PRIVATE_KEY_PATH"]
	if !ok || path == "" {
		return append(missing, "GITHUB_APP_PRIVATE_KEY_PATH"), nil
	}
	pem, err := os.ReadFile(path) // #nosec G304 -- path is explicitly supplied by the operator in the dotenv file
	if err != nil {
		return nil, errors.New("heimdall secrets-sync: GITHUB_APP_PRIVATE_KEY_PATH: cannot read key file")
	}
	if err := writeSecret(outDir, "github_app_private_key", pem); err != nil {
		return nil, errors.New("heimdall secrets-sync: GITHUB_APP_PRIVATE_KEY_PATH: cannot write secret file")
	}
	return missing, nil
}

func readDotenv(path string) (map[string]string, error) {
	f, err := os.Open(path) // #nosec G304 -- path is explicitly supplied by the operator via --env
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return parseDotenv(f)
}

func parseDotenv(input io.Reader) (map[string]string, error) {
	values := make(map[string]string)
	s := bufio.NewScanner(input)
	s.Buffer(make([]byte, 4096), 1024*1024)
	for s.Scan() {
		if err := parseDotenvLine(values, s.Text()); err != nil {
			return nil, err
		}
	}
	if s.Err() != nil {
		return nil, errors.New("invalid dotenv input")
	}
	return values, nil
}

func parseDotenvLine(values map[string]string, raw string) error {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, "#") {
		return nil
	}
	line = strings.TrimPrefix(line, "export ")
	key, value, ok := strings.Cut(line, "=")
	key, value = strings.TrimSpace(key), strings.TrimSpace(value)
	if !ok || key == "" {
		return errors.New("invalid dotenv entry")
	}
	if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
		value = value[1 : len(value)-1]
	}
	values[key] = value
	return nil
}

func writeSecret(dir, name string, contents []byte) error {
	path := filepath.Join(dir, name)
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("destination is a symlink")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) // #nosec G304 -- name is from the fixed allowlist
	if err != nil {
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(contents); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func writeDiagnostic(w io.Writer, message string) { _, _ = io.WriteString(w, message) }
