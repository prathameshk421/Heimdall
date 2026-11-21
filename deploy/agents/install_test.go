package agents

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallDryRunPlansEveryManifestAgentWithoutReadingTokens(t *testing.T) {
	t.Parallel()

	repo := filepath.Join("..", "..")
	agentsDir := t.TempDir()
	secretsDir := t.TempDir()
	for _, name := range []string{"test-author", "reviewer-1", "reviewer-2", "reviewer-3", "resolver", "profiler"} {
		if err := os.WriteFile(filepath.Join(secretsDir, "agent_token_"+name), []byte("dry-run-secret"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(secretsDir, "deploy_api_key"), []byte("dry-run-secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	script := filepath.Join(repo, "deploy", "agents", "install.sh")
	cmd := exec.CommandContext(context.Background(), "bash", script,
		"--dry-run",
		"--repo", repo,
		"--agents-dir", agentsDir,
		"--secrets-dir", secretsDir,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("dry-run failed: %v\n%s", err, output)
	}
	text := string(output)
	for _, name := range []string{"test-author", "reviewer-1", "reviewer-2", "reviewer-3", "resolver", "profiler"} {
		if !strings.Contains(text, "plan "+name) {
			t.Errorf("dry-run output missing agent %q: %s", name, text)
		}
	}
	if strings.Contains(text, "dry-run-secret") {
		t.Fatal("dry-run output leaked a token")
	}
	if !strings.Contains(text, "model=deploy/deepseek-ai/deepseek-v4.1-flash-1017") {
		t.Fatal("dry-run output did not report the pinned model")
	}
}

func TestInstallDryRunRefusesMissingTokenBeforePlanning(t *testing.T) {
	t.Parallel()

	repo := filepath.Join("..", "..")
	secretsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(secretsDir, "deploy_api_key"), []byte("dry-run-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(repo, "deploy", "agents", "install.sh")
	cmd := exec.CommandContext(context.Background(), "bash", script,
		"--dry-run",
		"--repo", repo,
		"--agents-dir", t.TempDir(),
		"--secrets-dir", secretsDir,
	)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("dry-run unexpectedly succeeded: %s", output)
	}
	if strings.Contains(string(output), "dry-run-secret") {
		t.Fatal("error output leaked a token")
	}
	if !strings.Contains(string(output), "missing token file") {
		t.Fatalf("error did not identify the missing token file: %s", output)
	}
}

// writeAgentSecrets writes a token file per manifest agent and, when key is not empty, the model key file.
func writeAgentSecrets(t *testing.T, dir, key string) {
	t.Helper()
	for _, name := range []string{"test-author", "reviewer-1", "reviewer-2", "reviewer-3", "resolver", "profiler"} {
		if err := os.WriteFile(filepath.Join(dir, "agent_token_"+name), []byte("token-"+name+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if key != "" {
		if err := os.WriteFile(filepath.Join(dir, "deploy_api_key"), []byte(key+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// The Switch controller passes only an allowlist of variables to agent units, so a {env:DEPLOY_API_KEY} reference
// never resolves (H11, B4). The installer writes the key from the secrets directory into each config, like the token.
func TestInstallWritesTheDeployKeyFromTheSecretsFileIntoEveryConfig(t *testing.T) {
	t.Parallel()

	repo := filepath.Join("..", "..")
	agentsDir := t.TempDir()
	secretsDir := t.TempDir()
	writeAgentSecrets(t, secretsDir, "deploy-key-value")

	script := filepath.Join(repo, "deploy", "agents", "install.sh")
	output, err := exec.CommandContext(context.Background(), "bash", script,
		"--repo", repo, "--agents-dir", agentsDir, "--secrets-dir", secretsDir).CombinedOutput()
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, output)
	}
	if strings.Contains(string(output), "deploy-key-value") {
		t.Fatal("install output leaked the Deploy key")
	}
	for _, name := range []string{"test-author", "reviewer-1", "reviewer-2", "reviewer-3", "resolver", "profiler"} {
		raw, err := os.ReadFile(filepath.Join(agentsDir, "agent", name, "opencode.json"))
		if err != nil {
			t.Fatal(err)
		}
		var config struct {
			Provider map[string]struct {
				Options struct {
					APIKey string `json:"apiKey"`
				} `json:"options"`
			} `json:"provider"`
		}
		if err := json.Unmarshal(raw, &config); err != nil {
			t.Fatalf("%s: decode config: %v", name, err)
		}
		if got := config.Provider["deploy"].Options.APIKey; got != "deploy-key-value" {
			t.Errorf("%s: apiKey = %q, want the key from the Deploy key file", name, got)
		}
		if strings.Contains(string(raw), "{env:") || strings.Contains(string(raw), "{{file:") {
			t.Errorf("%s: config still holds an unresolved reference", name)
		}
	}
}

func TestInstallRefusesAMissingDeployKeyBeforeWritingAnyConfig(t *testing.T) {
	t.Parallel()

	repo := filepath.Join("..", "..")
	agentsDir := t.TempDir()
	secretsDir := t.TempDir()
	writeAgentSecrets(t, secretsDir, "")

	script := filepath.Join(repo, "deploy", "agents", "install.sh")
	output, err := exec.CommandContext(context.Background(), "bash", script,
		"--repo", repo, "--agents-dir", agentsDir, "--secrets-dir", secretsDir).CombinedOutput()
	if err == nil {
		t.Fatalf("install unexpectedly succeeded: %s", output)
	}
	if !strings.Contains(string(output), "missing Deploy key file") {
		t.Fatalf("error did not name the missing Deploy key file: %s", output)
	}
	if entries, _ := os.ReadDir(filepath.Join(agentsDir, "agent")); len(entries) != 0 {
		t.Fatalf("install wrote %d agent directories before refusing", len(entries))
	}
}
