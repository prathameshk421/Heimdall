package agents

import (
	"context"
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
	for _, name := range []string{"test-author", "reviewer-1", "reviewer-2", "reviewer-3", "resolver"} {
		if err := os.WriteFile(filepath.Join(secretsDir, "agent_token_"+name), []byte("dry-run-secret"), 0o600); err != nil {
			t.Fatal(err)
		}
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
	for _, name := range []string{"test-author", "reviewer-1", "reviewer-2", "reviewer-3", "resolver"} {
		if !strings.Contains(text, "plan "+name) {
			t.Errorf("dry-run output missing agent %q: %s", name, text)
		}
	}
	if strings.Contains(text, "dry-run-secret") {
		t.Fatal("dry-run output leaked a token")
	}
	if !strings.Contains(text, "model=openrouter/deepseek/deepseek-v4-flash") {
		t.Fatal("dry-run output did not report the pinned model")
	}
}

func TestInstallDryRunRefusesMissingTokenBeforePlanning(t *testing.T) {
	t.Parallel()

	repo := filepath.Join("..", "..")
	script := filepath.Join(repo, "deploy", "agents", "install.sh")
	cmd := exec.CommandContext(context.Background(), "bash", script,
		"--dry-run",
		"--repo", repo,
		"--agents-dir", t.TempDir(),
		"--secrets-dir", t.TempDir(),
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
