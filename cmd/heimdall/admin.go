package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
)

const adminInitStateVersion = 1

type adminInitState struct {
	Version       int      `json:"version"`
	ConfigPath    string   `json:"config_path"`
	SecretsDir    string   `json:"secrets_dir"`
	RequiredFiles []string `json:"required_files"`
	MissingFiles  []string `json:"missing_files"`
}

func runAdmin(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) >= 2 && args[0] == "secret" && args[1] == "set" {
		return runAdminSecret(args[2:], stdout, stderr, config.Getenv, readHiddenSecret)
	}
	if len(args) == 0 || args[0] != "init" {
		writeDiagnostic(stderr, "usage: heimdall admin init --config PATH [--secrets-dir DIR]; heimdall admin secret set --name NAME [--from-file PATH]\n")
		return 2
	}
	return runAdminInitWorkflow(args, stdin, stdout, stderr)
}

func runAdminInitWorkflow(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	configPath, err := adminConfigPath(args, config.Getenv)
	if err != nil {
		writeDiagnostic(stderr, "heimdall admin init: config path is invalid\n")
		return 2
	}
	switch adminConfigStatus(configPath) {
	case 2:
		if code := runAdminCreateConfig(args, stdin, stdout, stderr); code != 0 {
			return code
		}
	case 0, 3:
		return runAdminCreateConfig(args, stdin, stdout, stderr)
	}
	progressArgs := []string{"--config", configPath}
	for index := 1; index < len(args); index++ {
		if args[index] == "--config" {
			index++
			continue
		}
		if strings.HasPrefix(args[index], "--config=") {
			continue
		}
		progressArgs = append(progressArgs, args[index])
	}
	return runAdminProgress(progressArgs, stdout, stderr, config.Getenv)
}

func adminConfigPath(args []string, getenv func(string) string) (string, error) {
	raw := ""
	for i, arg := range args {
		if arg == "--config" && i+1 < len(args) {
			raw = args[i+1]
			break
		}
		if strings.HasPrefix(arg, "--config=") {
			raw = strings.TrimPrefix(arg, "--config=")
			break
		}
	}
	if raw == "" {
		raw = getenv("HEIMDALL_CONFIG")
	}
	if raw == "" {
		raw = "/etc/heimdall/heimdall.toml"
	}
	return canonicalAdminConfigPath(raw)
}

// runAdminProgress records resumable setup state without reading secret values.
func runAdminProgress(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	configPath, secretsPath, code, err := parseAdminInit(args, getenv, stderr)
	if err != nil {
		writeDiagnostic(stderr, err.Error()+"\n")
		return code
	}
	state, err := prepareAdminInit(configPath, secretsPath)
	if err != nil {
		writeDiagnostic(stderr, "heimdall admin init: "+err.Error()+"\n")
		return 1
	}
	if len(state.MissingFiles) == 0 {
		_, err = fmt.Fprintf(stdout, "setup prepared; all %d required secret files are present with private permissions\n", len(state.RequiredFiles))
	} else {
		_, err = fmt.Fprintf(stdout, "setup prepared; create these mode-0600 files under %s: %s\n", state.SecretsDir, strings.Join(state.MissingFiles, ", "))
	}
	if err != nil {
		writeDiagnostic(stderr, "heimdall admin init: write progress: output failed\n")
		return 1
	}
	if _, err := fmt.Fprintln(stdout, "resume with: heimdall admin init --config <path>"); err != nil {
		writeDiagnostic(stderr, "heimdall admin init: write progress: output failed\n")
		return 1
	}
	if _, err := fmt.Fprintln(stdout, "core: run heimdall doctor --host, deploy with make deploy, then verify heimdall doctor and authenticated MCP health"); err != nil {
		writeDiagnostic(stderr, "heimdall admin init: write progress: output failed\n")
		return 1
	}
	if _, err := fmt.Fprintln(stdout, "integrations: complete GitHub App and Jira setup, then resume and run heimdall connect --check"); err != nil {
		writeDiagnostic(stderr, "heimdall admin init: write progress: output failed\n")
		return 1
	}
	if _, err := fmt.Fprintln(stdout, "connect progress: see saved integration steps with heimdall connect progress --config <path>; repeat heimdall connect <kind> <name> after fixing a check"); err != nil {
		writeDiagnostic(stderr, "heimdall admin init: write progress: output failed\n")
		return 1
	}
	if _, err := fmt.Fprintln(stdout, "agent runtime: run heimdall controller enroll --server <Switch URL> --name heimdall-vm --data-dir /var/lib/switch-agent-controller-data/<controller-uid>; it prompts privately for the one-time code, then run install-controller.sh --controller-data-dir with that same path"); err != nil {
		writeDiagnostic(stderr, "heimdall admin init: write progress: output failed\n")
		return 1
	}
	if _, err := fmt.Fprintln(stdout, "recovery: make recover reconciles services and verifies server health, authenticated MCP, and Graphiti"); err != nil {
		writeDiagnostic(stderr, "heimdall admin init: write progress: output failed\n")
		return 1
	}
	return 0
}

func parseAdminInit(args []string, getenv func(string) string, stderr io.Writer) (string, string, int, error) {
	fs := flag.NewFlagSet("heimdall admin init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "path to the operator's heimdall.toml")
	secretsPath := fs.String("secrets-dir", "", "directory for secret files (default: secrets_dir in config)")
	if err := fs.Parse(args); err != nil {
		return "", "", 2, err
	}
	if fs.NArg() != 0 {
		return "", "", 2, errors.New("heimdall admin init: unexpected argument")
	}
	if *configPath == "" {
		*configPath = getenv("HEIMDALL_CONFIG")
	}
	if *configPath == "" {
		return "", "", 2, errors.New("heimdall admin init: --config or HEIMDALL_CONFIG is required")
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return "", "", 1, errors.New("heimdall admin init: config does not load; check the path and TOML fields")
	}
	if *secretsPath == "" {
		*secretsPath = cfg.SecretsDir
	}
	if *secretsPath == "" {
		return "", "", 2, errors.New("heimdall admin init: --secrets-dir is required when config has no secrets_dir")
	}
	return *configPath, *secretsPath, 0, nil
}

func prepareAdminInit(configPath, secretsPath string) (adminInitState, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return adminInitState{}, errors.New("config does not load; check the path and TOML fields")
	}
	if err := preparePrivateDir(secretsPath); err != nil {
		return adminInitState{}, errors.New("cannot prepare secrets directory; check ownership and symlinks")
	}
	required := setupSecretNames(cfg)
	missing, err := checkSecretFiles(secretsPath, required)
	if err != nil {
		return adminInitState{}, errors.New("a secret file is not a private regular file; inspect file names and permissions only")
	}
	state := adminInitState{
		Version:       adminInitStateVersion,
		ConfigPath:    configPath,
		SecretsDir:    secretsPath,
		RequiredFiles: required,
		MissingFiles:  missing,
	}
	if err := writeAdminState(filepath.Join(filepath.Dir(configPath), "heimdall-setup.json"), state); err != nil {
		return adminInitState{}, errors.New("cannot record setup progress")
	}
	return state, nil
}

func setupSecretNames(cfg config.Config) []string {
	names := []string{
		cfg.Database.URLSecret,
		cfg.Jira.EmailSecret,
		cfg.Jira.APITokenSecret,
		cfg.Switch.APIKeySecret,
		cfg.Switch.RegistrationTokenSecret,
		cfg.GitHub.AppIDSecret,
		cfg.GitHub.InstallationIDSecret,
		cfg.GitHub.PrivateKeySecret,
		"postgres_password",
		"openrouter_api_key",
	}
	for _, name := range append([]string{cfg.Agents.TestAuthor}, append(cfg.Agents.Reviewers, cfg.Agents.Resolver)...) {
		if name != "" {
			names = append(names, "agent_token_"+name)
		}
	}
	filtered := names[:0]
	for _, name := range names {
		if name != "" {
			filtered = append(filtered, name)
		}
	}
	names = slices.Compact(slices.Sorted(slices.Values(filtered)))
	return names
}

func checkSecretFiles(dir string, names []string) ([]string, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	missing := make([]string, 0)
	for _, name := range names {
		info, err := root.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			missing = append(missing, name)
			continue
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
			return nil, fmt.Errorf("invalid secret file %s", name)
		}
	}
	return missing, nil
}

func writeAdminState(path string, state adminInitState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(filepath.Dir(absolute))
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	tempName, err := adminTempName(".heimdall-setup-")
	if err != nil {
		return err
	}
	file, err := root.OpenFile(tempName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = root.Remove(tempName) }()
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return root.Rename(tempName, filepath.Base(absolute))
}
